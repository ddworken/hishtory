package lib

import (
	"os"
	"os/user"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
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

	// Test building an actual entry
	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	shared.Check(t, err)
	if entry.ExitCode != 120 {
		t.Fatalf("history entry has unexpected exit code: %v", entry.ExitCode)
	}
	user, err := user.Current()
	if err != nil {
		t.Fatalf("failed to retrieve user: %v", err)
	}
	if entry.LocalUsername != user.Username {
		t.Fatalf("history entry has unexpected user name: %v", entry.LocalUsername)
	}
	if !strings.HasPrefix(entry.CurrentWorkingDirectory, "/") && !strings.HasPrefix(entry.CurrentWorkingDirectory, "~/") {
		t.Fatalf("history entry has unexpected cwd: %v", entry.CurrentWorkingDirectory)
	}
	if entry.Command != "ls /" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-09T") && !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-10T") {
		t.Fatalf("history entry has incorrect date in the start time: %v", entry.StartTime.Format(time.RFC3339))
	}
	if entry.StartTime.Unix() != 1641774958 {
		t.Fatalf("history entry has incorrect Unix time in the start time: %v", entry.StartTime.Unix())
	}

	// Test the behavior when the command is prefixed with a space
	// TODO: write a test for this
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

	entry := data.MakeFakeHistoryEntry("ls ~/")
	db.Create(entry)
	var historyEntries []*data.HistoryEntry
	result := db.Find(&historyEntries)
	shared.Check(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	if !data.EntryEquals(entry, *dbEntry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v \ninput=%#v", *dbEntry, entry)
	}
}

func TestSearch(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	// Insert data
	entry1 := data.MakeFakeHistoryEntry("ls /foo")
	db.Create(entry1)
	entry2 := data.MakeFakeHistoryEntry("ls /bar")
	db.Create(entry2)

	// Search for data
	results, err := data.Search(db, "ls", 5)
	shared.Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2!", len(results))
	}
	if !data.EntryEquals(*results[0], entry2) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[0], entry2)
	}
	if !data.EntryEquals(*results[1], entry1) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[1], entry1)
	}
}

func TestAddToDbIfNew(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	// Add duplicate entries
	entry1 := data.MakeFakeHistoryEntry("ls /foo")
	AddToDbIfNew(db, entry1)
	AddToDbIfNew(db, entry1)
	entry2 := data.MakeFakeHistoryEntry("ls /foo")
	AddToDbIfNew(db, entry2)
	AddToDbIfNew(db, entry2)
	AddToDbIfNew(db, entry1)

	// Check there should only be two entries
	var entries []data.HistoryEntry
	result := db.Find(&entries)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if len(entries) != 2 {
		t.Fatalf("entries has an incorrect length: %d", len(entries))
	}
}

// TODO: Some day find a way of testing the table display
