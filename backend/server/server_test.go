package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
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
	apiERegisterHandler(nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2+"&user_id="+userId, nil)
	apiERegisterHandler(nil, deviceReq)
	deviceReq = httptest.NewRequest(http.MethodGet, "/?device_id="+otherDev+"&user_id="+otherUser, nil)
	apiERegisterHandler(nil, deviceReq)

	// Submit a few entries for different devices
	entry := data.MakeFakeHistoryEntry("ls ~/")
	encEntry, err := data.EncryptHistoryEntry("key", entry)
	shared.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiESubmitHandler(nil, submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+devId1, nil)
	apiEQueryHandler(w, searchReq)
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
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+devId2, nil)
	apiEQueryHandler(w, searchReq)
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
	searchReq = httptest.NewRequest(http.MethodGet, "/?user_id="+data.UserId("key"), nil)
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
