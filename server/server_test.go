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
	defer shared.BackupAndRestore(t)
	shared.Check(t, shared.Setup([]string{}))

	// Submit an entry
	entry, err := shared.BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  "})
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
	dbEntry := retrievedEntries[0]
	if len(retrievedEntries) != 1 {
		t.Fatalf("Expected to retrieve 1 entry, found %d", len(retrievedEntries))
	}
	if dbEntry.UserSecret != "" {
		t.Fatalf("Response contains a user secret: %#v", *dbEntry)
	}
	entry.UserSecret = ""
	if !shared.EntryEquals(*dbEntry, *entry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v\ninput=%#v", *dbEntry, *entry)
	}
}
