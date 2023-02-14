package lib

import (
	"os"
	"os/user"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"
)

func TestSetup(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()

	homedir, err := os.UserHomeDir()
	testutils.Check(t, err)
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	testutils.Check(t, Setup("", false))
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH))
	testutils.Check(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
	config := hctx.GetConf(hctx.MakeContext())
	if config.IsOffline != false {
		t.Fatalf("hishtory config should have been offline")
	}
}

func TestSetupOffline(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()

	homedir, err := os.UserHomeDir()
	testutils.Check(t, err)
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	testutils.Check(t, Setup("", true))
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH))
	testutils.Check(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
	config := hctx.GetConf(hctx.MakeContext())
	if config.IsOffline != true {
		t.Fatalf("hishtory config should have been offline, actual=%#v", string(data))
	}
}

func TestBuildHistoryEntry(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()
	testutils.Check(t, Setup("", false))

	// Test building an actual entry for bash
	entry, err := BuildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "bash", "120", " 123  ls /foo  ", "1641774958"})
	testutils.Check(t, err)
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
	testutils.Check(t, err)
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

	// Test building an entry for fish
	entry, err = BuildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "fish", "120", "ls /foo\n", "1641774958"})
	testutils.Check(t, err)
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

	// Test building an entry that is empty, and thus not saved
	entry, err = BuildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "zsh", "120", " \n", "1641774958"})
	testutils.Check(t, err)
	if entry != nil {
		t.Fatalf("expected history entry to be nil")
	}
}

func TestBuildHistoryEntryWithTimestampStripping(t *testing.T) {
	defer testutils.BackupAndRestoreEnv("HISTTIMEFORMAT")()
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()
	testutils.Check(t, Setup("", false))

	testcases := []struct {
		input, histtimeformat, expectedCommand string
	}{
		{" 123  ls /foo  ", "", "ls /foo"},
		{" 2389  [2022-09-28 04:38:32 +0000] echo", "", "[2022-09-28 04:38:32 +0000] echo"},
		{" 2389  [2022-09-28 04:38:32 +0000] echo", "[%F %T %z] ", "echo"},
	}
	for _, tc := range testcases {
		conf := hctx.GetConf(hctx.MakeContext())
		conf.LastSavedHistoryLine = ""
		testutils.Check(t, hctx.SetConfig(conf))

		os.Setenv("HISTTIMEFORMAT", tc.histtimeformat)
		entry, err := BuildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "bash", "120", tc.input, "1641774958"})
		testutils.Check(t, err)
		if entry == nil {
			t.Fatalf("entry is unexpectedly nil")
		}
		if entry.Command != tc.expectedCommand {
			t.Fatalf("BuildHistoryEntry(%#v) returned %#v (expected=%#v)", tc.input, entry.Command, tc.expectedCommand)
		}
	}
}

func TestPersist(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	testutils.Check(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

	entry := testutils.MakeFakeHistoryEntry("ls ~/")
	testutils.Check(t, db.Create(entry).Error)
	var historyEntries []*data.HistoryEntry
	result := db.Find(&historyEntries)
	testutils.Check(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	if !data.EntryEquals(entry, *dbEntry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v \ninput=%#v", *dbEntry, entry)
	}
}

func TestSearch(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	testutils.Check(t, hctx.InitConfig())
	ctx := hctx.MakeContext()
	db := hctx.GetDb(ctx)

	// Insert data
	entry1 := testutils.MakeFakeHistoryEntry("ls /foo")
	testutils.Check(t, db.Create(entry1).Error)
	entry2 := testutils.MakeFakeHistoryEntry("ls /bar")
	testutils.Check(t, db.Create(entry2).Error)

	// Search for data
	results, err := Search(ctx, db, "ls", 5)
	testutils.Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2, results=%#v", len(results), results)
	}
	if !data.EntryEquals(*results[0], entry2) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[0], entry2)
	}
	if !data.EntryEquals(*results[1], entry1) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[1], entry1)
	}

	// Search but exclude bar
	results, err = Search(ctx, db, "ls -bar", 5)
	testutils.Check(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// Search but exclude foo
	results, err = Search(ctx, db, "ls -foo", 5)
	testutils.Check(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// Search but include / also
	results, err = Search(ctx, db, "ls /", 5)
	testutils.Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// Search but exclude slash
	results, err = Search(ctx, db, "ls -/", 5)
	testutils.Check(t, err)
	if len(results) != 0 {
		t.Fatalf("Search() returned %d results, expected 0, results=%#v", len(results), results)
	}

	// Tests for escaping
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("ls -baz")).Error)
	results, err = Search(ctx, db, "ls", 5)
	testutils.Check(t, err)
	if len(results) != 3 {
		t.Fatalf("Search() returned %d results, expected 3, results=%#v", len(results), results)
	}
	results, err = Search(ctx, db, "ls -baz", 5)
	testutils.Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2, results=%#v", len(results), results)
	}
	results, err = Search(ctx, db, "ls \\-baz", 5)
	testutils.Check(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}
}

