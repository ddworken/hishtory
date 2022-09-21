package lib

import (
	"os"
	"os/user"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
)

func TestSetup(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer()()

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
	defer shared.RunTestServer()()
	shared.Check(t, Setup([]string{}))

	// Test building an actual entry for bash
	entry, err := BuildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "bash", "120", " 123  ls /foo  ", "1641774958"})
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
	if !strings.HasPrefix(entry.HomeDirectory, "/") {
		t.Fatalf("history entry has unexpected home directory: %v", entry.HomeDirectory)
	}
	if entry.Command != "ls /foo" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-09T") && !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-10T") {
		t.Fatalf("history entry has incorrect date in the start time: %v", entry.StartTime.Format(time.RFC3339))
	}
	if entry.StartTime.Unix() != 1641774958 {
		t.Fatalf("history entry has incorrect Unix time in the start time: %v", entry.StartTime.Unix())
	}

	// Test building an entry for zsh
	entry, err = BuildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "zsh", "120", "ls /foo\n", "1641774958"})
	shared.Check(t, err)
	if entry.ExitCode != 120 {
		t.Fatalf("history entry has unexpected exit code: %v", entry.ExitCode)
	}
	if entry.LocalUsername != user.Username {
		t.Fatalf("history entry has unexpected user name: %v", entry.LocalUsername)
	}
	if !strings.HasPrefix(entry.CurrentWorkingDirectory, "/") && !strings.HasPrefix(entry.CurrentWorkingDirectory, "~/") {
		t.Fatalf("history entry has unexpected cwd: %v", entry.CurrentWorkingDirectory)
	}
	if !strings.HasPrefix(entry.HomeDirectory, "/") {
		t.Fatalf("history entry has unexpected home directory: %v", entry.HomeDirectory)
	}
	if entry.Command != "ls /foo" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-09T") && !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-10T") {
		t.Fatalf("history entry has incorrect date in the start time: %v", entry.StartTime.Format(time.RFC3339))
	}
	if entry.StartTime.Unix() != 1641774958 {
		t.Fatalf("history entry has incorrect Unix time in the start time: %v", entry.StartTime.Unix())
	}
}

func TestPersist(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	shared.Check(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

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
	shared.Check(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

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
	shared.Check(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

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

func TestParseCrossPlatformInt(t *testing.T) {
	res, err := parseCrossPlatformInt("123")
	if err != nil {
		t.Fatalf("failed to parse int: %v", err)
	}
	if res != 123 {
		t.Fatalf("failed to parse cross platform int %d", res)
	}
	res, err = parseCrossPlatformInt("123N")
	if err != nil {
		t.Fatalf("failed to parse int: %v", err)
	}
	if res != 123 {
		t.Fatalf("failed to parse cross platform int %d", res)
	}
}

func TestBuildRegexFromTimeFormat(t *testing.T) {
	testcases := []struct {
		formatString, regex string
	}{
		{"%Y ", "[0-9]{4} "},
		{"%F %T ", "[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} "},
		{"%F%T", "[0-9]{4}-[0-9]{2}-[0-9]{2}[0-9]{2}:[0-9]{2}:[0-9]{2}"},
		{"%%", "%"},
		{"%%%%", "%%"},
		{"%%%Y", "%[0-9]{4}"},
		{"%%%F%T", "%[0-9]{4}-[0-9]{2}-[0-9]{2}[0-9]{2}:[0-9]{2}:[0-9]{2}"},
	}

	for _, tc := range testcases {
		if regex := buildRegexFromTimeFormat(tc.formatString); regex != tc.regex {
			t.Fatalf("building a regex for %#v returned %#v (expected=%#v)", tc.formatString, regex, tc.regex)
		}
	}
}

func TestMaybeSkipBashHistTimePrefix(t *testing.T) {
	defer shared.BackupAndRestoreEnv("HISTTIMEFORMAT")()

	testcases := []struct {
		env, cmdLine, expected string
	}{
		{"%F %T ", "2019-07-12 13:02:31 sudo apt update", "sudo apt update"},
		{"%F %T ", "2019-07-12 13:02:31 ls a b", "ls a b"},
		{"%F %T ", "2019-07-12 13:02:31 ls a ", "ls a "},
		{"%F %T ", "2019-07-12 13:02:31 ls a", "ls a"},
		{"%F %T ", "2019-07-12 13:02:31 ls", "ls"},
		{"%F %T ", "2019-07-12 13:02:31 ls -Slah", "ls -Slah"},
		{"%F ", "2019-07-12 ls -Slah", "ls -Slah"},
		{"%F  ", "2019-07-12  ls -Slah", "ls -Slah"},
		{"%Y", "2019ls -Slah", "ls -Slah"},
		{"%Y%Y", "20192020ls -Slah", "ls -Slah"},
		{"%Y%Y", "20192020ls -Slah20192020", "ls -Slah20192020"},
		{"", "ls -Slah", "ls -Slah"},
		{"[%F %T] ", "[2019-07-12 13:02:31] sudo apt update", "sudo apt update"},
		{"[%F a %T] ", "[2019-07-12 a 13:02:31] sudo apt update", "sudo apt update"},
		{"aaa ", "aaa sudo apt update", "sudo apt update"},
		{"%c ", "Sun Aug 19 02:56:02 2012 sudo apt update", "sudo apt update"},
		{"%c ", "Sun Aug 19 02:56:02 2012 ls", "ls"},
		{"[%c] ", "[Sun Aug 19 02:56:02 2012] ls", "ls"},
		{"[%c %t] ", "[Sun Aug 19 02:56:02 2012 	] ls", "ls"},
		{"[%c %t]", "[Sun Aug 19 02:56:02 2012 	]ls", "ls"},
		{"[%c %t]", "[Sun Aug 19 02:56:02 2012 	]foo", "foo"},
		{"[%c %t", "[Sun Aug 19 02:56:02 2012 	foo", "foo"},
	}

	for _, tc := range testcases {
		os.Setenv("HISTTIMEFORMAT", tc.env)
		stripped, err := maybeSkipBashHistTimePrefix(tc.cmdLine)
		if err != nil {
			t.Fatal(err)
		}
		if stripped != tc.expected {
			t.Fatalf("skipping the time prefix returned %#v (expected=%#v for %#v)", stripped, tc.expected, tc.cmdLine)
		}
	}
}
