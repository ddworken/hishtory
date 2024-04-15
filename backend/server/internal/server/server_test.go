package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/backend/server/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/go-test/deep"
	"github.com/google/uuid"
)

var DB *database.DB

const testDBDSN = "file::memory:?_journal_mode=WAL&cache=shared"

func TestMain(m *testing.M) {
	// Set env variable
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()
	os.Setenv("HISHTORY_TEST", "1")

	// setup test database
	db, err := database.OpenSQLite(testDBDSN, &gorm.Config{})
	if err != nil {
		panic(fmt.Errorf("failed to connect to the DB: %w", err))
	}
	underlyingDb, err := db.DB.DB()
	if err != nil {
		panic(fmt.Errorf("failed to access underlying DB: %w", err))
	}
	underlyingDb.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode = WAL")
	err = db.AddDatabaseTables()
	if err != nil {
		panic(fmt.Errorf("failed to add database tables: %w", err))
	}

	DB = db

	os.Exit(m.Run())
}

func TestESubmitThenQuery(t *testing.T) {
	// Set up
	s := NewServer(DB, TrackUsageData(false))

	// Register a few devices
	userId := data.UserId("key")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("otherkey")
	otherDev := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev+"&user_id="+otherUser, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)

	// Submit an entry from device 1
	entry := testutils.MakeFakeHistoryEntry("ls ~/")
	encEntry, err := data.EncryptHistoryEntry("key", entry)
	require.NoError(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Empty(t, deserializeSubmitResponse(t, w).DeletionRequests)
	require.NotEmpty(t, deserializeSubmitResponse(t, w).DumpRequests)

	// Query for device id 1, no results returned
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	require.Equal(t, w.Result().StatusCode, 200)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Equal(t, 0, len(retrievedEntries))

	// Query for device id 2 and the entry is found
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Len(t, retrievedEntries, 1)
	dbEntry := retrievedEntries[0]
	require.Equal(t, dbEntry.DeviceId, devId2)
	require.Equal(t, dbEntry.UserId, data.UserId("key"))
	require.Equal(t, 0, dbEntry.ReadCount)
	decEntry, err := data.DecryptHistoryEntry("key", *dbEntry)
	require.NoError(t, err)
	require.Equal(t, decEntry, entry)

	// Bootstrap handler should return 2 entries, one for each device
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?user_id="+data.UserId("key")+"&device_id="+devId1, nil)
	s.apiBootstrapHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Len(t, retrievedEntries, 2)

	// Assert that we aren't leaking connections
	assertNoLeakedConnections(t, DB)
}

func TestDumpRequestAndResponse(t *testing.T) {
	// Set up
	s := NewServer(DB, TrackUsageData(false))

	// Register a first device for two different users
	userId := data.UserId("dkey")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("dOtherkey")
	otherDev1 := uuid.Must(uuid.NewRandom()).String()
	otherDev2 := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev2+"&user_id="+otherUser, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)

	// Query for dump requests, there should be one for userId
	w := httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId1, nil))
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var dumpRequests []*shared.DumpRequest
	require.NoError(t, json.Unmarshal(respBody, &dumpRequests))
	require.Len(t, dumpRequests, 1)
	dumpRequest := dumpRequests[0]
	require.Equal(t, devId2, dumpRequest.RequestingDeviceId)
	require.Equal(t, userId, dumpRequest.UserId)

	// And one for otherUser
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser+"&device_id="+otherDev1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	dumpRequests = make([]*shared.DumpRequest, 0)
	require.NoError(t, json.Unmarshal(respBody, &dumpRequests))
	require.Len(t, dumpRequests, 1)
	dumpRequest = dumpRequests[0]
	require.Equal(t, otherDev2, dumpRequest.RequestingDeviceId)
	require.Equal(t, otherUser, dumpRequest.UserId)

	// And none if we query for a user ID that doesn't exit
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id=foo&device_id=bar", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	resp := strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(resp))

	// And none for a missing user ID
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id=%20&device_id=%20", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	resp = strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(resp))

	// Now submit a dump for userId
	entry1Dec := testutils.MakeFakeHistoryEntry("ls ~/")
	entry1, err := data.EncryptHistoryEntry("dkey", entry1Dec)
	require.NoError(t, err)
	entry2Dec := testutils.MakeFakeHistoryEntry("aaaaaaáaaa")
	entry2, err := data.EncryptHistoryEntry("dkey", entry1Dec)
	require.NoError(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{entry1, entry2})
	require.NoError(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?user_id="+userId+"&requesting_device_id="+devId2+"&source_device_id="+devId1, bytes.NewReader(reqBody))
	s.apiSubmitDumpHandler(httptest.NewRecorder(), submitReq)

	// Check that the dump request is no longer there for userId for either device ID
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	resp = strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(respBody))

	w = httptest.NewRecorder()

	// The other user
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId2, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	resp = strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(respBody))

	// But it is there for the other user
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser+"&device_id="+otherDev1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	dumpRequests = make([]*shared.DumpRequest, 0)
	require.NoError(t, json.Unmarshal(respBody, &dumpRequests))
	require.Len(t, dumpRequests, 1)
	dumpRequest = dumpRequests[0]
	require.Equal(t, otherDev2, dumpRequest.RequestingDeviceId)
	require.Equal(t, otherUser, dumpRequest.UserId)

	// And finally, query to ensure that the dumped entries are in the DB
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Len(t, retrievedEntries, 2)
	for _, dbEntry := range retrievedEntries {
		require.Equal(t, devId2, dbEntry.DeviceId)
		require.Equal(t, userId, dbEntry.UserId)
		require.Equal(t, 0, dbEntry.ReadCount)
		decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
		require.NoError(t, err)
		require.True(t, assert.ObjectsAreEqual(decEntry, entry1Dec) || assert.ObjectsAreEqual(decEntry, entry2Dec))
	}

	// Assert that we aren't leaking connections
	assertNoLeakedConnections(t, DB)
}

