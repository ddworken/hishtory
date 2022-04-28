package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/shared"
	"github.com/google/uuid"
)

func TestESubmitThenQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	InitDB()

	// Register a few devices
	userId := data.UserId("key")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	devId2 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("otherkey")
	otherDev := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiRegisterHandler(nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	apiRegisterHandler(nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev+"&user_id="+otherUser, nil)
	apiRegisterHandler(nil, deviceReq)

	// Submit a few entries for different devices
	entry := data.MakeFakeHistoryEntry("ls ~/")
	encEntry, err := data.EncryptHistoryEntry("key", entry)
	shared.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(nil, submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiQueryHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	shared.Check(t, json.Unmarshal(respBody, &retrievedEntries))
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
	if dbEntry.ReadCount != 1 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err := data.DecryptHistoryEntry("key", *dbEntry)
	shared.Check(t, err)
	if !data.EntryEquals(decEntry, entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, entry)
	}

	// Same for device id 2
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	shared.Check(t, json.Unmarshal(respBody, &retrievedEntries))
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
	if dbEntry.ReadCount != 1 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err = data.DecryptHistoryEntry("key", *dbEntry)
	shared.Check(t, err)
	if !data.EntryEquals(decEntry, entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, entry)
	}

	// Bootstrap handler should return 2 entries, one for each device
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?user_id="+data.UserId("key")+"&device_id="+devId1, nil)
	apiEBootstrapHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	shared.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 2 {
		t.Fatalf("Expected to retrieve 2 entries, found %d", len(retrievedEntries))
	}
}

func TestDumpRequestAndResponse(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	InitDB()

	// Register a first device for two different users
	userId := data.UserId("dkey")
	devId1 := uuid.Must(uuid.NewRandom()).String()
	otherUser := data.UserId("dOtherkey")
	otherDev1 := uuid.Must(uuid.NewRandom()).String()
	deviceReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiRegisterHandler(nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev1+"&user_id="+otherUser, nil)
	apiRegisterHandler(nil, deviceReq)

	// Query for dump requests, there should be one for userId
	w := httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId, nil))
	res := w.Result()
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var dumpRequests []*DumpRequest
	shared.Check(t, json.Unmarshal(respBody, &dumpRequests))
	if len(dumpRequests) != 1 {
		t.Fatalf("expected one pending dump request, got %#v", dumpRequests)
	}
	dumpRequest := dumpRequests[0]
	if dumpRequest.RequestingDeviceId != devId1 {
		t.Fatalf("unexpected device ID")
	}
	if dumpRequest.UserId != userId {
		t.Fatalf("unexpected user ID")
	}

	// And one for otherUser
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	dumpRequests = make([]*DumpRequest, 0)
	shared.Check(t, json.Unmarshal(respBody, &dumpRequests))
	if len(dumpRequests) != 1 {
		t.Fatalf("expected one pending dump request, got %#v", dumpRequests)
	}
	dumpRequest = dumpRequests[0]
	if dumpRequest.RequestingDeviceId != otherDev1 {
		t.Fatalf("unexpected device ID")
	}
	if dumpRequest.UserId != otherUser {
		t.Fatalf("unexpected user ID")
	}

	// And none if we query without a user ID
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}

	// And none if we query for a user ID that doesn't exit
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id=foo", nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}

	// Now submit a dump for userId
	entry1Dec := data.MakeFakeHistoryEntry("ls ~/")
	entry1, err := data.EncryptHistoryEntry("dkey", entry1Dec)
	shared.Check(t, err)
	entry2Dec := data.MakeFakeHistoryEntry("aaaaaaáaaa")
	entry2, err := data.EncryptHistoryEntry("dkey", entry1Dec)
	shared.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{entry1, entry2})
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/?user_id="+userId+"&requesting_device_id="+devId1, bytes.NewReader(reqBody))
	apiSubmitDumpHandler(nil, submitReq)

	// Check that the dump request is no longer there for userId
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+userId, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	if string(respBody) != "[]" {
		t.Fatalf("got unexpected respBody: %#v", string(respBody))
	}

	// But it is there for the other user
	w = httptest.NewRecorder()
	apiGetPendingDumpRequestsHandler(w, httptest.NewRequest(http.MethodGet, "/?user_id="+otherUser, nil))
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	dumpRequests = make([]*DumpRequest, 0)
	shared.Check(t, json.Unmarshal(respBody, &dumpRequests))
	if len(dumpRequests) != 1 {
		t.Fatalf("expected one pending dump request, got %#v", dumpRequests)
	}
	dumpRequest = dumpRequests[0]
	if dumpRequest.RequestingDeviceId != otherDev1 {
		t.Fatalf("unexpected device ID")
	}
	if dumpRequest.UserId != otherUser {
		t.Fatalf("unexpected user ID")
	}

	// And finally, query to ensure that the dumped entries are in the DB
	w = httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1+"&user_id="+userId, nil)
	apiQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	respBody, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	shared.Check(t, json.Unmarshal(respBody, &retrievedEntries))
	if len(retrievedEntries) != 2 {
		t.Fatalf("Expected to retrieve 2 entries, found %d", len(retrievedEntries))
	}
	for _, dbEntry := range retrievedEntries {
		if dbEntry.DeviceId != devId1 {
			t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
		}
		if dbEntry.UserId != userId {
			t.Fatalf("Response contains an incorrect user ID: %#v", *dbEntry)
		}
		if dbEntry.ReadCount != 1 {
			t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
		}
		decEntry, err := data.DecryptHistoryEntry("dkey", *dbEntry)
		shared.Check(t, err)
		if !data.EntryEquals(decEntry, entry1Dec) && !data.EntryEquals(decEntry, entry2Dec) {
			t.Fatalf("DB data is different than input! \ndb   =%#v\nentry1=%#v\nentry2=%#v", *dbEntry, entry1Dec, entry2Dec)
		}
	}
}

func TestUpdateReleaseVersion(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
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
