package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
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

	// Register a few devices
	userId := data.UserId("key")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("otherkey")
	otherDev := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev+"&user_id="+otherUser, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)

	// Submit a few entries for different devices
	entry := testutils.MakeFakeHistoryEntry("ls ~/")
	encEntry, err := data.EncryptHistoryEntry("key", entry)
	testutils.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(context.Background(), nil, submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiQueryHandler(context.Background(), w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
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
	apiQueryHandler(context.Background(), w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
	apiBootstrapHandler(context.Background(), w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	testutils.Check(t, err)
	testutils.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 2 {
		t.Fatalf("Expected to retrieve 2 entries, found %d", len(retrievedEntries))
	}
}

func TestDumpRequestAndResponse(t *testing.T) {
	// Set up
	InitDB()

	// Register a first device for two different users
	userId := data.UserId("dkey")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("dOtherkey")
	otherDev1 := uuid.Must(uuid.NewRandom()).String()
	otherDev2 := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev2+"&user_id="+otherUser, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)

	// Query for dump requests, there should be one for userId
	w := httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId1, nil))
	res := w.Result()
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
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
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser+"&device_id="+otherDev1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id=foo&device_id=bar", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	testutils.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}

	// And none for a missing user ID
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id=%20&device_id=%20", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	testutils.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}

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
	apiSubmitDumpHandler(context.Background(), nil, submitReq)

	// Check that the dump request is no longer there for userId for either device ID
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	testutils.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}
	w = httptest.NewRecorder()
	// The other user
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId+"&device_id="+devId2, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	testutils.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}

	// But it is there for the other user
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(context.Background(), w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser+"&device_id="+otherDev1, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
	apiQueryHandler(context.Background(), w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
}

func TestDeletionRequests(t *testing.T) {
	// Set up
	InitDB()

	// Register two devices for two different users
	userId := data.UserId("dkey")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("dOtherkey")
	otherDev1 := uuid.Must(uuid.NewRandom()).String()
	otherDev2 := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev2+"&user_id="+otherUser, nil)
	apiRegisterHandler(context.Background(), nil, deviceReq)

	// Add an entry for user1
	entry1 := testutils.MakeFakeHistoryEntry("ls ~/")
	entry1.DeviceId = devId1
	encEntry, err := data.EncryptHistoryEntry("dkey", entry1)
	testutils.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(context.Background(), nil, submitReq)

	// And another entry for user1
	entry2 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry2.DeviceId = devId2
	encEntry, err = data.EncryptHistoryEntry("dkey", entry2)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(context.Background(), nil, submitReq)

	// And an entry for user2 that has the same timestamp as the previous entry
	entry3 := testutils.MakeFakeHistoryEntry("ls /foo/bar")
	entry3.StartTime = entry1.StartTime
	entry3.EndTime = entry1.EndTime
	encEntry, err = data.EncryptHistoryEntry("dOtherkey", entry3)
	testutils.Check(t, err)
	reqBody, err = json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(context.Background(), nil, submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiQueryHandler(context.Background(), w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
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
	addDeletionRequestHandler(context.Background(), nil, req)

	// Query again for device id 1 and get a single result
	time.Sleep(10 * time.Millisecond)
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiQueryHandler(context.Background(), w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
	apiQueryHandler(context.Background(), w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
	getDeletionRequestsHandler(context.Background(), w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
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
}

// TODO: test add tests that check usage data