func TestAddToDbIfNew(t *testing.T) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	testutils.Check(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

	// Add duplicate entries
	entry1 := testutils.MakeFakeHistoryEntry("ls /foo")
	AddToDbIfNew(db, entry1)
	AddToDbIfNew(db, entry1)
	entry2 := testutils.MakeFakeHistoryEntry("ls /foo")
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
		t.Fatalf("entries has an incorrect length: %d, entries=%#v", len(entries), entries)
	}
}

func TestParseCrossPlatformInt(t *testing.T) {
	res, err := parseCrossPlatformInt("123")
	testutils.Check(t, err)
	if res != 123 {
		t.Fatalf("failed to parse cross platform int %d", res)
	}
	res, err = parseCrossPlatformInt("123N")
	testutils.Check(t, err)
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

func TestGetLastCommand(t *testing.T) {
	testcases := []struct {
		input, expectedOutput string
	}{
		{"    0  ls", "ls"},
		{"   33  ls", "ls"},
		{"   33  ls --aaaa foo bar ", "ls --aaaa foo bar"},
		{" 2389  [2022-09-28 04:38:32 +0000] echo", "[2022-09-28 04:38:32 +0000] echo"},
	}
	for _, tc := range testcases {
		actualOutput, err := getLastCommand(tc.input)
		testutils.Check(t, err)
		if actualOutput != tc.expectedOutput {
			t.Fatalf("getLastCommand(%#v) returned %#v (expected=%#v)", tc.input, actualOutput, tc.expectedOutput)
		}
	}
}

func TestMaybeSkipBashHistTimePrefix(t *testing.T) {
	defer testutils.BackupAndRestoreEnv("HISTTIMEFORMAT")()

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
		{"[%F %T %z]", "[2022-09-28 04:17:06 +0000]foo", "foo"},
		{"[%F %T %z] ", "[2022-09-28 04:17:06 +0000] foo", "foo"},
	}

	for _, tc := range testcases {
		os.Setenv("HISTTIMEFORMAT", tc.env)
		stripped, err := maybeSkipBashHistTimePrefix(tc.cmdLine)
		testutils.Check(t, err)
		if stripped != tc.expected {
			t.Fatalf("skipping the time prefix returned %#v (expected=%#v for %#v)", stripped, tc.expected, tc.cmdLine)
		}
	}
}

