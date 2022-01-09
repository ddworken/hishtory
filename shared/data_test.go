package shared

import (
	"testing"
)

func TestPersist(t *testing.T) {
	defer BackupAndRestore(t)
	Check(t, Setup([]string{}))
	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  "})
	Check(t, err)
	Check(t, Persist(*entry))

	db, err := OpenDB()
	Check(t, err)
	var historyEntries []*HistoryEntry
	result := db.Find(&historyEntries)
	Check(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	if !EntryEquals(*entry, *dbEntry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v \ninput=%#v", *dbEntry, *entry)
	}
}

func TestSearch(t *testing.T) {
	defer BackupAndRestore(t)
	Check(t, Setup([]string{}))

	// Insert data
	entry1, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  "})
	Check(t, err)
	Check(t, Persist(*entry1))
	entry2, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /foo  "})
	Check(t, err)
	Check(t, Persist(*entry2))
	entry3, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  echo hi  "})
	Check(t, err)
	Check(t, Persist(*entry3))

	// Search for data
	db, err := OpenDB()
	Check(t, err)
	secret, err := GetUserSecret()
	Check(t, err)
	results, err := Search(db, secret, "ls", 5)
	Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2!", len(results))
	}
	if !EntryEquals(*results[0], *entry2) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[0], entry2)
	}
	if !EntryEquals(*results[1], *entry1) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[1], entry1)
	}
}
