package lib

import (
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Set env variable
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()
	os.Setenv("HISHTORY_TEST", "1")
}

func TestSetup(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()

	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	require.NoError(t, Setup("", false))
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH))
	require.NoError(t, err)
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
	require.NoError(t, err)
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	require.NoError(t, Setup("", true))
	if _, err := os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH))
	require.NoError(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
	config := hctx.GetConf(hctx.MakeContext())
	if config.IsOffline != true {
		t.Fatalf("hishtory config should have been offline, actual=%#v", string(data))
	}
}
func TestPersist(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

	entry := testutils.MakeFakeHistoryEntry("ls ~/")
	require.NoError(t, db.Create(entry).Error)
	var historyEntries []*data.HistoryEntry
	result := db.Find(&historyEntries)
	require.NoError(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	require.Equal(t, entry, *dbEntry)
}

func TestSearch(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
	ctx := hctx.MakeContext()
	db := hctx.GetDb(ctx)

	// Insert data
	entry1 := testutils.MakeFakeHistoryEntry("ls /foo")
	require.NoError(t, db.Create(entry1).Error)
	entry2 := testutils.MakeFakeHistoryEntry("ls /bar")
	require.NoError(t, db.Create(entry2).Error)

	// Search for data
	results, err := Search(ctx, db, "ls", 5)
	require.NoError(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2, results=%#v", len(results), results)
	}
	require.Equal(t, entry2, *results[0])
	require.Equal(t, entry1, *results[1])

	// Search but exclude bar
	results, err = Search(ctx, db, "ls -bar", 5)
	require.NoError(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// Search but exclude foo
	results, err = Search(ctx, db, "ls -foo", 5)
	require.NoError(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// Search but include / also
	results, err = Search(ctx, db, "ls /", 5)
	require.NoError(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// Search but exclude slash
	results, err = Search(ctx, db, "ls -/", 5)
	require.NoError(t, err)
	if len(results) != 0 {
		t.Fatalf("Search() returned %d results, expected 0, results=%#v", len(results), results)
	}

	// Tests for escaping
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("ls -baz")).Error)
	results, err = Search(ctx, db, "ls", 5)
	require.NoError(t, err)
	if len(results) != 3 {
		t.Fatalf("Search() returned %d results, expected 3, results=%#v", len(results), results)
	}
	results, err = Search(ctx, db, "ls -baz", 5)
	require.NoError(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2, results=%#v", len(results), results)
	}
	results, err = Search(ctx, db, "ls \\-baz", 5)
	require.NoError(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 1, results=%#v", len(results), results)
	}

	// A malformed search query, but we should just ignore the dash since this is a common enough thing
	results, err = Search(ctx, db, "ls -", 5)
	require.NoError(t, err)
	if len(results) != 3 {
		t.Fatalf("Search() returned %d results, expected 3, results=%#v", len(results), results)
	}

	// A search for an entry containing a backslash
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("echo '\\'")).Error)
	results, err = Search(ctx, db, "\\\\", 5)
	require.NoError(t, err)
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, expected 3, results=%#v", len(results), results)
	}
}

func TestAddToDbIfNew(t *testing.T) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
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
	require.NoError(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02 T15:04:00 -08:00")
	require.NoError(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_T15:04:00_-08:00")
	require.NoError(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04:00")
	require.NoError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_T15:04:00")
	require.NoError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_15:04:00")
	require.NoError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04")
	require.NoError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_15:04")
	require.NoError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02")
	require.NoError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 0 || ts.Minute() != 0 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("1693163976")
	require.NoError(t, err)
	if ts.Year() != 2023 || ts.Month() != time.August || ts.Day() != 27 || ts.Hour() != 12 || ts.Minute() != 19 || ts.Second() != 36 {
		t.Fatalf("parsed time incorrectly: %d %s", ts.Unix(), ts.GoString())
	}
}

func TestUnescape(t *testing.T) {
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
		{"\\", ""},
		{"\\\\", "\\"},
	}
	for _, tc := range testcases {
		actual := unescape(tc.input)
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

func TestAugmentedIsOfflineError(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()
	defer testutils.BackupAndRestoreEnv("HISHTORY_SIMULATE_NETWORK_ERROR")()

	// By default, when the hishtory server is up, then IsOfflineError checks the error msg
	require.True(t, isHishtoryServerUp())
	require.False(t, IsOfflineError(fmt.Errorf("unchecked error type")))

	// When the hishtory server is down, then all error messages are treated as being due to offline errors
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	require.False(t, isHishtoryServerUp())
	require.True(t, IsOfflineError(fmt.Errorf("unchecked error type")))
}