func TestDeletionRequests(t *testing.T) {
	// Set up
	s := NewServer(DB, TrackUsageData(false))

	// Register two devices for two different users
	userId := data.UserId("dkey")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("dOtherkey")
	otherDev1 := uuid.Must(uuid.NewRandom()).String()
	otherDev2 := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev2+"&user_id="+otherUser, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)

	// Add an entry for user1
	entry1 := testutils.MakeFakeHistoryEntry("ls ~/")
	entry1.DeviceId = devId1
	encEntry, err := data.EncryptHistoryEntry("dkey", entry1)
	require.NoError(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Empty(t, deserializeSubmitResponse(t, w).DeletionRequests)

	// And another entry for user1
	entry2 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry2.DeviceId = devId2
	encEntry, err = data.EncryptHistoryEntry("dkey", entry2)
	require.NoError(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId2, bytes.NewReader(reqBody))
	w = httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Empty(t, deserializeSubmitResponse(t, w).DeletionRequests)

	// And an entry for user2 that has the same timestamp as the previous entry
	entry3 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry3.StartTime = entry1.StartTime
	entry3.EndTime = entry1.EndTime
	encEntry, err = data.EncryptHistoryEntry("dOtherkey", entry3)
	require.NoError(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w = httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Empty(t, deserializeSubmitResponse(t, w).DeletionRequests)

	// Query for device id 1
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Len(t, retrievedEntries, 1)
	for _, dbEntry := range retrievedEntries {
		require.Equal(t, devId1, dbEntry.DeviceId)
		require.Equal(t, data.UserId("dkey"), dbEntry.UserId)
		require.Equal(t, 0, dbEntry.ReadCount)
		decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
		require.NoError(t, err)
		require.True(t, assert.ObjectsAreEqual(decEntry, entry1) || assert.ObjectsAreEqual(decEntry, entry2))
	}

	// Submit a redact request for entry1
	delReqTime := time.Now()
	delReq := shared.DeletionRequest{
		UserId:   data.UserId("dkey"),
		SendTime: delReqTime,
		Messages: shared.MessageIdentifiers{Ids: []shared.MessageIdentifier{
			{DeviceId: devId1, EndTime: entry1.EndTime},
		}},
	}
	reqBody, err = json.Marshal(delReq)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	s.addDeletionRequestHandler(httptest.NewRecorder(), req)

	// Query again for device id 1 and get a single result
	time.Sleep(10 * time.Millisecond)
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Len(t, retrievedEntries, 1)
	dbEntry := retrievedEntries[0]
	require.Equal(t, devId1, dbEntry.DeviceId)
	require.Equal(t, data.UserId("dkey"), dbEntry.UserId)
	require.Equal(t, 1, dbEntry.ReadCount)
	decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
	require.NoError(t, err)
	require.Equal(t, decEntry, entry2)

	// Query for user 2
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Len(t, retrievedEntries, 1)
	dbEntry = retrievedEntries[0]
	require.Equal(t, otherDev1, dbEntry.DeviceId)
	require.Equal(t, data.UserId("dOtherkey"), dbEntry.UserId)
	require.Equal(t, 0, dbEntry.ReadCount)
	decEntry, err = data.DecryptHistoryEntry("dOtherkey", *dbEntry)
	require.NoError(t, err)
	require.Equal(t, decEntry, entry3)

	// Check that apiSubmit tells the client that there is a pending deletion request
	encEntry, err = data.EncryptHistoryEntry("dkey", entry2)
	require.NoError(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId2, bytes.NewReader(reqBody))
	w = httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.NotEmpty(t, deserializeSubmitResponse(t, w).DeletionRequests)

	// Query for deletion requests
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.getDeletionRequestsHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	var deletionRequests []*shared.DeletionRequest
	require.NoError(t, json.Unmarshal(respBody, &deletionRequests))
	require.Len(t, deletionRequests, 1)
	deletionRequest := deletionRequests[0]
	expected := shared.DeletionRequest{
		UserId:              data.UserId("dkey"),
		DestinationDeviceId: devId1,
		SendTime:            delReqTime,
		ReadCount:           1,
		Messages: shared.MessageIdentifiers{Ids: []shared.MessageIdentifier{
			{DeviceId: devId1, EndTime: entry1.EndTime},
		}},
	}
	if diff := deep.Equal(*deletionRequest, expected); diff != nil {
		t.Error(diff)
	}

	// Assert that we aren't leaking connections
	assertNoLeakedConnections(t, DB)
}

func TestHealthcheck(t *testing.T) {
	s := NewServer(DB, TrackUsageData(true))
	w := httptest.NewRecorder()
	s.healthCheckHandler(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, 200, w.Code)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, "OK", string(respBody))

	// Assert that we aren't leaking connections
	assertNoLeakedConnections(t, DB)
}

