package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ddworken/hishtory/internal/database"
	"github.com/ddworken/hishtory/internal/server"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/go-test/deep"
	"github.com/google/uuid"
)

func TestESubmitThenQuery(t *testing.T) {
	// Set up
	InitDB()
	s := server.NewServer(GLOBAL_DB)

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
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	s.apiSubmitHandler(httptest.NewRecorder(), submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	s.apiQueryHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	testutils.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry := retrievedEntries[0]
	if dbEntry.DeviceId != devId1 {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.UserId != data.UserId("key") {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.ReadCount != 0 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err := data.DecryptHistoryEntry("key", *dbEntry)
	testutils.Check(t, err)
	if !data.EntryEquals(decEntry, entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, entry)
	}

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
	assertNoLeakedConnections(t, GLOBAL_DB)
}

func TestDumpRequestAndResponse(t *testing.T) {
	// Set up
	InitDB()
	s := server.NewServer(GLOBAL_DB)

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
	assertNoLeakedConnections(t, GLOBAL_DB)
}

func TestUpdateReleaseVersion(t *testing.T) {
	if !testutils.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}

	// Set up
	InitDB()

	// Check that ReleaseVersion hasn't been set yet
	if ReleaseVersion != "UNKNOWN" {
		t.Fatalf("initial ReleaseVersion isn't as expected: %#v", ReleaseVersion)
	}

	// Update it
	err := updateReleaseVersion()
	if err != nil {
		t.Fatalf("updateReleaseVersion failed: %v", err)
	}

	// If ReleaseVersion is still unknown, skip because we're getting rate limited
	if ReleaseVersion == "UNKNOWN" {
		t.Skip()
	}
	// Otherwise, check that the new value looks reasonable
	if !strings.HasPrefix(ReleaseVersion, "v0.") {
		t.Fatalf("ReleaseVersion wasn't updated to contain a version: %#v", ReleaseVersion)
	}

	// Assert that we aren't leaking connections
	assertNoLeakedConnections(t, GLOBAL_DB)
}

func TestDeletionRequests(t *testing.T) {
	// Set up
	InitDB()
	s := server.NewServer(GLOBAL_DB)

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
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	s.apiSubmitHandler(httptest.NewRecorder(), submitReq)

	// And another entry for user1
	entry2 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry2.DeviceId = devId2
	encEntry, err = data.EncryptHistoryEntry("dkey", entry2)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	s.apiSubmitHandler(httptest.NewRecorder(), submitReq)

	// And an entry for user2 that has the same timestamp as the previous entry
	entry3 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry3.StartTime = entry1.StartTime
	entry3.EndTime = entry1.EndTime
	encEntry, err = data.EncryptHistoryEntry("dOtherkey", entry3)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	s.apiSubmitHandler(httptest.NewRecorder(), submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
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
			{DeviceId: devId1, Date: entry1.EndTime},
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
			{DeviceId: devId1, Date: entry1.EndTime},
		}},
	}
	if diff := deep.Equal(*deletionRequest, expected); diff != nil {
		t.Error(diff)
	}

	// Assert that we aren't leaking connections
	assertNoLeakedConnections(t, GLOBAL_DB)
}

func TestHealthcheck(t *testing.T) {
	s := server.NewServer(GLOBAL_DB)
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
	assertNoLeakedConnections(t, GLOBAL_DB)
}

func TestLimitRegistrations(t *testing.T) {
	// Set up
	InitDB()
	s := server.NewServer(GLOBAL_DB)
	checkGormResult(GLOBAL_DB.Exec("DELETE FROM enc_history_entries"))
	checkGormResult(GLOBAL_DB.Exec("DELETE FROM devices"))
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
	InitDB()
	s := server.NewServer(GLOBAL_DB)

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
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	s.apiSubmitHandler(httptest.NewRecorder(), submitReq)

	// Call cleanDatabase and just check that there are no panics
	testutils.Check(t, GLOBAL_DB.Clean(context.TODO()))
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
