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