func TestLimitRegistrations(t *testing.T) {
	// Set up
	s := NewServer(DB, TrackUsageData(false))

	if resp := DB.Exec("DELETE FROM enc_history_entries"); resp.Error != nil {
		t.Fatalf("failed to delete enc_history_entries: %v", resp.Error)
	}

	if resp := DB.Exec("DELETE FROM devices"); resp.Error != nil {
		t.Fatalf("failed to delete devices: %v", resp.Error)
	}
	defer testutils.BackupAndRestoreEnv("HISHTORY_MAX_NUM_USERS")()
	os.Setenv("HISHTORY_MAX_NUM_USERS", "2")

	// Register three devices across two users
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+uuid.Must(uuid.NewRandom()).String()+"&user_id="+data.UserId("user1"), nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+uuid.Must(uuid.NewRandom()).String()+"&user_id="+data.UserId("user1"), nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+uuid.Must(uuid.NewRandom()).String()+"&user_id="+data.UserId("user2"), nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)

	// And this next one should fail since it is a new user
	defer func() { _ = recover() }()
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+uuid.Must(uuid.NewRandom()).String()+"&user_id="+data.UserId("user3"), nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	t.Errorf("expected panic")
}

func TestCleanDatabaseNoErrors(t *testing.T) {
	// Init
	s := NewServer(DB, TrackUsageData(false))

	// Create a user and an entry
	userId := data.UserId("dkey")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiRegisterHandler(httptest.NewRecorder(), deviceReq)
	entry1 := testutils.MakeFakeHistoryEntry("ls ~/")
	entry1.DeviceId = devId1
	encEntry, err := data.EncryptHistoryEntry("dkey", entry1)
	require.NoError(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Empty(t, deserializeSubmitResponse(t, w).DeletionRequests)
	require.NotEmpty(t, deserializeSubmitResponse(t, w).DumpRequests)

	// Call cleanDatabase and just check that there are no panics
	require.NoError(t, DB.Clean(context.TODO()))
}

func assertNoLeakedConnections(t *testing.T, db *database.DB) {
	stats, err := db.Stats()
	if err != nil {
		t.Fatal(err)
	}
	numConns := stats.OpenConnections
	if numConns > 1 {
		t.Fatalf("expected DB to have not leak connections, actually have %d", numConns)
	}
}

func deserializeSubmitResponse(t *testing.T, w *httptest.ResponseRecorder) shared.SubmitResponse {
	submitResponse := shared.SubmitResponse{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &submitResponse))
	return submitResponse
}
