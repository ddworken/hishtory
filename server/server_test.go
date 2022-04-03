package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ddworken/hishtory/shared"
)

func TestSubmitThenQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	InitDB()
	shared.Check(t, shared.Setup(0, []string{}))

	// Submit an entry
	entry, err := shared.BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	shared.Check(t, err)
	reqBody, err := json.Marshal(entry)
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(nil, submitReq)
	// Also submit one for another user
	otherEntry := *entry
	otherEntry.UserSecret = "aaaaaaaaa"
	otherEntry.Command = "other"
	reqBody, err = json.Marshal(otherEntry)
	shared.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(nil, submitReq)

	// Retrieve the entry
	secret, err := shared.GetUserSecret()
	shared.Check(t, err)
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?user_secret="+secret, nil)
	apiSearchHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var retrievedEntries []*shared.HistoryEntry
	shared.Check(t, json.Unmarshal(data, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry := retrievedEntries[0]
	if dbEntry.UserSecret != "" {
		t.Fatalf("Response contains a user secret: %#v", *dbEntry)
	}
	entry.UserSecret = ""
	if !shared.EntryEquals(*dbEntry, *entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, *entry)
	}
}

func TestNoUserSecretGivesNoResults(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	InitDB()
	shared.Check(t, shared.Setup(0, []string{}))

	// Submit an entry
	entry, err := shared.BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	shared.Check(t, err)
	reqBody, err := json.Marshal(entry)
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(nil, submitReq)

	// Retrieve entries with no user secret
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/", nil)
	apiSearchHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var retrievedEntries []*shared.HistoryEntry
	shared.Check(t, json.Unmarshal(data, &retrievedEntries))
	if len(retrievedEntries) != 0 {
		t.Fatalf("Expected to retrieve 0 entries, found %d, results[0]=%#v", len(retrievedEntries), retrievedEntries[0])
	}
}

func TestSearchQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	InitDB()
	shared.Check(t, shared.Setup(0, []string{}))

	// Submit an entry that we'll match
	entry, err := shared.BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /bar  ", "1641774958326745663"})
	shared.Check(t, err)
	reqBody, err := json.Marshal(entry)
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(nil, submitReq)
	// Submit an entry that we won't match
	entry, err = shared.BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /foo  ", "1641774958326745663"})
	shared.Check(t, err)
	reqBody, err = json.Marshal(entry)
	shared.Check(t, err)
	submitReq = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiSubmitHandler(nil, submitReq)

	// Retrieve the entry
	secret, err := shared.GetUserSecret()
	shared.Check(t, err)
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?user_secret="+secret+"&query=foo", nil)
	apiSearchHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var retrievedEntries []*shared.HistoryEntry
	shared.Check(t, json.Unmarshal(data, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry := retrievedEntries[0]
	if dbEntry.Command != "ls /foo" {
		t.Fatalf("Response contains an unexpected command: %#v", *dbEntry)
	}
}

func TestESubmitThenQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	InitDB()
	shared.Check(t, shared.Setup(0, []string{}))

	// Submit a few entries for different devices
	entry, err := shared.BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	shared.Check(t, err)
	encEntry, err := shared.EncryptHistoryEntry("key", *entry)
	shared.Check(t, err)
	reqBody, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	shared.Check(t, err)
	submitReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	apiESubmitHandler(nil, submitReq)

	// Query for device id 1
	w := httptest.NewRecorder()
	searchReq := httptest.NewRequest(http.MethodGet, "/?device_id="+shared.DeviceId("key", 1), nil)
	apiEQueryHandler(w, searchReq)
	res := w.Result()
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	var retrievedEntries []*shared.EncHistoryEntry
	shared.Check(t, json.Unmarshal(data, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry := retrievedEntries[0]
	if dbEntry.DeviceId != shared.DeviceId("key", 1) {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.UserId != shared.UserId("key") {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.ReadCount != 1 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err := shared.DecryptHistoryEntry("key", 1, *dbEntry)
	shared.Check(t, err)
	if !shared.EntryEquals(decEntry, *entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, *entry)
	}

	// Same for device id 2
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?device_id="+shared.DeviceId("key", 2), nil)
	apiEQueryHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	data, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	shared.Check(t, json.Unmarshal(data, &retrievedEntries))
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	dbEntry = retrievedEntries[0]
	if dbEntry.DeviceId != shared.DeviceId("key", 2) {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.UserId != shared.UserId("key") {
		t.Fatalf("Response contains an incorrect device ID: %#v", *dbEntry)
	}
	if dbEntry.ReadCount != 1 {
		t.Fatalf("db.ReadCount should have been 1, was %v", dbEntry.ReadCount)
	}
	decEntry, err = shared.DecryptHistoryEntry("key", 2, *dbEntry)
	shared.Check(t, err)
	if !shared.EntryEquals(decEntry, *entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, *entry)
	}

	// Bootstrap handler should return 3 entries, one for each device
	w = httptest.NewRecorder()
	searchReq = httptest.NewRequest(http.MethodGet, "/?user_id="+shared.UserId("key"), nil)
	apiEBootstrapHandler(w, searchReq)
	res = w.Result()
	defer res.Body.Close()
	data, err = ioutil.ReadAll(res.Body)
	shared.Check(t, err)
	shared.Check(t, json.Unmarshal(data, &retrievedEntries))
	if len(retrievedEntries) != 3 {
		t.Fatalf("Expected to retrieve 3 entries, found %d", len(retrievedEntries))
	}

}