func TestChunks(t *testing.T) {
	testcases := []struct {
		input     []int
		chunkSize int
		output    [][]int
	}{
		{[]int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{[]int{1, 2, 3, 4, 5}, 3, [][]int{{1, 2, 3}, {4, 5}}},
		{[]int{1, 2, 3, 4, 5}, 1, [][]int{{1}, {2}, {3}, {4}, {5}}},
		{[]int{1, 2, 3, 4, 5}, 4, [][]int{{1, 2, 3, 4}, {5}}},
	}
	for _, tc := range testcases {
		actual := shared.Chunks(tc.input, tc.chunkSize)
		if !reflect.DeepEqual(actual, tc.output) {
			t.Fatal("chunks failure")
		}
	}
}
func TestZshWeirdness(t *testing.T) {
	testcases := []struct {
		input  string
		output string
	}{
		{": 1666062975:0;bash", "bash"},
		{": 16660:0;ls", "ls"},
		{"ls", "ls"},
		{"0", "0"},
		{"hgffddxsdsrzsz xddfgdxfdv gdfc ghcvhgfcfg vgv", "hgffddxsdsrzsz xddfgdxfdv gdfc ghcvhgfcfg vgv"},
	}
	for _, tc := range testcases {
		actual := stripZshWeirdness(tc.input)
		if !reflect.DeepEqual(actual, tc.output) {
			t.Fatalf("weirdness failure for %#v", tc.input)
		}
	}
}

func TestParseTimeGenerously(t *testing.T) {
	ts, err := parseTimeGenerously("2006-01-02T15:04:00-08:00")
	testutils.Check(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02 T15:04:00 -08:00")
	testutils.Check(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_T15:04:00_-08:00")
	testutils.Check(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04:00")
	testutils.Check(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_T15:04:00")
	testutils.Check(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_15:04:00")
	testutils.Check(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04")
	testutils.Check(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_15:04")
	testutils.Check(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02")
	testutils.Check(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 0 || ts.Minute() != 0 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
}

func TestStripBackslash(t *testing.T) {
	testcases := []struct {
		input  string
		output string
	}{
		{"f bar", "f bar"},
		{"f \\bar", "f bar"},
		{"f\\:bar", "f:bar"},
		{"f\\:bar\\", "f:bar"},
		{"\\f\\:bar\\", "f:bar"},
		{"", ""},
		{"\\\\", ""},
	}
	for _, tc := range testcases {
		actual := stripBackslash(tc.input)
		if !reflect.DeepEqual(actual, tc.output) {
			t.Fatalf("unescape failure for %#v, actual=%#v", tc.input, actual)
		}
	}
}

func TestContainsUnescaped(t *testing.T) {
	testcases := []struct {
		input    string
		token    string
		expected bool
	}{
		{"f bar", "f", true},
		{"f bar", "f bar", true},
		{"f bar", "f r", false},
		{"f bar", "f ", true},
		{"foo:bar", ":", true},
		{"foo:bar", "-", false},
		{"foo\\:bar", ":", false},
		{"foo\\-bar", "-", false},
		{"foo\\-bar", "foo", true},
		{"foo\\-bar", "bar", true},
		{"foo\\-bar", "a", true},
	}
	for _, tc := range testcases {
		actual := containsUnescaped(tc.input, tc.token)
		if !reflect.DeepEqual(actual, tc.expected) {
			t.Fatalf("containsUnescaped failure for containsUnescaped(%#v, %#v), actual=%#v", tc.input, tc.token, actual)
		}
	}
}

func TestSplitEscaped(t *testing.T) {
	testcases := []struct {
		input    string
		char     rune
		limit    int
		expected []string
	}{
		{"foo bar", ' ', 2, []string{"foo", "bar"}},
		{"foo bar baz", ' ', 2, []string{"foo", "bar baz"}},
		{"foo bar baz", ' ', 3, []string{"foo", "bar", "baz"}},
		{"foo bar baz", ' ', 1, []string{"foo bar baz"}},
		{"foo bar baz", ' ', -1, []string{"foo", "bar", "baz"}},
		{"foo\\ bar baz", ' ', -1, []string{"foo\\ bar", "baz"}},
		{"foo\\bar baz", ' ', -1, []string{"foo\\bar", "baz"}},
		{"foo\\bar baz foob", ' ', 2, []string{"foo\\bar", "baz foob"}},
		{"foo\\ bar\\ baz", ' ', -1, []string{"foo\\ bar\\ baz"}},
		{"foo\\ bar\\  baz", ' ', -1, []string{"foo\\ bar\\ ", "baz"}},
	}
	for _, tc := range testcases {
		actual := splitEscaped(tc.input, tc.char, tc.limit)
		if !reflect.DeepEqual(actual, tc.expected) {
			t.Fatalf("containsUnescaped failure for splitEscaped(%#v, %#v, %#v), actual=%#v", tc.input, string(tc.char), tc.limit, actual)
		}
	}
}
