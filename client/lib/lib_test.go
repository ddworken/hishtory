package lib

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	// Set env variable
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()
	os.Setenv("HISHTORY_TEST", "1")
	m.Run()
}

func requireEntriesEqual(t *testing.T, expected, actual data.HistoryEntry) {
	require.Equal(t, normalizeEntryTimezone(expected), normalizeEntryTimezone(actual))
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
	requireEntriesEqual(t, entry, *dbEntry)
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
	requireEntriesEqual(t, entry2, *results[0])
	requireEntriesEqual(t, entry1, *results[1])

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

	// A search for just a plain colon, returns an error but doesn't crash
	_, err = Search(ctx, db, ":", 5)
	require.Error(t, err)
	require.Equal(t, "search query contains malformed search atom ':'", err.Error())
	_, err = Search(ctx, db, "foo :", 5)
	require.Error(t, err)
	require.Equal(t, "search query contains malformed search atom ':'", err.Error())
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
			t.Fatalf("failure for containsUnescaped(%#v, %#v), actual=%#v", tc.input, tc.token, actual)
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
		// Basic tests
		{"foo bar", ' ', 2, []string{"foo", "bar"}},
		{"foo bar baz", ' ', 2, []string{"foo", "bar baz"}},
		{"foo bar baz", ' ', 3, []string{"foo", "bar", "baz"}},
		{"foo bar baz", ' ', 1, []string{"foo bar baz"}},
		{"foo bar baz", ' ', -1, []string{"foo", "bar", "baz"}},
		// Tests for escaping
		{"foo\\ bar baz", ' ', -1, []string{"foo bar", "baz"}},
		{"foo\\bar baz", ' ', -1, []string{"foobar", "baz"}},
		{"foo\\bar baz foob", ' ', 2, []string{"foobar", "baz foob"}},
		{"foo\\ bar\\ baz", ' ', -1, []string{"foo bar baz"}},
		{"foo\\ bar\\  baz", ' ', -1, []string{"foo bar ", "baz"}},
		// Tests for single quotes
		{"'foo bar'", ' ', -1, []string{"foo bar"}},
		{"'foo bar' '   '", ' ', -1, []string{"foo bar", "   "}},
		{"'foo bar baz' and", ' ', -1, []string{"foo bar baz", "and"}},
		{"'foo bar baz", ' ', -1, []string{"'foo", "bar", "baz"}},
		{"'foo bar baz\\''", ' ', -1, []string{"foo bar baz'"}},
		{"cwd:'foo bar :baz\\''", ':', -1, []string{"cwd", "foo bar :baz'"}},
		{"cwd:'foo bar :baz\\''", ' ', -1, []string{"cwd:foo bar \\:baz'"}},
		// Tests for double quotes
		{"\"foo bar\"", ' ', -1, []string{"foo bar"}},
		{"\"foo bar\" \"   \"", ' ', -1, []string{"foo bar", "   "}},
		{"\"foo bar baz\" and", ' ', -1, []string{"foo bar baz", "and"}},
		{"\"foo bar baz", ' ', -1, []string{"\"foo", "bar", "baz"}},
		{"\"foo bar baz\\\"\"", ' ', -1, []string{"foo bar baz\""}},
		{"cwd:\"foo bar :baz\\\"\"", ':', -1, []string{"cwd", "foo bar :baz\""}},
		{"cwd:\"foo bar :baz\\\"\"", ' ', -1, []string{"cwd:foo bar \\:baz\""}},
		// Tests for complex quotes
		{"\"foo'bar\"", ' ', -1, []string{"foo'bar"}},
		{"'foo\"bar'", ' ', -1, []string{"foo\"bar"}},
		{"\"foo'bar", ' ', -1, []string{"\"foo'bar"}},
		{"'foo\"bar", ' ', -1, []string{"'foo\"bar"}},
		{"\"foo'\\\"bar\"", ' ', -1, []string{"foo'\"bar"}},
		{"'foo\"\\'bar'", ' ', -1, []string{"foo\"'bar"}},
		{"''", ' ', -1, []string{""}},
		{"\"\"", ' ', -1, []string{""}},
		{"\\\"", ' ', -1, []string{"\""}},
		{"\\'", ' ', -1, []string{"'"}},
		// Tests the behavior of quotes with
		{"it's", ' ', -1, []string{"it's"}},
		{"'foo bar", ' ', -1, []string{"'foo", "bar"}},
		// Tests for various complex/interesting escaping
		{"ls \\-foo", ' ', -1, []string{"ls", "\\-foo"}},
		{"ls \\-foo \\a \\\\", ' ', -1, []string{"ls", "\\-foo", "a", "\\\\"}},
		{"foo:bar", ':', -1, []string{"foo", "bar"}},
		{"foo:bar", ' ', -1, []string{"foo:bar"}},
		{"foo\\:bar", ':', -1, []string{"foo\\:bar"}},
		{"foo\\:bar", ' ', -1, []string{"foo\\:bar"}},
		// Tests for quoting colons
		{"foo:bar", ' ', -1, []string{"foo:bar"}},
		{"'foo:bar'", ' ', -1, []string{"foo\\:bar"}},
		{"\"foo:bar\"", ' ', -1, []string{"foo\\:bar"}},
		// Tests for quoting dashes
		{"'-foo'", ' ', -1, []string{"\\-foo"}},
		{"'--foo'", ' ', -1, []string{"\\--foo"}},
		{"bar '--foo'", ' ', -1, []string{"bar", "\\--foo"}},
		{"bar 'foo-baz'", ' ', -1, []string{"bar", "foo-baz"}},
	}
	for _, tc := range testcases {
		actual := splitEscaped(tc.input, tc.char, tc.limit)
		if !reflect.DeepEqual(actual, tc.expected) {
			t.Fatalf("failure for splitEscaped(%#v, %#v, %#v), actual=%#v", tc.input, string(tc.char), tc.limit, actual)
		}
	}
}

