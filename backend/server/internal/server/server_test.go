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

	// Submit a few entries for different devices
	entry := testutils.MakeFakeHistoryEntry("ls ~/")
	encEntry, err := data.EncryptHistoryEntry("key", entry)
	testutils.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Equal(t, shared.SubmitResponse{HaveDumpRequests: true, HaveDeletionRequests: false}, deserializeSubmitResponse(t, w))

	// Query for device id 1
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	require.Equal(t, w.Result().StatusCode, 200)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	testutils.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	require.Equal(t, 1, len(retrievedEntries))
	dbEntry := retrievedEntries[0]
	require.Equal(t, devId1, dbEntry.DeviceId)
	require.Equal(t, data.UserId("key"), dbEntry.UserId)
	require.Equal(t, 0, dbEntry.ReadCount)
	decEntry, err := data.DecryptHistoryEntry("key", *dbEntry)
	testutils.Check(t, err)
	require.True(t, data.EntryEquals(decEntry, entry))

	// Same for device id 2
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry = retrievedEntries[0]
	if dbEntry.DeviceId != devId2 {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.UserId != data.UserId("key") {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.ReadCount != 0 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err = data.DecryptHistoryEntry("key", *dbEntry)
	testutils.Check(t, err)
	if !data.EntryEquals(decEntry, entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, entry)
	}

	// Bootstrap handler should return 2 entries, one for each device
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?user_id="+data.UserId("key")+"&device_id="+devId1, nil)
	s.apiBootstrapHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 2 {
		t.Fatalf("Expected to retrieve 2 entries, found %d", len(retrievedEntries))
	}

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
	testutils.Check(t, err)
	var dumpRequests []*shared.DumpRequest
	testutils.Check(t, json.Unmarshal(respBody, &dumpRequests))
	if len(dumpRequests) != 1 {
		t.Fatalf("expected one pending dump request, got %#v", dumpRequests)
	}
	dumpRequest := dumpRequests[0]
	if dumpRequest.RequestingDeviceId != devId2 {
		t.Fatalf("unexpected device ID")
	}
	if dumpRequest.UserId != userId {
		t.Fatalf("unexpected user ID")
	}

	// And one for otherUser
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser+"&device_id="+otherDev1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	dumpRequests = make([]*shared.DumpRequest, 0)
	testutils.Check(t, json.Unmarshal(respBody, &dumpRequests))
	if len(dumpRequests) != 1 {
		t.Fatalf("expected one pending dump request, got %#v", dumpRequests)
	}
	dumpRequest = dumpRequests[0]
	if dumpRequest.RequestingDeviceId != otherDev2 {
		t.Fatalf("unexpected device ID")
	}
	if dumpRequest.UserId != otherUser {
		t.Fatalf("unexpected user ID")
	}

	// And none if we query for a user ID that doesn't exit
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id=foo&device_id=bar", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	resp := strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(resp))

	// And none for a missing user ID
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id=%20&device_id=%20", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	resp = strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(resp))

	// Now submit a dump for userId
	entry1Dec := testutils.MakeFakeHistoryEntry("ls ~/")
	entry1, err := data.EncryptHistoryEntry("dkey", entry1Dec)
	testutils.Check(t, err)
	entry2Dec := testutils.MakeFakeHistoryEntry("aaaaaaáaaa")
	entry2, err := data.EncryptHistoryEntry("dkey", entry1Dec)
	testutils.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{entry1, entry2})
	testutils.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?user_id="+userId+"&requesting_device_id="+devId2+"&source_device_id="+devId1, bytes.NewReader(reqBody))
	s.apiSubmitDumpHandler(httptest.NewRecorder(), submitReq)

	// Check that the dump request is no longer there for userId for either device ID
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	resp = strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(respBody))

	w = httptest.NewRecorder()

	// The other user
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId2, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	resp = strings.TrimSpace(string(respBody))
	require.Equalf(t, "[]", resp, "got unexpected respBody: %#v", string(respBody))

	// But it is there for the other user
	w = httptest.NewRecorder()
	s.apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser+"&device_id="+otherDev1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	dumpRequests = make([]*shared.DumpRequest, 0)
	testutils.Check(t, json.Unmarshal(respBody, &dumpRequests))
	if len(dumpRequests) != 1 {
		t.Fatalf("expected one pending dump request, got %#v", dumpRequests)
	}
	dumpRequest = dumpRequests[0]
	if dumpRequest.RequestingDeviceId != otherDev2 {
		t.Fatalf("unexpected device ID")
	}
	if dumpRequest.UserId != otherUser {
		t.Fatalf("unexpected user ID")
	}

	// And finally, query to ensure that the dumped entries are in the DB
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 2 {
		t.Fatalf("Expected to retrieve 2 entries, found %d", len(retrievedEntries))
	}
	for _, dbEntry := range retrievedEntries {
		if dbEntry.DeviceId != devId2 {
			t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
		}
		if dbEntry.UserId != userId {
			t.Fatalf("Response contains an incorrect user ID: %#v", *dbEntry)
		}
		if dbEntry.ReadCount != 0 {
			t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
		}
		decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
		testutils.Check(t, err)
		if !data.EntryEquals(decEntry, entry1Dec) && !data.EntryEquals(decEntry, entry2Dec) {
			t.Fatalf("DB data is different than input! \ndb   =%#v\nentry1=%#v\nentry2=%#v", *dbEntry, entry1Dec, entry2Dec)
		}
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
	testutils.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Equal(t, shared.SubmitResponse{HaveDumpRequests: true, HaveDeletionRequests: false}, deserializeSubmitResponse(t, w))

	// And another entry for user1
	entry2 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry2.DeviceId = devId2
	encEntry, err = data.EncryptHistoryEntry("dkey", entry2)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId2, bytes.NewReader(reqBody))
	w = httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Equal(t, shared.SubmitResponse{HaveDumpRequests: true, HaveDeletionRequests: false}, deserializeSubmitResponse(t, w))

	// And an entry for user2 that has the same timestamp as the previous entry
	entry3 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry3.StartTime = entry1.StartTime
	entry3.EndTime = entry1.EndTime
	encEntry, err = data.EncryptHistoryEntry("dOtherkey", entry3)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w = httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Equal(t, shared.SubmitResponse{HaveDumpRequests: true, HaveDeletionRequests: false}, deserializeSubmitResponse(t, w))

	// Query for device id 1
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	testutils.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 2 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	for _, dbEntry := range retrievedEntries {
		if dbEntry.DeviceId != devId1 {
			t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
		}
		if dbEntry.UserId != data.UserId("dkey") {
			t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
		}
		if dbEntry.ReadCount != 0 {
			t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
		}
		decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
		testutils.Check(t, err)
		if !data.EntryEquals(decEntry, entry1) && !data.EntryEquals(decEntry, entry2) {
			t.Fatalf("DB data is different than input! \ndb   =%#v\nentry1=%#v\nentry2=%#v", *dbEntry, entry1, entry2)
		}
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
	testutils.Check(t, err)
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
	testutils.Check(t, err)
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry := retrievedEntries[0]
	if dbEntry.DeviceId != devId1 {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.UserId != data.UserId("dkey") {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.ReadCount != 1 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
	testutils.Check(t, err)
	if !data.EntryEquals(decEntry, entry2) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\nentry=%#v", *dbEntry, entry2)
	}

	// Query for user 2
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	s.apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry = retrievedEntries[0]
	if dbEntry.DeviceId != otherDev1 {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.UserId != data.UserId("dOtherkey") {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.ReadCount != 0 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err = data.DecryptHistoryEntry("dOtherkey", *dbEntry)
	testutils.Check(t, err)
	if !data.EntryEquals(decEntry, entry3) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\nentry=%#v", *dbEntry, entry3)
	}

	// Check that apiSubmit tells the client that there is a pending deletion request
	encEntry, err = data.EncryptHistoryEntry("dkey", entry2)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId2, bytes.NewReader(reqBody))
	w = httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Equal(t, shared.SubmitResponse{HaveDumpRequests: true, HaveDeletionRequests: true}, deserializeSubmitResponse(t, w))

	// Query for deletion requests
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.getDeletionRequestsHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = io.ReadAll(res.Body)
	testutils.Check(t, err)
	var deletionRequests []*shared.DeletionRequest
	testutils.Check(t, json.Unmarshal(respBody, &deletionRequests))
	if len(deletionRequests) != 1 {
		t.Fatalf("received %d deletion requests, expected only one", len(deletionRequests))
	}
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
	s := NewServer(DB, TrackUsageData(false))
	w := httptest.NewRecorder()
	s.healthCheckHandler(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 200 {
		t.Fatalf("expected 200 resp code for healthCheckHandler")
	}
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	testutils.Check(t, err)
	if string(respBody) != "OK" {
		t.Fatalf("expected healthcheckHandler to return OK")
	}

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
	testutils.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?source_device_id="+devId1, bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.apiSubmitHandler(w, submitReq)
	require.Equal(t, 200, w.Result().StatusCode)
	require.Equal(t, shared.SubmitResponse{HaveDumpRequests: true, HaveDeletionRequests: false}, deserializeSubmitResponse(t, w))

	// Call cleanDatabase and just check that there are no panics
	testutils.Check(t, DB.Clean(context.TODO()))
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
