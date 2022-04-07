package lib

import (
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"
)

func TestSetup(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()
	homedir, err := os.UserHomeDir()
	shared.Check(t, err)
	if _, err := os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	shared.Check(t, Setup([]string{}))
	if _, err := os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	shared.Check(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
}

func TestBuildHistoryEntry(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()
	shared.Check(t, Setup([]string{}))
	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	shared.Check(t, err)
	if entry.ExitCode != 120 {
		t.Fatalf("history entry has unexpected exit code: %v", entry.ExitCode)
	}
	if entry.LocalUsername != "david" {
		t.Fatalf("history entry has unexpected user name: %v", entry.LocalUsername)
	}
	if !strings.HasPrefix(entry.CurrentWorkingDirectory, "/") && !strings.HasPrefix(entry.CurrentWorkingDirectory, "~/") {
		t.Fatalf("history entry has unexpected cwd: %v", entry.CurrentWorkingDirectory)
	}
	if entry.Command != "ls /" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if entry.StartTime.Format(time.RFC3339) != "2022-01-09T16:35:58-08:00" {
		t.Fatalf("history entry has incorrect start time: %v", entry.StartTime.Format(time.RFC3339))
	}
}

func TestGetUserSecret(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()
	shared.Check(t, Setup([]string{}))
	secret1, err := GetUserSecret()
	shared.Check(t, err)
	if len(secret1) < 10 || strings.Contains(secret1, " ") || strings.Contains(secret1, "\n") {
		t.Fatalf("unexpected secret: %v", secret1)
	}

	shared.Check(t, Setup([]string{}))
	secret2, err := GetUserSecret()
	shared.Check(t, err)

	if secret1 == secret2 {
		t.Fatalf("GetUserSecret() returned the same values for different setups! val=%v", secret1)
	}
}

func TestPersist(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	entry := shared.MakeFakeHistoryEntry("ls ~/")
	db.Create(entry)
	var historyEntries []*shared.HistoryEntry
	result := db.Find(&historyEntries)
	shared.Check(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	if !shared.EntryEquals(entry, *dbEntry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v \ninput=%#v", *dbEntry, entry)
	}
}

func TestSearch(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	// Insert data
	entry1 := shared.MakeFakeHistoryEntry("ls /foo")
	db.Create(entry1)
	entry2 := shared.MakeFakeHistoryEntry("ls /bar")
	db.Create(entry2)

	// Search for data
	results, err := shared.Search(db, "ls", 5)
	shared.Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2!", len(results))
	}
	if !shared.EntryEquals(*results[0], entry2) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[0], entry2)
	}
	if !shared.EntryEquals(*results[1], entry1) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[1], entry1)
	}
}