func TestParseNonAtomizedToken(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
	ctx := hctx.MakeContext()

	// Default
	q, args, err := parseNonAtomizedToken(ctx, "echo hello")
	require.NoError(t, err)
	require.Equal(t, "(false OR command LIKE ? OR hostname LIKE ? OR current_working_directory LIKE ? )", q)
	require.Len(t, args, 3)
	require.Equal(t, args[0], "%echo hello%")
	require.Equal(t, args[1], "%echo hello%")
	require.Equal(t, args[2], "%echo hello%")

	// Skipping cwd
	config := hctx.GetConf(ctx)
	config.DefaultSearchColumns = []string{"command", "hostname"}
	q, args, err = parseNonAtomizedToken(ctx, "echo hello")
	require.NoError(t, err)
	require.Equal(t, "(false OR command LIKE ? OR hostname LIKE ? )", q)
	require.Len(t, args, 2)
	require.Equal(t, args[0], "%echo hello%")
	require.Equal(t, args[1], "%echo hello%")

	// Skipping cwd and hostname
	config.DefaultSearchColumns = []string{"command"}
	q, args, err = parseNonAtomizedToken(ctx, "echo hello")
	require.NoError(t, err)
	require.Equal(t, "(false OR command LIKE ? )", q)
	require.Len(t, args, 1)
	require.Equal(t, args[0], "%echo hello%")
}

func TestWhere(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
	ctx := hctx.MakeContext()
	db := hctx.GetDb(ctx)

	testcases := []struct {
		in_query       string
		in_args        []any
		expected_query string
	}{
		{"exit_code = ?", []any{1}, "SELECT * FROM `history_entries` WHERE exit_code = 1"},
		{"exit_code = ?", []any{1, nil, nil}, "SELECT * FROM `history_entries` WHERE exit_code = 1"},
		{"exit_code = ? OR exit_code = ?", []any{1, 2, nil}, "SELECT * FROM `history_entries` WHERE exit_code = 1 OR exit_code = 2"},
	}

	for _, tc := range testcases {
		tx := where(db, tc.in_query, tc.in_args...)
		queryString := tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
			var entries []data.HistoryEntry
			return tx.Find(&entries)
		})
		require.Equal(t, tc.expected_query, queryString)

	}
}

