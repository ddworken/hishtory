package cmd

import (
	"os"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/stretchr/testify/require"
)

func TestBuildHistoryEntry(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()
	require.NoError(t, lib.Setup("", false))

	// Test building an actual entry for bash
	entry, err := buildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "bash", "120", " 123  ls /foo  ", "1641774958"})
	require.NoError(t, err)
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
	entry, err = buildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "zsh", "120", "ls /foo\n", "1641774958"})
	require.NoError(t, err)
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
	entry, err = buildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "fish", "120", "ls /foo\n", "1641774958"})
	require.NoError(t, err)
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
	entry, err = buildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "zsh", "120", " \n", "1641774958"})
	require.NoError(t, err)
	if entry != nil {
		t.Fatalf("expected history entry to be nil")
	}
}

func TestBuildHistoryEntryWithTimestampStripping(t *testing.T) {
	defer testutils.BackupAndRestoreEnv("HISTTIMEFORMAT")()
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()
	require.NoError(t, lib.Setup("", false))

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
		require.NoError(t, hctx.SetConfig(conf))

		os.Setenv("HISTTIMEFORMAT", tc.histtimeformat)
		entry, err := buildHistoryEntry(hctx.MakeContext(), []string{"unused", "saveHistoryEntry", "bash", "120", tc.input, "1641774958"})
		require.NoError(t, err)
		if entry == nil {
			t.Fatalf("entry is unexpectedly nil")
		}
		if entry.Command != tc.expectedCommand {
			t.Fatalf("buildHistoryEntry(%#v) returned %#v (expected=%#v)", tc.input, entry.Command, tc.expectedCommand)
		}
	}
}

func TestParseCrossPlatformInt(t *testing.T) {
	res, err := parseCrossPlatformInt("123")
	require.NoError(t, err)
	if res != 123 {
		t.Fatalf("failed to parse cross platform int %d", res)
	}
	res, err = parseCrossPlatformInt("123N")
	require.NoError(t, err)
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
		require.NoError(t, err)
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
		require.NoError(t, err)
		if stripped != tc.expected {
			t.Fatalf("skipping the time prefix returned %#v (expected=%#v for %#v)", stripped, tc.expected, tc.cmdLine)
		}
	}
}