func TestIsRedactCommand(t *testing.T) {
	testcases := []struct {
		command  string
		expected bool
	}{
		// Basic redact commands
		{"hishtory redact foo", true},
		{"hishtory redact sensitive-term", true},
		{"hishtory redact", true},
		{"hishtory delete foo", true},
		{"hishtory delete sensitive-term", true},

		// With environment variables
		{"HISHTORY_REDACT_FORCE=1 hishtory redact foo", true},
		{"HISHTORY_REDACT_FORCE=true hishtory redact bar", true},
		{"FOO=bar BAZ=qux hishtory redact test", true},
		{"FOO=bar hishtory delete test", true},

		// With leading/trailing whitespace
		{"  hishtory redact foo  ", true},
		{"  HISHTORY_REDACT_FORCE=1 hishtory redact foo  ", true},
		{" hishtory delete bar ", true},

		// Non-redact commands
		{"hishtory query foo", false},
		{"hishtory export", false},
		{"hishtory status", false},
		{"echo hishtory redact", true},
		{"echo hishtory delete", true},
		{"ls /tmp", false},
		{"hishtory", false},
		{"FOO=bar echo test", false},

		// Edge cases
		{"hishtoryredact", false},
		{"hishtory  redact", true},
		{"", false},
		{"   ", false},
	}

	for _, tc := range testcases {
		actual := IsRedactCommand(tc.command)
		require.Equal(t, tc.expected, actual, "IsRedactCommand(%#v) unexpected result", tc.command)
	}
}

func TestAddToDbIfNewSkipsRedactCommands(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
	db := hctx.GetDb(hctx.MakeContext())

	// Redact commands must be omitted from history persistence
	redactEntry := testutils.MakeFakeHistoryEntry("hishtory redact sensitive_password")
	AddToDbIfNew(db, redactEntry)

	var entries []*data.HistoryEntry
	require.NoError(t, db.Find(&entries).Error)
	require.Empty(t, entries, "expected redact commands to be skipped by AddToDbIfNew")

	deleteEntry := testutils.MakeFakeHistoryEntry("hishtory delete sensitive_api_key")
	AddToDbIfNew(db, deleteEntry)

	entries = nil
	require.NoError(t, db.Find(&entries).Error)
	require.Empty(t, entries, "expected delete commands to be skipped by AddToDbIfNew")

	// Standard command must be persisted
	normalEntry := testutils.MakeFakeHistoryEntry("curl -I https://example.com")
	AddToDbIfNew(db, normalEntry)

	entries = nil
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 1)
	require.Equal(t, "curl -I https://example.com", entries[0].Command)
}

func TestImportHistorySkipsRedactCommands(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	require.NoError(t, hctx.InitConfig())
	ctx := hctx.MakeContext()
	config := hctx.GetConf(ctx)
	config.IsOffline = true
	db := hctx.GetDb(ctx)

	homedir := hctx.GetHome(ctx)
	histFile := filepath.Join(homedir, ".bash_history")
	histContent := "git status\nhishtory redact secret_token\nls -la\nhishtory delete secret_key\n"
	require.NoError(t, os.WriteFile(histFile, []byte(histContent), 0600))

	numImported, err := ImportHistory(ctx, false, true)
	require.NoError(t, err)
	require.Equal(t, 2, numImported)

	var entries []*data.HistoryEntry
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		require.False(t, IsRedactCommand(entry.Command), "redact command was saved to history: %s", entry.Command)
	}
}
