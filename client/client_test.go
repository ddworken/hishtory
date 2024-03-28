package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ddworken/hishtory/client/cmd"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/ai"
	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/stretchr/testify/require"
)

func skipSlowTests() bool {
	return os.Getenv("FAST") != ""
}

func TestMain(m *testing.M) {
	// Configure key environment variables
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()
	os.Setenv("HISHTORY_TEST", "1")
	defer testutils.BackupAndRestoreEnv("HISHTORY_SKIP_INIT_IMPORT")()
	os.Setenv("HISHTORY_SKIP_INIT_IMPORT", "1")

	// Start the test server
	defer testutils.RunTestServer()()

	// Build the client so it is available in /tmp/client
	cmd := exec.Command("go", "build", "-o", "/tmp/client")
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	err := cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("failed to build client: %v", err))
	}

	// Start the tests
	m.Run()
}

var shellTesters []shellTester = []shellTester{bashTester{}, zshTester{}}

func TestParam(t *testing.T) {
	if skipSlowTests() {
		shellTesters = shellTesters[:1]
	}
	for _, tester := range shellTesters {
		t.Run("testRepeatedCommandThenQuery/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testRepeatedCommandThenQuery(t, tester) }))
		t.Run("testRepeatedCommandAndQuery/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testRepeatedCommandAndQuery(t, tester) }))
		t.Run("testRepeatedEnableDisable/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testRepeatedEnableDisable(t, tester) }))
		t.Run("testExcludeHiddenCommand/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testExcludeHiddenCommand(t, tester) }))
		t.Run("testUpdate/head->release/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testUpdateFromHeadToRelease(t, tester) }))
		t.Run("testUpdate/prev->release/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testUpdateFromPrevToRelease(t, tester) }))
		t.Run("testUpdate/prev->release/prod/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testUpdateFromPrevToReleaseViaProd(t, tester) }))
		t.Run("testUpdate/prev->current/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testUpdateFromPrevToCurrent(t, tester) }))
		t.Run("testAdvancedQuery/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testAdvancedQuery(t, tester) }))
		t.Run("testIntegration/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testIntegration(t, tester, Online) }))
		t.Run("testIntegration/offline/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testIntegration(t, tester, Offline) }))
		t.Run("testIntegrationWithNewDevice/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testIntegrationWithNewDevice(t, tester) }))
		t.Run("testHishtoryBackgroundSaving/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testHishtoryBackgroundSaving(t, tester) }))
		t.Run("testDisplayTable/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testDisplayTable(t, tester) }))
		t.Run("testTableDisplayCwd/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testTableDisplayCwd(t, tester) }))
		t.Run("testTimestampsAreReasonablyCorrect/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testTimestampsAreReasonablyCorrect(t, tester) }))
		t.Run("testRequestAndReceiveDbDump/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testRequestAndReceiveDbDump(t, tester) }))
		t.Run("testInstallViaPythonScript/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testInstallViaPythonScript(t, tester) }))
		t.Run("testExportWithQuery/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testExportWithQuery(t, tester) }))
		t.Run("testHelpCommand/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testHelpCommand(t, tester) }))
		t.Run("testReuploadHistoryEntries/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testReuploadHistoryEntries(t, tester) }))
		t.Run("testHishtoryOffline/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testHishtoryOffline(t, tester) }))
		t.Run("testInitialHistoryImport/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testInitialHistoryImport(t, tester) }))
		t.Run("testLocalRedaction/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testLocalRedaction(t, tester, Online) }))
		t.Run("testLocalRedaction/offline/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testLocalRedaction(t, tester, Offline) }))
		t.Run("testRemoteRedaction/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testRemoteRedaction(t, tester) }))
		t.Run("testMultipleUsers/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testMultipleUsers(t, tester) }))
		t.Run("testConfigGetSet/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testConfigGetSet(t, tester) }))
		t.Run("testHandleUpgradedFeatures/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testHandleUpgradedFeatures(t, tester) }))
		t.Run("testCustomColumns/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testCustomColumns(t, tester) }))
		t.Run("testUninstall/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testUninstall(t, tester) }))
		t.Run("testPresaving/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testPresaving(t, tester, tester.ShellName()) }))
		t.Run("testPresavingOffline/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testPresavingOffline(t, tester) }))
		t.Run("testPresavingDisabled/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testPresavingDisabled(t, tester) }))
		t.Run("testControlR/online/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testControlR(t, tester, tester.ShellName(), Online) }))
		t.Run("testControlR/offline/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testControlR(t, tester, tester.ShellName(), Offline) }))
		t.Run("testTabCompletion/"+tester.ShellName(), wrapTestForSharding(func(t *testing.T) { testTabCompletion(t, tester, tester.ShellName()) }))
	}
	t.Run("testTabCompletion/fish", wrapTestForSharding(func(t *testing.T) { testTabCompletion(t, zshTester{}, "fish") }))
	t.Run("testPresaving/fish", wrapTestForSharding(func(t *testing.T) { testPresaving(t, zshTester{}, "fish") }))
	t.Run("testControlR/fish", wrapTestForSharding(func(t *testing.T) { testControlR(t, bashTester{}, "fish", Online) }))
	t.Run("testTui/search/online", wrapTestForSharding(func(t *testing.T) { testTui_search(t, Online) }))
	t.Run("testTui/search/offline", wrapTestForSharding(func(t *testing.T) { testTui_search(t, Offline) }))
	t.Run("testTui/general/online", wrapTestForSharding(func(t *testing.T) { testTui_general(t, Online) }))
	t.Run("testTui/general/offline", wrapTestForSharding(func(t *testing.T) { testTui_general(t, Offline) }))
	t.Run("testTui/scroll", wrapTestForSharding(testTui_scroll))
	t.Run("testTui/resize", wrapTestForSharding(testTui_resize))
	t.Run("testTui/delete", wrapTestForSharding(testTui_delete))
	t.Run("testTui/color", wrapTestForSharding(testTui_color))
	t.Run("testTui/errors", wrapTestForSharding(testTui_errors))
	t.Run("testTui/ai", wrapTestForSharding(testTui_ai))
	t.Run("testTui/defaultFilter", wrapTestForSharding(testTui_defaultFilter))

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func testIntegration(t *testing.T, tester shellTester, onlineStatus OnlineStatus) {
	// Set up
	defer testutils.BackupAndRestore(t)()

	// Run the test
	testBasicUserFlow(t, tester, onlineStatus)
}

func testIntegrationWithNewDevice(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()

	// Run the test
	userSecret := testBasicUserFlow(t, tester, Online)

	// Install it again
	testutils.ResetLocalState(t)
	installHishtory(t, tester, userSecret)

	// Querying should show the history from the previous run
	out := tester.RunInteractiveShell(t, `hishtory query`)
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		require.Contains(t, out, item)
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}

	tester.RunInteractiveShell(t, "echo mynewcommand")
	out = hishtoryQuery(t, tester, "")
	require.Contains(t, out, "echo mynewcommand")
	if strings.Count(out, "echo mynewcommand") != 1 {
		t.Fatalf("output has `echo mynewcommand` the wrong number of times")
	}

	// Install it a 3rd time
	testutils.ResetLocalState(t)
	installHishtory(t, tester, "adifferentsecret")

	// Run a command that shouldn't be in the hishtory later on
	tester.RunInteractiveShell(t, `echo notinthehistory`)
	out = hishtoryQuery(t, tester, "")
	require.Contains(t, out, "echo notinthehistory")
	require.NotContains(t, out, "mynewcommand")
	require.NotContains(t, out, "thisisrecorded")

	// Set the secret key to the previous secret key
	out, err := tester.RunInteractiveShellRelaxed(t, ` export HISHTORY_SKIP_INIT_IMPORT=1
yes | hishtory init `+userSecret)
	require.NoError(t, err)
	require.Contains(t, out, "Setting secret hishtory key to "+userSecret, "Failed to re-init with the user secret")

	// Querying shouldn't show the entry from the previous account
	out = hishtoryQuery(t, tester, "")
	require.NotContains(t, out, "notinthehistory", "output contains the unexpected item: notinthehistory")

	// And it should show the history from the previous run on this account
	expected = []string{"echo thisisrecorded", "echo mynewcommand", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		require.Contains(t, out, item, "output is missing expected item")
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}

	tester.RunInteractiveShell(t, "echo mynewercommand")
	out = hishtoryQuery(t, tester, "")
	require.Contains(t, out, "echo mynewercommand")
	if strings.Count(out, "echo mynewercommand") != 1 {
		t.Fatalf("output has `echo mynewercommand` the wrong number of times")
	}

	// Manually submit an event that isn't in the local DB, and then we'll
	// check if we see it when we do a query without ever having done an init
	newEntry := testutils.MakeFakeHistoryEntry("othercomputer")
	newEntry.StartTime = time.Now()
	newEntry.EndTime = time.Now()
	manuallySubmitHistoryEntry(t, userSecret, newEntry)

	// Now check if that is in there when we do hishtory query
	out = hishtoryQuery(t, tester, "")
	require.Contains(t, out, "othercomputer", "hishtory query doesn't contain cmd run on another machine")

	// Run a reupload just to test that flow
	tester.RunInteractiveShell(t, "hishtory reupload")

	// Finally, test the export command
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail | grep -v '/tmp/client install'`)
	require.NotContains(t, out, "thisisnotrecorded", "hishtory export contains a command that should not have been recorded")
	expectedOutputWithoutKey := "hishtory status\nhishtory query\nls /a\nls /bar\nls /foo\necho foo\necho bar\nhishtory disable\nhishtory enable\necho thisisrecorded\nhishtory query\nhishtory query foo\necho hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\" # mycommand\nhishtory query complex\nhishtory query\necho mynewcommand\nhishtory query\nyes | hishtory init %s\nhishtory query\necho mynewercommand\nhishtory query\nothercomputer\nhishtory query\nhishtory reupload\n"
	expectedOutput := fmt.Sprintf(expectedOutputWithoutKey, userSecret)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// And test the export for each shell without anything filtered out
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail | grep -v 'hishtory init '`)
	testutils.CompareGoldens(t, out, "testIntegrationWithNewDevice-"+tester.ShellName())

	// And test the table but with a subset of columns that is static
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns Hostname 'Exit Code' Command`)
	out = tester.RunInteractiveShell(t, `hishtory query -pipefail | grep -v 'hishtory init ' | grep -v 'ls /'`)
	testutils.CompareGoldens(t, out, "testIntegrationWithNewDevice-table"+tester.ShellName())

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func installWithOnlineStatus(t testing.TB, tester shellTester, onlineStatus OnlineStatus) string {
	if onlineStatus == Online {
		return installHishtory(t, tester, "")
	} else {
		return installHishtory(t, tester, "--offline")
	}
}

func testBasicUserFlow(t *testing.T, tester shellTester, onlineStatus OnlineStatus) string {
	// Test install
	userSecret := installWithOnlineStatus(t, tester, onlineStatus)
	assertOnlineStatus(t, onlineStatus)

	// Test the status subcommand
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Assert that hishtory is correctly using the dev config.sh
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	dat, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"))
	require.NoError(t, err, "failed to read config.sh")
	require.NotContains(t, string(dat), "# Background Run", "config.sh is the prod version when it shouldn't be")

	// Test the banner
	if onlineStatus == Online {
		os.Setenv("FORCED_BANNER", "HELLO_FROM_SERVER")
		defer os.Setenv("FORCED_BANNER", "")
		out = hishtoryQuery(t, tester, "")
		require.Contains(t, out, "HELLO_FROM_SERVER\nHostname", "hishtory query didn't show the banner message")
		os.Setenv("FORCED_BANNER", "")
	}

	// Test recording commands
	out, err = tester.RunInteractiveShellRelaxed(t, `ls /a
ls /bar
ls /foo
echo foo
echo bar
hishtory disable
echo thisisnotrecorded
sleep 0.5
hishtory enable
echo thisisrecorded`)
	require.NoError(t, err)
	if out != "foo\nbar\nthisisnotrecorded\nthisisrecorded\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		require.Contains(t, out, item, "output is missing expected item")
	}

	// Test the actual table output
	hostnameMatcher := `\S+`
	tableDividerMatcher := `\s+`
	pathMatcher := `~?/[a-zA-Z_0-9/-]+`
	datetimeMatcher := `[a-zA-Z]{3}\s\d{1,2}\s\d{4}\s[0-9:]+\s([A-Z]{3}|[+-]\d{4})`
	runtimeMatcher := `[0-9.ms]+`
	exitCodeMatcher := `0`
	pipefailMatcher := `set -em?o pipefail`
	line1Matcher := `Hostname` + tableDividerMatcher + `CWD` + tableDividerMatcher + `Timestamp` + tableDividerMatcher + `Runtime` + tableDividerMatcher + `Exit Code` + tableDividerMatcher + `Command\s*\n`
	line2Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + `N/A` + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + `hishtory query` + tableDividerMatcher + `\n`
	line3Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + runtimeMatcher + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + pipefailMatcher + tableDividerMatcher + `\n`
	line4Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + runtimeMatcher + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + `echo thisisrecorded` + tableDividerMatcher + `\n`
	require.Regexp(t, regexp.MustCompile(line1Matcher), out)
	require.Regexp(t, regexp.MustCompile(line2Matcher), out)
	require.Regexp(t, regexp.MustCompile(line3Matcher), out)
	require.Regexp(t, regexp.MustCompile(line4Matcher), out)
	require.Regexp(t, regexp.MustCompile(line1Matcher+line2Matcher+line3Matcher+line4Matcher), out)

	// Test querying for a specific command
	out = hishtoryQuery(t, tester, "foo")
	expected = []string{"echo foo", "ls /foo"}
	unexpected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "ls /bar", "ls /a"}
	for _, item := range expected {
		require.Contains(t, out, item, "output is missing expected item")
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}
	for _, item := range unexpected {
		require.NotContains(t, out, item, "output is containing unexpected item")
	}

	// Add a complex command
	complexCommand := "echo hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\" # mycommand"
	_, _ = tester.RunInteractiveShellRelaxed(t, complexCommand)

	// Query for it
	out = hishtoryQuery(t, tester, "complex")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	require.Contains(t, out, complexCommand, "hishtory query doesn't contain the expected complex command")
	require.Contains(t, out, "hishtory query complex")

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)

	return userSecret
}

func testAdvancedQuery(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()

	// Install hishtory
	userSecret := installHishtory(t, tester, "")

	// Run some commands we can query for
	_, err := tester.RunInteractiveShellRelaxed(t, `echo nevershouldappear
notacommand
cd /tmp/
echo querybydir
hishtory disable`)
	require.NoError(t, err)

	// A super basic query just to ensure the basics are working
	out := hishtoryQuery(t, tester, `echo`)
	require.Contains(t, out, "echo querybydir")
	require.Contains(t, out, "echo nevershouldappear")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd
	out = hishtoryQuery(t, tester, `cwd:/tmp`)
	require.Contains(t, out, "echo querybydir", "hishtory query doesn't contain result matching cwd:/tmp")
	require.NotContains(t, out, "nevershouldappear")
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	// And again, but with a strailing slash
	out = hishtoryQuery(t, tester, `cwd:/tmp/`)
	require.Contains(t, out, "echo querybydir", "hishtory query doesn't contain result matching cwd:/tmp/")
	require.NotContains(t, out, "nevershouldappear")
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd without the slash
	out = hishtoryQuery(t, tester, `cwd:tmp`)
	require.Contains(t, out, "echo querybydir")
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd and another term
	out = hishtoryQuery(t, tester, `cwd:/tmp querybydir`)
	require.Contains(t, out, "echo querybydir")
	require.NotContains(t, out, "nevershouldappear")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on exit_code
	out = hishtoryQuery(t, tester, `exit_code:127`)
	require.Contains(t, out, "notacommand")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on exit_code and something else that matches nothing
	out = hishtoryQuery(t, tester, `exit_code:127 foo`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on before: and cwd:
	out = hishtoryQuery(t, tester, `before:2125-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = hishtoryQuery(t, tester, `before:2125-07-02 cwd:tmp`)
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = hishtoryQuery(t, tester, `before:2125-07-02 cwd:mp`)
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: and cwd:
	out = hishtoryQuery(t, tester, `after:1980-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: that returns no results
	out = hishtoryQuery(t, tester, `after:2120-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Manually submit an entry with a different hostname and username so we can test those atoms
	entry := testutils.MakeFakeHistoryEntry("cmd_with_diff_hostname_and_username")
	entry.LocalUsername = "otheruser"
	entry.Hostname = "otherhostname"
	manuallySubmitHistoryEntry(t, userSecret, entry)

	// Query based on the username that exists
	out = hishtoryQuery(t, tester, `user:otheruser`)
	require.Contains(t, out, "cmd_with_diff_hostname_and_username")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on the username that doesn't exist
	out = hishtoryQuery(t, tester, `user:noexist`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on the hostname
	out = hishtoryQuery(t, tester, `hostname:otherhostname`)
	require.Contains(t, out, "cmd_with_diff_hostname_and_username")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item
	out = hishtoryQuery(t, tester, "")
	require.Contains(t, out, "cmd_with_diff_hostname_and_username")
	out = hishtoryQuery(t, tester, `-cmd_with_diff_hostname_and_username`)
	require.NotContains(t, out, "cmd_with_diff_hostname_and_username")
	out = hishtoryQuery(t, tester, `-echo -pipefail`)
	require.NotContains(t, out, "echo")
	require.NotContains(t, out, "pipefail")
	require.Contains(t, out, "cmd_with_diff_hostname_and_username")
	if strings.Count(out, "\n") != 6 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out with an atom
	out = hishtoryQuery(t, tester, `-hostname:otherhostname`)
	require.NotContains(t, out, "cmd_with_diff_hostname_and_username")
	out = hishtoryQuery(t, tester, `-user:otheruser`)
	require.NotContains(t, out, "cmd_with_diff_hostname_and_username")
	out = hishtoryQuery(t, tester, `-exit_code:0`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item that also looks like it could be a search for a flag
	entry = testutils.MakeFakeHistoryEntry("foo -echo")
	manuallySubmitHistoryEntry(t, userSecret, entry)
	out = hishtoryQuery(t, tester, `-echo -install -pipefail`)
	require.NotContains(t, out, "echo")
	if strings.Count(out, "\n") != 6 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Search for a cwd based on the home directory
	entry = testutils.MakeFakeHistoryEntry("foobar")
	entry.HomeDirectory = "/home/david/"
	entry.CurrentWorkingDirectory = "~/dir/"
	manuallySubmitHistoryEntry(t, userSecret, entry)
	out = tester.RunInteractiveShell(t, `hishtory export cwd:~/dir`)
	expectedOutput := "foobar\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// And search with the fully expanded path
	out = tester.RunInteractiveShell(t, `hishtory export cwd:/home/david/dir`)
	expectedOutput = "foobar\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Search using an escaped dash
	out = tester.RunInteractiveShell(t, `hishtory export \\-echo`)
	expectedOutput = "foo -echo\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Search using a colon that doesn't match a column name
	manuallySubmitHistoryEntry(t, userSecret, testutils.MakeFakeHistoryEntry("foo:bar"))
	out = tester.RunInteractiveShell(t, `hishtory export foo\\:bar`)
	expectedOutput = "foo:bar\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func installFromHead(t *testing.T, tester shellTester) (string, string) {
	return installHishtory(t, tester, ""), "v0.Unknown"
}

func installFromPrev(t *testing.T, tester shellTester) (string, string) {
	defer testutils.BackupAndRestoreEnv("HISHTORY_FORCE_CLIENT_VERSION")()
	dd, err := cmd.GetDownloadData(makeTestOnlyContextWithFakeConfig())
	require.NoError(t, err)
	pv, err := shared.ParseVersionString(dd.Version)
	require.NoError(t, err)
	previousVersion := pv.Decrement()
	os.Setenv("HISHTORY_FORCE_CLIENT_VERSION", previousVersion.String())
	userSecret := installHishtory(t, tester, "")
	out := tester.RunInteractiveShell(t, ` hishtory update`)
	require.Regexp(t, regexp.MustCompile(`^Successfully updated hishtory from v0[.]Unknown to `+previousVersion.String()+`\n$`), out)
	return userSecret, previousVersion.String()
}

func updateToRelease(t *testing.T, tester shellTester) string {
	dd, err := cmd.GetDownloadData(makeTestOnlyContextWithFakeConfig())
	require.NoError(t, err)

	// Update
	out := tester.RunInteractiveShell(t, " hishtory update\necho postupdate")
	require.Regexp(t, regexp.MustCompile(`^Successfully updated hishtory from v0[.][a-zA-Z0-9]+ to `+dd.Version+`\npostupdate\n$`), out)
	require.NotContains(t, out, "skipping SLSA validation")

	// Update again and assert that it skipped the update
	out = tester.RunInteractiveShell(t, " hishtory update")
	require.Equal(t, fmt.Sprintf("Latest version (%s) is already installed\n", dd.Version), out)

	return dd.Version
}

func updateToHead(t *testing.T, tester shellTester) string {
	out := tester.RunInteractiveShell(t, " /tmp/client install\necho postupdate")
	require.Equal(t, "postupdate\n", out)
	return "v0.Unknown"
}

func testUpdateFromHeadToRelease(t *testing.T, tester shellTester) {
	testGenericUpdate(t, tester, installFromHead, updateToRelease)
}

func testUpdateFromPrevToRelease(t *testing.T, tester shellTester) {
	testGenericUpdate(t, tester, installFromPrev, updateToRelease)
}

func testUpdateFromPrevToCurrent(t *testing.T, tester shellTester) {
	testGenericUpdate(t, tester, installFromPrev, updateToHead)
}

func testUpdateFromPrevToReleaseViaProd(t *testing.T, tester shellTester) {
	defer testutils.BackupAndRestoreEnv("HISHTORY_SERVER")()
	os.Setenv("HISHTORY_SERVER", "https://api.hishtory.dev")
	testGenericUpdate(t, tester, installFromPrev, updateToRelease)
}

func testGenericUpdate(t *testing.T, tester shellTester, installInitialVersion func(*testing.T, shellTester) (string, string), installUpdatedVersion func(*testing.T, shellTester) string) {
	defer testutils.BackupAndRestoreEnv("HISHTORY_FORCE_CLIENT_VERSION")()
	if !testutils.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		t.Skip("skipping on linux/arm64 which is unsupported")
	}
	// Set up
	defer testutils.BackupAndRestore(t)()
	userSecret, initialVersion := installInitialVersion(t, tester)

	// Record a command before the update
	tester.RunInteractiveShell(t, "echo hello")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	require.Contains(t, out, fmt.Sprintf("hiSHtory: %s\nEnabled: true\nSecret Key: %s\nCommit Hash: ", initialVersion, userSecret))
	if initialVersion == "v0.Unknown" {
		require.Contains(t, out, "Commit Hash: Unknown")
	} else {
		require.NotContains(t, out, "Commit Hash: Unknown")
	}

	// Update
	updatedVersion := installUpdatedVersion(t, tester)

	// Then check the status command again to confirm the update worked
	out = tester.RunInteractiveShell(t, `hishtory status`)
	require.Contains(t, out, fmt.Sprintf("\nEnabled: true\nSecret Key: %s\nCommit Hash: ", userSecret))
	if updatedVersion != "v0.Unknown" {
		require.NotContains(t, out, "\nCommit Hash: Unknown\n")
	}

	// Check that the history was preserved after the update
	out = tester.RunInteractiveShell(t, "hishtory export -pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo hello\nhishtory status\necho postupdate\nhishtory status\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testRepeatedCommandThenQuery(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Run a command many times
	for i := 0; i < 25; i++ {
		tester.RunInteractiveShell(t, fmt.Sprintf("echo mycommand-%d", i))
	}

	// Check that it shows up correctly
	out = hishtoryQuery(t, tester, `mycommand`)
	if strings.Count(out, "\n") != 26 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	if strings.Count(out, "echo mycommand") != 24 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}
	require.Contains(t, out, "hishtory query mycommand")

	// Run a few more commands including some empty lines that don't get recorded
	tester.RunInteractiveShell(t, `echo mycommand-30


echo mycommand-31
echo mycommand-3`)

	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail | grep -v '/tmp/client install'")
	expectedOutput := "hishtory status\necho mycommand-0\necho mycommand-1\necho mycommand-2\necho mycommand-3\necho mycommand-4\necho mycommand-5\necho mycommand-6\necho mycommand-7\necho mycommand-8\necho mycommand-9\necho mycommand-10\necho mycommand-11\necho mycommand-12\necho mycommand-13\necho mycommand-14\necho mycommand-15\necho mycommand-16\necho mycommand-17\necho mycommand-18\necho mycommand-19\necho mycommand-20\necho mycommand-21\necho mycommand-22\necho mycommand-23\necho mycommand-24\nhishtory query mycommand\necho mycommand-30\necho mycommand-31\necho mycommand-3\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testRepeatedCommandAndQuery(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Run a command many times
	for i := 0; i < 25; i++ {
		tester.RunInteractiveShell(t, fmt.Sprintf("echo mycommand-%d", i))
		out = hishtoryQuery(t, tester, fmt.Sprintf("mycommand-%d", i))
		if strings.Count(out, "\n") != 3 {
			t.Fatalf("hishtory query #%d has the wrong number of lines=%d, out=%#v", i, strings.Count(out, "\n"), out)
		}
		if strings.Count(out, "echo mycommand") != 1 {
			t.Fatalf("hishtory query #%d has the wrong number of commands=%d, out=%#v", i, strings.Count(out, "echo mycommand"), out)
		}
		require.Contains(t, out, "hishtory query mycommand-")
	}
}

func testRepeatedEnableDisable(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Run a command many times
	for i := 0; i < 25; i++ {
		tester.RunInteractiveShell(t, fmt.Sprintf(`echo mycommand-%d
 hishtory disable
echo shouldnotshowup
sleep 0.5
hishtory enable`, i))
		out := hishtoryQuery(t, tester, fmt.Sprintf("mycommand-%d", i))
		if strings.Count(out, "\n") != 3 {
			t.Fatalf("hishtory query #%d has the wrong number of lines=%d, out=%#v", i, strings.Count(out, "\n"), out)
		}
		if strings.Count(out, "echo mycommand") != 1 {
			t.Fatalf("hishtory query #%d has the wrong number of commands=%d, out=%#v", i, strings.Count(out, "echo mycommand"), out)
		}
		require.Contains(t, out, "hishtory query mycommand-")
		out = hishtoryQuery(t, tester, "")
		require.NotContains(t, out, "shouldnotshowup")
	}

	out := tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo mycommand-0\nhishtory enable\nhishtory query mycommand-0\nhishtory query\necho mycommand-1\nhishtory enable\nhishtory query mycommand-1\nhishtory query\necho mycommand-2\nhishtory enable\nhishtory query mycommand-2\nhishtory query\necho mycommand-3\nhishtory enable\nhishtory query mycommand-3\nhishtory query\necho mycommand-4\nhishtory enable\nhishtory query mycommand-4\nhishtory query\necho mycommand-5\nhishtory enable\nhishtory query mycommand-5\nhishtory query\necho mycommand-6\nhishtory enable\nhishtory query mycommand-6\nhishtory query\necho mycommand-7\nhishtory enable\nhishtory query mycommand-7\nhishtory query\necho mycommand-8\nhishtory enable\nhishtory query mycommand-8\nhishtory query\necho mycommand-9\nhishtory enable\nhishtory query mycommand-9\nhishtory query\necho mycommand-10\nhishtory enable\nhishtory query mycommand-10\nhishtory query\necho mycommand-11\nhishtory enable\nhishtory query mycommand-11\nhishtory query\necho mycommand-12\nhishtory enable\nhishtory query mycommand-12\nhishtory query\necho mycommand-13\nhishtory enable\nhishtory query mycommand-13\nhishtory query\necho mycommand-14\nhishtory enable\nhishtory query mycommand-14\nhishtory query\necho mycommand-15\nhishtory enable\nhishtory query mycommand-15\nhishtory query\necho mycommand-16\nhishtory enable\nhishtory query mycommand-16\nhishtory query\necho mycommand-17\nhishtory enable\nhishtory query mycommand-17\nhishtory query\necho mycommand-18\nhishtory enable\nhishtory query mycommand-18\nhishtory query\necho mycommand-19\nhishtory enable\nhishtory query mycommand-19\nhishtory query\necho mycommand-20\nhishtory enable\nhishtory query mycommand-20\nhishtory query\necho mycommand-21\nhishtory enable\nhishtory query mycommand-21\nhishtory query\necho mycommand-22\nhishtory enable\nhishtory query mycommand-22\nhishtory query\necho mycommand-23\nhishtory enable\nhishtory query mycommand-23\nhishtory query\necho mycommand-24\nhishtory enable\nhishtory query mycommand-24\nhishtory query\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testExcludeHiddenCommand(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	tester.RunInteractiveShell(t, `echo hello1
 echo hidden
echo hello2
 echo hidden`)
	tester.RunInteractiveShell(t, " echo hidden")
	out := hishtoryQuery(t, tester, "-pipefail")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v, bash hishtory file=%#v", strings.Count(out, "\n"), out, tester.RunInteractiveShell(t, "cat ~/.bash_history"))
	}
	if strings.Count(out, "echo hello") != 2 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}
	if strings.Count(out, "echo hello1") != 1 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}
	if strings.Count(out, "echo hello2") != 1 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}
	require.NotContains(t, out, "hidden")

	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo hello1\necho hello2\n"
	if out != expectedOutput {
		t.Fatalf("hishtory export has unexpected output=%#v", out)
	}
}

func waitForBackgroundSavesToComplete(t testing.TB) {
	lastOut := ""
	lastErr := ""
	for i := 0; i < 20; i++ {
		cmd := exec.Command(getPidofCommand(), "hishtory")
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil && err.Error() != "exit status 1" {
			t.Fatalf("failed to check if hishtory was running: %v, stdout=%#v, stderr=%#v", err, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "\n") {
			// pidof had no output, so hishtory isn't running and we're done waiting
			time.Sleep(1000 * time.Millisecond)
			return
		}
		lastOut = stdout.String()
		lastErr = stderr.String()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("failed to wait until hishtory wasn't running (lastOut=%#v, lastErr=%#v)", lastOut, lastErr)
}

func testTimestampsAreReasonablyCorrect(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Record a command
	out := tester.RunInteractiveShell(t, "echo hello")
	if out != "hello\n" {
		t.Fatalf("running echo hello had unexpected out=%#v", out)
	}

	// Query for it and check that the timestamp that gets recorded looks reasonable
	out = hishtoryQuery(t, tester, "echo hello")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	expectedDate := time.Now().Format("Jan 2 2006")
	require.Contains(t, out, expectedDate)
}

func testTableDisplayCwd(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Record a command
	out := tester.RunInteractiveShell(t, `cd ~/.hishtory/
echo hello
cd /tmp/
echo other`)
	if out != "hello\nother\n" {
		t.Fatalf("running echo hello had unexpected out=%#v", out)
	}

	// Query for it and check that the directory gets recorded correctly
	out = hishtoryQuery(t, tester, "echo hello")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	require.Contains(t, out, "~/"+data.GetHishtoryPath())
	out = hishtoryQuery(t, tester, "echo other")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	require.Contains(t, out, "/tmp")
}

func testHishtoryBackgroundSaving(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()

	// Check that we can find the go binary
	_, err := exec.LookPath("go")
	require.NoError(t, err)

	// Test install with an unset HISHTORY_TEST var so that we save in the background (this is likely to be flakey!)
	out := tester.RunInteractiveShell(t, `unset HISHTORY_TEST
CGO_ENABLED=0 go build -o /tmp/client
/tmp/client install`)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output=%#v: matches=%#v", out, matches)
	}
	userSecret := matches[1]

	// Assert that config.sh isn't the dev version
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	dat, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"))
	require.NoError(t, err, "failed to read config.sh")
	require.NotContains(t, string(dat), "except it doesn't run the save process in the background", "config.sh is the testing version when it shouldn't be")

	// Test the status subcommand
	out = tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Test recording commands
	out, err = tester.RunInteractiveShellRelaxed(t, `ls /a
echo foo`)
	require.NoError(t, err)
	if out != "foo\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	waitForBackgroundSavesToComplete(t)
	time.Sleep(time.Second)
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo foo", "ls /a"}
	for _, item := range expected {
		require.Contains(t, out, item, "output is missing expected item")
	}

	// Test querying for a specific command
	waitForBackgroundSavesToComplete(t)
	out = hishtoryQuery(t, tester, "foo")
	require.Contains(t, out, "echo foo")
	require.NotContains(t, out, "ls /a")
}

func testDisplayTable(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Submit two fake entries
	tmz, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	entry1 := testutils.MakeFakeHistoryEntry("table_cmd1")
	entry1.StartTime = time.Unix(1650096186, 0).In(tmz)
	entry1.EndTime = time.Unix(1650096190, 0).In(tmz)
	manuallySubmitHistoryEntry(t, userSecret, entry1)
	entry2 := testutils.MakeFakeHistoryEntry("table_cmd2")
	entry2.StartTime = time.Unix(1650096196, 0).In(tmz)
	entry2.EndTime = time.Unix(1650096220, 0).In(tmz)
	entry2.CurrentWorkingDirectory = "~/foo/"
	entry2.ExitCode = 3
	manuallySubmitHistoryEntry(t, userSecret, entry2)

	// Query and check the table
	tester.RunInteractiveShell(t, ` hishtory disable`)
	out := hishtoryQuery(t, tester, "table")
	testutils.CompareGoldens(t, out, "testDisplayTable-defaultColumns")

	// Adjust the columns that should be displayed
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns Hostname Command`)

	// And check the table again
	out = hishtoryQuery(t, tester, "table")
	testutils.CompareGoldens(t, out, "testDisplayTable-customColumns")

	// And again
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns Hostname 'Exit Code' Command`)
	out = hishtoryQuery(t, tester, "table")
	testutils.CompareGoldens(t, out, "testDisplayTable-customColumns-2")

	// And again
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns CWD`)
	out = hishtoryQuery(t, tester, "table")
	testutils.CompareGoldens(t, out, "testDisplayTable-customColumns-3")

	// Test displaying a command with multiple lines
	entry3 := testutils.MakeFakeHistoryEntry("while :\ndo\nls /table/\ndone")
	manuallySubmitHistoryEntry(t, userSecret, entry3)
	out = hishtoryQuery(t, tester, "table")
	testutils.CompareGoldens(t, out, "testDisplayTable-customColumns-multiLineCommand")

	// Add a custom column
	tester.RunInteractiveShell(t, `hishtory config-add custom-columns foo "echo aaaaaaaaaaaaa"`)
	require.NoError(t, os.Chdir("/"))
	tester.RunInteractiveShell(t, ` hishtory enable`)
	tester.RunInteractiveShell(t, `echo table-1`)
	tester.RunInteractiveShell(t, `echo table-2`)
	tester.RunInteractiveShell(t, `echo bar`)
	tester.RunInteractiveShell(t, ` hishtory disable`)
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns foo`)

	// And run a query and confirm it is displayed
	out = hishtoryQuery(t, tester, "table")
	testutils.CompareGoldens(t, out, "testDisplayTable-customColumns-trulyCustom")
}

func testRequestAndReceiveDbDump(t *testing.T, tester shellTester) {
	// Set up
	defer testutils.BackupAndRestore(t)()
	secretKey := installHishtory(t, tester, "")

	// Confirm there are no pending dump requests
	ctx := hctx.MakeContext()
	config := hctx.GetConf(ctx)
	deviceId1 := config.DeviceId
	respBytes, err := lib.ApiGet(ctx, "/api/v1/get-dump-requests?user_id="+data.UserId(secretKey)+"&device_id="+deviceId1)
	resp := strings.TrimSpace(string(respBytes))
	require.NoError(t, err, "failed to get pending dump requests")
	require.Equalf(t, "[]", resp, "there are pending dump requests! user_id=%#v, resp=%#v", data.UserId(secretKey), resp)

	// Record two commands and then query for them
	out := tester.RunInteractiveShell(t, `echo hello
echo other`)
	if out != "hello\nother\n" {
		t.Fatalf("running echo had unexpected out=%#v", out)
	}

	// Query for it and check that the directory gets recorded correctly
	out = hishtoryQuery(t, tester, "echo")
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	require.Contains(t, out, "hishtory query echo")
	require.Contains(t, out, "echo hello")
	require.Contains(t, out, "echo other")

	// Back up this copy
	restoreFirstInstallation := testutils.BackupAndRestoreWithId(t, "-install1")

	// Wipe the DB to simulate entries getting deleted because they've already been read and expired
	_, err = lib.ApiGet(ctx, "/api/v1/wipe-db-entries")
	require.NoError(t, err, "failed to wipe the remote DB")

	// Install a new one (with the same secret key but a diff device id)
	installHishtory(t, tester, secretKey)

	// Confirm there is now a pending dump requests that the first device should respond to
	respBytes, err = lib.ApiGet(ctx, "/api/v1/get-dump-requests?user_id="+data.UserId(secretKey)+"&device_id="+deviceId1)
	resp = strings.TrimSpace(string(respBytes))
	require.NoError(t, err, "failed to get pending dump requests")
	require.NotEqualf(t, "[]", resp, "There are no pending dump requests! user_id=%#v, resp=%#v", data.UserId(secretKey), string(resp))

	// Check that the new one doesn't have the commands yet
	out = hishtoryQuery(t, tester, "echo")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has unexpected number of lines, should contain no entries: out=%#v", out)
	}
	require.Contains(t, out, "hishtory query echo")
	require.NotContains(t, out, "echo hello", "hishtory query contains unexpected command")
	require.NotContains(t, out, "echo other", "hishtory query contains unexpected command")
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	if out != "hishtory query echo\n" {
		t.Fatalf("hishtory export has unexpected out=%#v", out)
	}

	// Restore the first copy
	restoreSecondInstallation := testutils.BackupAndRestoreWithId(t, "-install2")
	restoreFirstInstallation()

	// Confirm it still has the correct entries via hishtory export (and this runs a command to trigger it to dump the DB)
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput := "echo hello\necho other\nhishtory query echo\nhishtory query echo\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Confirm there are no pending dump requests for the first device
	respBytes, err = lib.ApiGet(ctx, "/api/v1/get-dump-requests?user_id="+data.UserId(secretKey)+"&device_id="+deviceId1)
	resp = strings.TrimSpace(string(respBytes))
	require.NoError(t, err, "failed to get pending dump requests")
	require.Equalf(t, "[]", resp, "There are pending dump requests! user_id=%#v, resp=%#v", data.UserId(secretKey), string(resp))

	// Restore the second copy and confirm it has the commands
	restoreSecondInstallation()
	out = hishtoryQuery(t, tester, "ech")
	if strings.Count(out, "\n") != 6 {
		t.Fatalf("hishtory query has unexpected number of lines=%d: out=%#v", strings.Count(out, "\n"), out)
	}
	require.Contains(t, out, "hishtory query ech")
	expected := []string{"echo hello", "echo other"}
	for _, item := range expected {
		require.Contains(t, out, item)
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}

	// And check hishtory export too for good measure
	out = tester.RunInteractiveShell(t, ` hishtory export | grep -v pipefail`)
	expectedOutput = "echo hello\necho other\nhishtory query echo\nhishtory query echo\nhishtory query ech\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func TestInstallViaPythonScriptWithCustomHishtoryPath(t *testing.T) {
	markTestForSharding(t, 0)
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("HISHTORY_PATH")()
	altHishtoryPath := ".other-path"
	os.Setenv("HISHTORY_PATH", altHishtoryPath)

	// Make sure ~/$HISHTORY_PATH/ is also cleared out and empty
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(path.Join(homedir, altHishtoryPath)))

	testInstallViaPythonScriptChild(t, zshTester{})
}

func TestInstallViaPythonScriptInOfflineMode(t *testing.T) {
	markTestForSharding(t, 1)
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("HISHTORY_OFFLINE")()
	os.Setenv("HISHTORY_OFFLINE", "1")
	tester := zshTester{}

	// Check that installing works
	testInstallViaPythonScriptChild(t, tester)

	// And check that it installed in offline mode
	out := tester.RunInteractiveShell(t, `hishtory status -v`)
	require.Contains(t, out, "\nSync Mode: Disabled\n")
}

func testInstallViaPythonScript(t *testing.T, tester shellTester) {
	defer testutils.BackupAndRestore(t)()
	testInstallViaPythonScriptChild(t, tester)

	// And check that it installed in online mode
	out := tester.RunInteractiveShell(t, `hishtory status -v`)
	require.Contains(t, out, "\nSync Mode: Enabled\n")
}

func testInstallViaPythonScriptChild(t *testing.T, tester shellTester) {
	if !testutils.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}

	// Set up
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()

	// Install via the python script
	out := tester.RunInteractiveShell(t, `curl https://hishtory.dev/install.py | python3 -`)
	require.Contains(t, out, "Succesfully installed hishtory")
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output=%#v: matches=%#v", out, matches)
	}
	userSecret := matches[1]

	// Test the status subcommand
	downloadData, err := cmd.GetDownloadData(makeTestOnlyContextWithFakeConfig())
	require.NoError(t, err)
	out = tester.RunInteractiveShell(t, `hishtory status`)
	expectedOut := fmt.Sprintf("hiSHtory: %s\nEnabled: true\nSecret Key: %s\nCommit Hash: ", downloadData.Version, userSecret)
	require.Contains(t, out, expectedOut)

	// And test that it recorded that command
	time.Sleep(time.Second)
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	if out != "hishtory status\n" {
		t.Fatalf("unexpected output from hishtory export=%#v", out)
	}
}

func TestInstallViaPythonScriptFromHead(t *testing.T) {
	markTestForSharding(t, 2)
	defer testutils.BackupAndRestore(t)()
	tester := zshTester{}

	// Set up
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()

	// Install via the python script
	out := tester.RunInteractiveShell(t, `cat backend/web/landing/www/install.py | python3 -`)
	require.Contains(t, out, "Succesfully installed hishtory")
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output=%#v: matches=%#v", out, matches)
	}
	userSecret := matches[1]

	// Test the status subcommand
	downloadData, err := cmd.GetDownloadData(makeTestOnlyContextWithFakeConfig())
	require.NoError(t, err)
	out = tester.RunInteractiveShell(t, `hishtory status`)
	expectedOut := fmt.Sprintf("hiSHtory: %s\nEnabled: true\nSecret Key: %s\nCommit Hash: ", downloadData.Version, userSecret)
	require.Contains(t, out, expectedOut)

	// And test that it recorded that command
	time.Sleep(time.Second)
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	if out != "hishtory status\n" {
		t.Fatalf("unexpected output from hishtory export=%#v", out)
	}

	// And check that it installed in online mode
	out = tester.RunInteractiveShell(t, `hishtory status -v`)
	require.Contains(t, out, "\nSync Mode: Enabled\n")
}

func testExportWithQuery(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Test recording commands
	out, err := tester.RunInteractiveShellRelaxed(t, `ls /a
ls /bar
ls /foo
echo foo
echo bar
hishtory disable
echo thisisnotrecorded
sleep 0.5
cd /tmp/
hishtory enable
echo thisisrecorded
echo bar &
sleep 1`)
	require.NoError(t, err)
	if out != "foo\nbar\nthisisnotrecorded\nthisisrecorded\nbar\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a", "echo bar &", "sleep 1"}
	for _, item := range expected {
		require.Contains(t, out, item)
	}

	// Test querying for a specific command
	out = hishtoryQuery(t, tester, "foo")
	expected = []string{"echo foo", "ls /foo"}
	unexpected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "ls /bar", "ls /a"}
	for _, item := range expected {
		require.Contains(t, out, item)
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}
	for _, item := range unexpected {
		require.NotContains(t, out, item)
	}

	// Test using export with a query
	out = tester.RunInteractiveShell(t, `hishtory export foo`)
	if out != "ls /foo\necho foo\nhishtory query foo\nhishtory export foo\n" {
		t.Fatalf("expected hishtory export to equal out=%#v", out)
	}

	// Test a more complex query with export
	out = tester.RunInteractiveShell(t, `hishtory export cwd:/tmp/`)
	if out != "hishtory enable\necho thisisrecorded\necho bar &\nsleep 1\n" {
		t.Fatalf("expected hishtory export to equal out=%#v", out)
	}
}

func testHelpCommand(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Test the help command
	out := tester.RunInteractiveShell(t, `hishtory help`)
	if !strings.HasPrefix(out, "hiSHtory: Better shell history") {
		t.Fatalf("expected hishtory help to contain intro, actual=%#v", out)
	}
	out2 := tester.RunInteractiveShell(t, `hishtory -h`)
	if out != out2 {
		t.Fatalf("expected hishtory -h to equal help")
	}
}

func TestStripBashTimePrefix(t *testing.T) {
	// Setup
	markTestForSharding(t, 4)
	defer testutils.BackupAndRestore(t)()
	tester := bashTester{}
	installHishtory(t, tester, "")

	// Add a HISTTIMEFORMAT to the bashrc
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	f, err := os.OpenFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"),
		os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.WriteString("\nexport HISTTIMEFORMAT='%F %T '\n")
	require.NoError(t, err)

	// Record a command
	tester.RunInteractiveShell(t, `ls -Slah`)

	// Check it shows up correctly
	out := tester.RunInteractiveShell(t, "hishtory export ls")
	if out != "ls -Slah\nhishtory export ls\n" {
		t.Fatalf("hishtory had unexpected output=%#v", out)
	}

	// Update it to another complex one
	homedir, err = os.UserHomeDir()
	require.NoError(t, err)
	f, err = os.OpenFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"),
		os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.WriteString("\nexport HISTTIMEFORMAT='[%c] '\n")
	require.NoError(t, err)

	// Record a command
	tester.RunInteractiveShell(t, `echo foo`)

	// Check it shows up correctly
	out = tester.RunInteractiveShell(t, "hishtory export echo")
	if out != "echo foo\nhishtory export echo\n" {
		t.Fatalf("hishtory had unexpected output=%#v", out)
	}
}

func testReuploadHistoryEntries(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()

	// Init an initial device
	userSecret := installHishtory(t, tester, "")

	// Set up a second device
	restoreFirstProfile := testutils.BackupAndRestoreWithId(t, "-install1")
	installHishtory(t, tester, userSecret)

	// Device 2: Record a command
	tester.RunInteractiveShell(t, `echo 1`)

	// Device 2: Record a command with a simulated network error
	tester.RunInteractiveShell(t, `echo 2`)
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	tester.RunInteractiveShell(t, `echo 3`)
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")

	// Device 1: Run an export and confirm that the network only contains the first command
	restoreSecondProfile := testutils.BackupAndRestoreWithId(t, "-install2")
	restoreFirstProfile()
	out := tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput := "echo 1\necho 2\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 2: Run another command but with the network re-enabled
	restoreFirstProfile = testutils.BackupAndRestoreWithId(t, "-install1")
	restoreSecondProfile()
	tester.RunInteractiveShell(t, `echo 4`)

	// Device 2: Run export which contains all results (as it did all along since it is stored offline)
	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput = "echo 1\necho 2\necho 3\necho 4\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 1: Now it too sees all the results
	restoreFirstProfile()
	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput = "echo 1\necho 2\necho 3\necho 4\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testHishtoryOffline(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()

	// Init an initial device
	userSecret := installHishtory(t, tester, "")

	// Set up a second device
	restoreFirstProfile := testutils.BackupAndRestoreWithId(t, "-install1")
	installHishtory(t, tester, userSecret)

	// Device 2: Record a command
	tester.RunInteractiveShell(t, `echo dev2`)

	// Device 1: Run a command
	restoreSecondProfile := testutils.BackupAndRestoreWithId(t, "-install2")
	restoreFirstProfile()
	tester.RunInteractiveShell(t, `echo dev1-a`)

	// Device 1: Query while offline
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	out := tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")
	expectedOutput := "echo dev2\necho dev1-a\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 2: Record another command
	restoreFirstProfile = testutils.BackupAndRestoreWithId(t, "-install1")
	restoreSecondProfile()
	tester.RunInteractiveShell(t, `echo dev2-b`)

	// Device 1: Query while offline before ever retrieving the command
	restoreFirstProfile()
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")
	expectedOutput = "echo dev2\necho dev1-a\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 1: Query while online and get the command
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = "echo dev2\necho dev1-a\necho dev2-b\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testInitialHistoryImport(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("HISHTORY_SKIP_INIT_IMPORT")()
	os.Setenv("HISHTORY_SKIP_INIT_IMPORT", "")

	// Record some commands before installing hishtory
	randomCmdUuid := uuid.Must(uuid.NewRandom()).String()
	captureTerminalOutputWithShellName(t, tester, "fish", []string{fmt.Sprintf("echo SPACE %s-fishcommand ENTER", randomCmdUuid)})
	randomCmd := fmt.Sprintf(`echo %v-foo
echo %v-bar`, randomCmdUuid, randomCmdUuid)
	tester.RunInteractiveShell(t, randomCmd)

	// Install hishtory
	installHishtory(t, tester, "")

	// Check that hishtory export has the commands
	out := tester.RunInteractiveShell(t, `hishtory export `+randomCmdUuid[:5])
	expectedOutput := strings.ReplaceAll(`echo UUID-fishcommand
echo UUID-foo
echo UUID-bar
hishtory export `+randomCmdUuid[:5]+`
`, "UUID", randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Compare the rest of the hishtory export
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail -/tmp/client -`+randomCmdUuid[:5])
	if out != "" {
		t.Fatalf("expected hishtory export to be empty, was=%v", out)
	}
}

func testLocalRedaction(t *testing.T, tester shellTester, onlineStatus OnlineStatus) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installWithOnlineStatus(t, tester, onlineStatus)
	assertOnlineStatus(t, onlineStatus)

	// Record some commands
	randomCmdUuid := uuid.Must(uuid.NewRandom()).String()
	randomCmd := fmt.Sprintf(`echo %v-foo
echo %v-bas
echo foo
ls /tmp`, randomCmdUuid, randomCmdUuid)
	tester.RunInteractiveShell(t, randomCmd)

	// Check that the previously recorded commands are in hishtory
	out := tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	expectedOutput := fmt.Sprintf("echo %s-foo\necho %s-bas\necho foo\nls /tmp\n", randomCmdUuid, randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Redact foo
	out = tester.RunInteractiveShell(t, `HISHTORY_REDACT_FORCE=1 hishtory redact foo`)
	if out != "Permanently deleting 3 entries\n" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

	// Check that the commands are redacted
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = fmt.Sprintf("echo %s-bas\nls /tmp\nHISHTORY_REDACT_FORCE=1 hishtory redact foo\n", randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Redact s
	out = tester.RunInteractiveShell(t, `HISHTORY_REDACT_FORCE=1 hishtory redact s`)
	if out != "Permanently deleting 10 entries\n" && out != "Permanently deleting 11 entries\n" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

	// Check that the commands are redacted
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = "HISHTORY_REDACT_FORCE=1 hishtory redact s\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Record another command
	tester.RunInteractiveShell(t, `echo hello`)
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = "HISHTORY_REDACT_FORCE=1 hishtory redact s\necho hello\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Redact it without HISHTORY_REDACT_FORCE
	out, err := tester.RunInteractiveShellRelaxed(t, `yes | hishtory redact hello`)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`This will permanently delete (1|2) entries, are you sure\? \[y/N] `), out)

	// And check it was redacted
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = "HISHTORY_REDACT_FORCE=1 hishtory redact s\nyes | hishtory redact hello\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testRemoteRedaction(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()

	// Install hishtory client 1
	userSecret := installHishtory(t, tester, "")

	// Record some commands
	randomCmdUuid := uuid.Must(uuid.NewRandom()).String()
	randomCmd := fmt.Sprintf(`echo %v-foo
echo %v-bas
echo foo
ls /tmp`, randomCmdUuid, randomCmdUuid)
	tester.RunInteractiveShell(t, randomCmd)

	// Check that the previously recorded commands are in hishtory
	out := tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput := fmt.Sprintf("echo %s-foo\necho %s-bas\necho foo\nls /tmp\n", randomCmdUuid, randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Install hishtory client 2
	restoreInstall1 := testutils.BackupAndRestoreWithId(t, "-1")
	installHishtory(t, tester, userSecret)

	// And confirm that it has the commands too
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Restore the first client, and redact some commands
	restoreInstall2 := testutils.BackupAndRestoreWithId(t, "-2")
	restoreInstall1()
	out = tester.RunInteractiveShell(t, `HISHTORY_REDACT_FORCE=1 hishtory redact `+randomCmdUuid)
	if out != "Permanently deleting 3 entries\n" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

	// Confirm that client1 doesn't have the commands
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = fmt.Sprintf("echo foo\nls /tmp\nHISHTORY_REDACT_FORCE=1 hishtory redact %s\n", randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Swap back to the second client and then confirm it processed the deletion request
	restoreInstall2()
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testConfigGetSet(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Config-get and set for enable-control-r
	out := tester.RunInteractiveShell(t, `hishtory config-get enable-control-r`)
	if out != "true\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-set enable-control-r false`)
	out = tester.RunInteractiveShell(t, `hishtory config-get enable-control-r`)
	if out != "false\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-set enable-control-r true`)
	out = tester.RunInteractiveShell(t, `hishtory config-get enable-control-r`)
	if out != "true\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}

	// config for displayed-columns
	out = tester.RunInteractiveShell(t, `hishtory config-get displayed-columns`)
	if out != "Hostname CWD Timestamp Runtime \"Exit Code\" Command \n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns Hostname Command 'Exit Code'`)
	out = tester.RunInteractiveShell(t, `hishtory config-get displayed-columns`)
	if out != "Hostname Command \"Exit Code\" \n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns Timestamp`)
	out = tester.RunInteractiveShell(t, `hishtory config-get displayed-columns`)
	if out != "Hostname Command \"Exit Code\" Timestamp \n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-delete displayed-columns Hostname`)
	out = tester.RunInteractiveShell(t, `hishtory config-get displayed-columns`)
	if out != "Command \"Exit Code\" Timestamp \n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns foobar`)
	out = tester.RunInteractiveShell(t, `hishtory config-get displayed-columns`)
	if out != "Command \"Exit Code\" Timestamp foobar \n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}

	// For OpenAI endpoints
	out = tester.RunInteractiveShell(t, `hishtory config-get ai-completion-endpoint`)
	if out != "https://api.openai.com/v1/chat/completions\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
	tester.RunInteractiveShell(t, `hishtory config-set ai-completion-endpoint https://example.com/foo/bar`)
	out = tester.RunInteractiveShell(t, `hishtory config-get ai-completion-endpoint`)
	if out != "https://example.com/foo/bar\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
}

func clearControlRSearchFromConfig(t testing.TB) {
	configContents, err := hctx.GetConfigContents()
	require.NoError(t, err)
	configContents = []byte(strings.ReplaceAll(string(configContents), "enable_control_r_search", "something-else"))
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	err = os.WriteFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH), configContents, 0o644)
	require.NoError(t, err)
}

func testHandleUpgradedFeatures(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Install, and there is no prompt since the config already mentions control-r
	_, err := tester.RunInteractiveShellRelaxed(t, `/tmp/client install`)
	require.NoError(t, err)
	_, err = tester.RunInteractiveShellRelaxed(t, `hishtory disable`)
	require.NoError(t, err)

	// Ensure that the config doesn't mention control-r
	clearControlRSearchFromConfig(t)

	// And check that hishtory says it is false by default
	out := tester.RunInteractiveShell(t, `hishtory config-get enable-control-r`)
	if out != "false\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}

	// And install again, this time it will get set to true by default
	clearControlRSearchFromConfig(t)
	tester.RunInteractiveShell(t, ` /tmp/client install`)

	// Now it should be enabled
	out = tester.RunInteractiveShell(t, `hishtory config-get enable-control-r`)
	if out != "true\n" {
		t.Fatalf("unexpected config-get output: %#v", out)
	}
}

func TestFish(t *testing.T) {
	// Setup
	markTestForSharding(t, 5)
	defer testutils.BackupAndRestore(t)()
	tester := bashTester{}
	installHishtory(t, tester, "")

	// Test recording in fish
	require.NoError(t, os.Chdir("/"))
	out := captureTerminalOutputWithShellName(t, tester, "fish", []string{
		"echo SPACE foo ENTER",
		"ENTER",
		"SPACE echo SPACE baz ENTER",
		"echo SPACE bar ENTER",
		"echo SPACE '\"foo\"' ENTER",
		"SPACE echo SPACE foobar ENTER",
		"ls SPACE /tmp/ SPACE '&' ENTER",
	})
	require.Contains(t, out, "Welcome to fish, the friendly interactive shell")
	require.Contains(t, out, "\nfoo\n")
	require.Contains(t, out, "\nbar\n")
	require.Contains(t, out, "\nbaz\n")
	require.Contains(t, out, "\nfoobar\n")

	// And test that fish exits properly, for #117
	out = captureTerminalOutputWithShellName(t, tester, "bash", []string{
		"fish ENTER",
		"echo SPACE foo ENTER",
		"exit ENTER",
	})
	require.Contains(t, out, "Welcome to fish, the friendly interactive shell")
	require.Contains(t, out, "\nfoo\n")
	require.NotContains(t, out, "There are still jobs active")
	require.NotContains(t, out, "A second attempt to exit will terminate them.")
	if runtime.GOOS == "darwin" {
		require.Contains(t, out, "exit\nbash")
	} else {
		require.Contains(t, out, "exit\nrunner@ghaction-runner-hostname:/$")
	}

	// Check export
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail | grep -v ps`)
	expectedOutput := "echo foo\necho bar\necho \"foo\"\nls /tmp/ &\nfish\necho foo\nexit\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Check a table to see some other metadata
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns CWD Hostname 'Exit Code' Command`)
	out = hishtoryQuery(t, tester, "-pipefail")
	testutils.CompareGoldens(t, out, "TestFish-table")
}

func setupTestTui(t testing.TB, onlineStatus OnlineStatus) (shellTester, string, *gorm.DB) {
	tester := zshTester{}
	userSecret := installWithOnlineStatus(t, tester, onlineStatus)
	assertOnlineStatus(t, onlineStatus)

	// Disable recording so that all our testing commands don't get recorded
	_, _ = tester.RunInteractiveShellRelaxed(t, ` hishtory disable`)

	// Insert a couple hishtory entries
	db := hctx.GetDb(hctx.MakeContext())
	e1 := testutils.MakeFakeHistoryEntry("ls ~/")
	require.NoError(t, db.Create(e1).Error)
	if onlineStatus == Online {
		manuallySubmitHistoryEntry(t, userSecret, e1)
	}
	e2 := testutils.MakeFakeHistoryEntry("echo 'aaaaaa bbbb'")
	require.NoError(t, db.Create(e2).Error)
	if onlineStatus == Online {
		manuallySubmitHistoryEntry(t, userSecret, e2)
	}
	return tester, userSecret, db
}

func testTui_resize(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, userSecret, _ := setupTestTui(t, Online)

	// Check the output when the size is smaller
	out := captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 20, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-SmallTerminal")

	// Check the output when the size is tiny
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 15, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-TinyTerminal")

	// Check the output when the size is tiny and the help page is open
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 15, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		{Keys: "C-h"},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-TinyTerminalHelp")

	// Check the output when the size is extra tiny
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 11, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
	})
	testutils.CompareGoldens(t, out, "TestTui-TiniestTerminal")

	// Check the output when the size is tiny and the user tries to open the help page, which doesn't work
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 11, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		{Keys: "C-h"},
	})
	testutils.CompareGoldens(t, out, "TestTui-TiniestTerminal")

	// Check that it resizes after the terminal size is adjusted
	manuallySubmitHistoryEntry(t, userSecret, testutils.MakeFakeHistoryEntry("echo 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'"))
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 20, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		{ResizeX: 300, ResizeY: 100},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-Resize")

	// Check that the cursor position is maintained after it is resized
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 20, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		{Keys: "Down"},
		{ResizeX: 300, ResizeY: 100, ExtraDelay: 1.0},
		{Keys: "Enter"},
	})
	require.Contains(t, out, "\necho 'aaaaaa bbbb'\n")

	// Check that it supports a very long search query
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 20, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		{Keys: "1234567890qwertyuip1234567890qwertyuip1234567890qwertyuip1234567890qwertyuip1234567890qwertyuip"},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-LongQuery")
}

func testTui_scroll(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, userSecret, _ := setupTestTui(t, Online)

	// Check that we can use left arrow keys to scroll
	out := captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"s",
		"Left",
		"l",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-LeftScroll")

	// Test horizontal scrolling by one to the right
	veryLongEntry := testutils.MakeFakeHistoryEntry("echo '1234567890qwertyuiopasdfghjklzxxcvbnm0987654321_0_1234567890qwertyuiopasdfghjklzxxcvbnm0987654321_1_1234567890qwertyuiopasdfghjklzxxcvbnm0987654321_2_1234567890qwertyuiopasdfghjklzxxcvbnm0987654321'")
	manuallySubmitHistoryEntry(t, userSecret, veryLongEntry)
	require.NoError(t, hctx.GetDb(hctx.MakeContext()).Create(veryLongEntry).Error)
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"S-Left S-Right S-Right S-Left",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-RightScroll")

	// Test horizontal scrolling by two
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"S-Right S-Right",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-RightScrollTwo")

	// TODO: Add a test here that shows all columns can be horizontally scrolled

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func testTui_defaultFilter(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, userSecret, _ := setupTestTui(t, Online)
	db := hctx.GetDb(hctx.MakeContext())
	e1 := testutils.MakeFakeHistoryEntry("exit 0")
	e1.ExitCode = 0
	require.NoError(t, db.Create(e1).Error)
	manuallySubmitHistoryEntry(t, userSecret, e1)
	e2 := testutils.MakeFakeHistoryEntry("exit 1")
	e2.ExitCode = 1
	require.NoError(t, db.Create(e2).Error)
	manuallySubmitHistoryEntry(t, userSecret, e2)

	// Configure a default filter
	require.Equal(t, "\"\"", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get default-filter`)))
	tester.RunInteractiveShell(t, `hishtory config-set default-filter "exit_code:0"`)
	require.Equal(t, "\"exit_code:0\"", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get default-filter`)))

	// Run a search query with no additional query
	out := stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
	}))
	testutils.CompareGoldens(t, out, "TestTui-DefaultFilter-Enabled")

	// Run a search query with an additional query
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"exit",
	}))
	testutils.CompareGoldens(t, out, "TestTui-DefaultFilter-EnabledAdditionalQuery")

	// Run a search query and delete the default filter
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"BSpace",
	}))
	testutils.CompareGoldens(t, out, "TestTui-DefaultFilter-Deleted")

	// Run a search query, type something, and then delete the default filter
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"exit Left Left Left Left Left BSpace BSpace",
	}))
	testutils.CompareGoldens(t, out, "TestTui-DefaultFilter-DeletedWithText")
}

func testTui_color(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, _, _ := setupTestTui(t, Online)
	tester.RunInteractiveShell(t, ` hishtory config-set highlight-matches false`)

	// Capture the TUI with full colored output, note that this golden will be harder to undersand
	// from inspection and primarily servers to detect unintended changes in hishtory's output.
	out := captureTerminalOutputComplex(t, TmuxCaptureConfig{tester: tester, complexCommands: []TmuxCommand{{Keys: "hishtory SPACE tquery ENTER"}}, includeEscapeSequences: true})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-ColoredOutput-"+runtime.GOOS+"-"+testutils.GetOsVersion(t))

	// And the same once a search query has been typed in
	out = captureTerminalOutputComplex(t, TmuxCaptureConfig{tester: tester, complexCommands: []TmuxCommand{{Keys: "hishtory SPACE tquery ENTER"}, {Keys: "ech"}}, includeEscapeSequences: true})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-ColoredOutputWithSearch-"+runtime.GOOS+"-"+testutils.GetOsVersion(t))

	// And one more time with highlight-matches
	tester.RunInteractiveShell(t, ` hishtory config-set highlight-matches true`)
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get highlight-matches`)))
	out = captureTerminalOutputComplex(t, TmuxCaptureConfig{tester: tester, complexCommands: []TmuxCommand{{Keys: "hishtory SPACE tquery ENTER"}, {Keys: "ech"}}, includeEscapeSequences: true})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-ColoredOutputWithSearch-Highlight-"+runtime.GOOS+"-"+testutils.GetOsVersion(t))

	// And one more time with customized colors
	testutils.CompareGoldens(t, tester.RunInteractiveShell(t, ` hishtory config-get color-scheme`), "TestTui-DefaultColorScheme")
	tester.RunInteractiveShell(t, ` hishtory config-set color-scheme selected-text #45f542`)
	tester.RunInteractiveShell(t, ` hishtory config-set color-scheme selected-background #4842f5`)
	tester.RunInteractiveShell(t, ` hishtory config-set color-scheme border-color #f54272`)
	out = captureTerminalOutputComplex(t, TmuxCaptureConfig{tester: tester, complexCommands: []TmuxCommand{{Keys: "hishtory SPACE tquery ENTER"}, {Keys: "ech"}}, includeEscapeSequences: true})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-ColoredOutputWithCustomColorScheme-"+runtime.GOOS+"-"+testutils.GetOsVersion(t))

	// And one more time with a default filter
	require.Equal(t, "\"\"", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get default-filter`)))
	tester.RunInteractiveShell(t, `hishtory config-set default-filter "exit_code:0"`)
	require.Equal(t, "\"exit_code:0\"", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get default-filter`)))
	out = captureTerminalOutputComplex(t, TmuxCaptureConfig{tester: tester, complexCommands: []TmuxCommand{{Keys: "hishtory SPACE tquery ENTER"}, {Keys: "ech"}}, includeEscapeSequences: true})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-ColoredOutputWithDefaultFilter-"+runtime.GOOS+"-"+testutils.GetOsVersion(t))
}

func testTui_delete(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, userSecret, _ := setupTestTui(t, Online)
	manuallySubmitHistoryEntry(t, userSecret, testutils.MakeFakeHistoryEntry("echo 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'"))

	// Check that we can delete an entry
	out := captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		// ExtraDelay so that the search query finishes before we hit delete
		{Keys: "aaaaaa", ExtraDelay: 1.0},
		{Keys: "C-K"},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-Delete")

	// And that it stays deleted
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-DeleteStill")

	// And that we can then delete another entry
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-K",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-DeleteAgain")

	// And that it stays deleted
	out = captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER", ExtraDelay: 1.5},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-DeleteAgainStill")

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func testTui_search(t *testing.T, onlineStatus OnlineStatus) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, _, _ := setupTestTui(t, onlineStatus)

	// Check hishtory export to confirm the right commands are in the initial set of history entries
	out := tester.RunInteractiveShell(t, `hishtory export`)
	expected := "ls ~/\necho 'aaaaaa bbbb'\n"
	if diff := cmp.Diff(expected, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s", diff)
	}

	// Check the output when there is a search
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"ls",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-Search")

	// Check the output when there is a selected result
	out = captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		// Extra delay to ensure that the search for 'ls' finishes before we select an entry
		{Keys: "ls", ExtraDelay: 2.0},
		{Keys: "ENTER"},
	})
	out = strings.Split(stripTuiCommandPrefix(t, out), "\n")[0]
	expected = `ls ~/`
	if diff := cmp.Diff(expected, out); diff != "" {
		t.Fatalf("hishtory tquery selection mismatch (-expected +got):\n%s", diff)
	}

	// Check the output when the initial search is invalid
	out = captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		// ExtraDelay to ensure that after searching for 'foo:' it gets the real results for an empty query
		{Keys: "hishtory SPACE tquery SPACE foo: ENTER", ExtraDelay: 1.5},
		{Keys: "ls", ExtraDelay: 1.0},
	})
	out = stripRequiredPrefix(t, out, "hishtory tquery foo:")
	testutils.CompareGoldens(t, out, "TestTui-InitialInvalidSearch")

	// Check the output when the search is invalid
	out = captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER", ExtraDelay: 1.0},
		// ExtraDelay to ensure that the search for 'ls' finishes before we make it invalid by adding ':'
		{Keys: "ls", ExtraDelay: 1.5},
		{Keys: ":"},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-InvalidSearch")

	// Check the output when the search is invalid and then edited to become valid
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"ls: BSpace",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-InvalidSearchBecomesValid")

	// Record a couple commands that we can use to test for supporting quoted searches
	db := hctx.GetDb(hctx.MakeContext())
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("for i in 1")).Error)
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("for i in 2")).Error)
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("i for in")).Error)
	out = tester.RunInteractiveShell(t, `hishtory export`)
	testutils.CompareGoldens(t, out, "TestTui-ExportWithAdditionalEntries")

	// Check the behavior when it is unquoted and fuzzy
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"for SPACE i SPACE in",
	}))
	testutils.CompareGoldens(t, out, "TestTui-SearchUnquoted")

	// Check the behavior when it is quoted and exact
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"'\"'for SPACE i SPACE in'\"'",
	}))
	testutils.CompareGoldens(t, out, "TestTui-SearchQuoted")

	// Check the behavior when it is backslashed
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"for\\\\ SPACE i\\\\ SPACE in",
	}))
	testutils.CompareGoldens(t, out, "TestTui-SearchBackslash")

	// Add another entry for testing quoting a colon
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("foo:bar")).Error)
	out = tester.RunInteractiveShell(t, `hishtory export`)
	testutils.CompareGoldens(t, out, "TestTui-ExportWithEvenMoreEntries")

	// And check that we can quote colons
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"foo:bar",
	}))
	testutils.CompareGoldens(t, out, "TestTui-SearchColonError")
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"foo\\\\:bar",
	}))
	testutils.CompareGoldens(t, out, "TestTui-SearchColonEscaped")
	out = stripTuiCommandPrefix(t, captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"'\"'foo:bar'\"'",
	}))
	testutils.CompareGoldens(t, out, "TestTui-SearchColonDoubleQuoted")
}

func testTui_general(t *testing.T, onlineStatus OnlineStatus) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, _, _ := setupTestTui(t, onlineStatus)

	// Check the initial output when there is no search
	out := captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery ENTER"})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-Initial")

	// Check that we can exit the TUI via pressing esc
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"Escape",
	})
	require.NotContains(t, out, "Search Query:")
	if testutils.IsGithubAction() {
		testutils.CompareGoldens(t, out, "TestTui-Exit-"+runtime.GOOS)
	}

	// Test opening the help page
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-h",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-HelpPage")

	// Test closing the help page
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-h C-h",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-HelpPageClosed")

	// Test selecting and cd-ing
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-x",
	})
	out = strings.Split(stripTuiCommandPrefix(t, out), "\n")[0]
	testutils.CompareGoldens(t, out, "TestTui-SelectAndCd")

	// Test jumping around the cursor via shortcuts
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"foo C-a AAA SPACE C-e SPACE ZZZ",
	})
	out = strings.Split(stripTuiCommandPrefix(t, out), "\n")[0]
	testutils.CompareGoldens(t, out, "TestTui-JumpCursor")

	// Test the User column
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns User`)
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
	})
	out = stripTuiCommandPrefix(t, out)
	require.Contains(t, out, "   User")
	require.Contains(t, out, " david ")

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func testTui_errors(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester, _, _ := setupTestTui(t, Online)

	// Check the output when the device is offline
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	out := captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery ENTER"})
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-Offline")

	// Check the output when the device is offline AND there is an invalid search
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery ENTER", "ls:"})
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-OfflineInvalid")
}

func testTui_ai(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("OPENAI_API_KEY")()
	os.Setenv("OPENAI_API_KEY", "")
	tester, _, _ := setupTestTui(t, Online)
	req, err := json.Marshal(
		ai.TestOnlyOverrideAiSuggestionRequest{Query: "myQuery", Suggestions: []string{"result 1", "result 2", "longer result 3"}},
	)
	require.NoError(t, err)
	_, err = lib.ApiPost(hctx.MakeContext(), "/api/v1/ai-suggest-override", "application/json", req)
	require.NoError(t, err)

	// Test running an AI query
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get ai-completion`)))
	out := captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		// ExtraDelay since AI queries are debounced and thus slower
		{Keys: "'?myQuery'", ExtraDelay: 1.0},
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-AiQuery")

	// Test that when it is disabled, no AI queries are run
	tester.RunInteractiveShell(t, `hishtory config-set ai-completion false`)
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"'?myQuery'",
	})
	out = stripTuiCommandPrefix(t, out)
	testutils.CompareGoldens(t, out, "TestTui-AiQuery-Disabled")
}

func testControlR(t *testing.T, tester shellTester, shellName string, onlineStatus OnlineStatus) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installWithOnlineStatus(t, tester, onlineStatus)
	assertOnlineStatus(t, onlineStatus)

	// Disable recording so that all our testing commands don't get recorded
	_, _ = tester.RunInteractiveShellRelaxed(t, ` hishtory disable`)
	_, _ = tester.RunInteractiveShellRelaxed(t, `hishtory config-set enable-control-r true`)
	tester.RunInteractiveShell(t, ` HISHTORY_REDACT_FORCE=true hishtory redact set emo pipefail`)

	// Insert a few hishtory entries that we'll use for testing into an empty DB
	db := hctx.GetDb(hctx.MakeContext())
	require.NoError(t, db.Where("true").Delete(&data.HistoryEntry{}).Error)
	e1 := testutils.MakeFakeHistoryEntry("ls ~/")
	e1.CurrentWorkingDirectory = "/etc/"
	e1.Hostname = "server"
	e1.ExitCode = 127
	require.NoError(t, db.Create(e1).Error)
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("ls ~/foo/")).Error)
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("ls ~/bar/")).Error)
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("echo 'aaaaaa bbbb'")).Error)
	require.NoError(t, db.Create(testutils.MakeFakeHistoryEntry("echo 'bar' &")).Error)

	// Check that they're there (and there aren't any other entries)
	var historyEntries []*data.HistoryEntry
	db.Model(&data.HistoryEntry{}).Find(&historyEntries)
	if len(historyEntries) != 5 {
		t.Fatalf("expected to find 6 history entries, actual found %d: %#v", len(historyEntries), historyEntries)
	}
	out := tester.RunInteractiveShell(t, `hishtory export`)
	testutils.CompareGoldens(t, out, "testControlR-InitialExport")

	// And check that the control-r binding brings up the search
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R"})
	split := strings.Split(out, "\n\n\n")
	out = strings.TrimSpace(split[len(split)-1])
	testutils.CompareGoldens(t, out, "testControlR-Initial")

	// And check that we can scroll down and select an option
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Down Down", "Enter"})
	if !strings.HasSuffix(out, " ls ~/bar/") {
		t.Fatalf("hishtory tquery returned the wrong result, out=%#v", out)
	}

	// And that the above works, but also with an ENTER to actually execute the selected command
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Down", "Enter", "Enter"})
	require.Contains(t, out, "echo 'aaaaaa bbbb'\naaaaaa bbbb\n", "hishtory tquery executed the wrong result")

	// Search for something more specific and select it
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "foo", "Enter"})
	if !strings.HasSuffix(out, " ls ~/foo/") {
		t.Fatalf("hishtory tquery returned the wrong result, out=%#v", out)
	}

	// Search for something more specific, and then unsearch, and then search for something else (using an alternate key binding for the down key)
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "fo", "BSpace BSpace", "bar", "C-N", "Enter"})
	if !strings.HasSuffix(out, " ls ~/bar/") {
		t.Fatalf("hishtory tquery returned the wrong result, out=%#v", out)
	}

	// Search using an atom
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "fo", "BSpace BSpace", "exit_code:2", "Enter"})
	if !strings.HasSuffix(out, " echo 'bar' &") {
		t.Fatalf("hishtory tquery returned the wrong result, out=%#v", out)
	}

	// Search and check that the table is updated
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "echo"}))
	testutils.CompareGoldens(t, out, "testControlR-Search")

	// An advanced search and check that the table is updated
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "cwd:/tmp/ SPACE ls"}))
	testutils.CompareGoldens(t, out, "testControlR-AdvancedSearch")

	// Set some different columns to be displayed and check that the table displays those
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns Hostname 'Exit Code' Command`)
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R"}))
	testutils.CompareGoldens(t, out, "testControlR-displayedColumns")

	// Add a custom column
	tester.RunInteractiveShell(t, `hishtory config-add custom-columns foo "echo foo"`)
	tester.RunInteractiveShell(t, ` hishtory enable`)
	tester.RunInteractiveShell(t, `ls /`)
	tester.RunInteractiveShell(t, ` hishtory disable`)

	// And run a query and confirm it is displayed
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns foo`)
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "-pipefail"}))
	testutils.CompareGoldens(t, out, "testControlR-customColumn")

	// Start with a search query, and then press control-r and it shows results for that query
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"ls", "C-R"}))
	testutils.CompareGoldens(t, out, "testControlR-InitialSearch")

	// Start with a search query, and then press control-r, then make the query more specific
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"e", "C-R", "cho"}))
	testutils.CompareGoldens(t, out, "testControlR-InitialSearchExpanded")

	// Start with a search query for which there are no results
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"asdf", "C-R"}))
	testutils.CompareGoldens(t, out, "testControlR-InitialSearchNoResults")

	// Start with a search query for which there are no results
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"asdf", "C-R", "BSpace BSpace BSpace BSpace echo"}))
	testutils.CompareGoldens(t, out, "testControlR-InitialSearchNoResultsThenFoundResults")

	// Search, hit control-c, and the table should be cleared
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"echo", "C-R", "c", "C-C"}))
	require.NotContains(t, out, "Search Query", "hishtory is showing a table even after control-c?")
	require.NotContains(t, out, "─────", "hishtory is showing a table even after control-c?")
	require.NotContains(t, out, "Exit Code", "hishtory is showing a table even after control-c?")
	if testutils.IsGithubAction() {
		if shellName == "fish" {
			require.Contains(t, out, "Welcome to fish, the friendly interactive shell")
			require.Contains(t, out, "> echo ")
		} else {
			testutils.CompareGoldens(t, out, "testControlR-ControlC-"+shellName+"-"+runtime.GOOS)
		}
	}

	// Disable control-r
	_, _ = tester.RunInteractiveShellRelaxed(t, `hishtory config-set enable-control-r false`)
	// And it shouldn't pop up
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R"})
	require.NotContains(t, out, "Search Query", "hishtory overrode control-r even when this was disabled?")
	require.NotContains(t, out, "─────", "hishtory overrode control-r even when this was disabled?")
	require.NotContains(t, out, "Exit Code", "hishtory overrode control-r even when this was disabled?")
	if testutils.IsGithubAction() && shellName != "fish" {
		testutils.CompareGoldens(t, out, "testControlR-"+shellName+"-Disabled-"+runtime.GOOS)
	}

	// Re-enable control-r
	_, err := tester.RunInteractiveShellRelaxed(t, `hishtory config-set enable-control-r true`)
	require.NoError(t, err)

	// And check that the control-r bindings work again
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "-pipefail SPACE -exit_code:0"}))
	testutils.CompareGoldens(t, out, "testControlR-Final")

	// Record a multi-line command
	tester.RunInteractiveShell(t, ` hishtory enable`)
	tester.RunInteractiveShell(t, `ls \
-Slah \
/`)
	tester.RunInteractiveShell(t, ` hishtory disable`)

	// Check that we display it in the table reasonably
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Slah"}))
	testutils.CompareGoldens(t, out, "testControlR-DisplayMultiline-"+shellName)

	// Check that we can select it correctly
	out = stripShellPrefix(captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Slah", "Enter"}))
	require.Contains(t, out, "-Slah", "out has unexpected output missing the selected row")
	if testutils.IsGithubAction() && shellName != "fish" {
		testutils.CompareGoldens(t, out, "testControlR-SelectMultiline-"+shellName+"-"+runtime.GOOS)
	}

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func testCustomColumns(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Record a few commands with no custom columns
	out := tester.RunInteractiveShell(t, `export FOOBAR='hello'
echo $FOOBAR world
cd /
echo baz`)
	if out != "hello world\nbaz\n" {
		t.Fatalf("unexpected command output=%#v", out)
	}

	// Check that the hishtory is saved correctly
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	testutils.CompareGoldens(t, out, "testCustomColumns-initHistory")

	// Configure a custom column
	tester.RunInteractiveShell(t, `hishtory config-add custom-columns git_remote '(git remote -v 2>/dev/null | grep origin 1>/dev/null ) && git remote get-url origin || true'`)

	// Run a few commands, some of which will have a git_remote
	out = tester.RunInteractiveShell(t, `echo foo
cd /
echo bar`)
	if out != "foo\nbar\n" {
		t.Fatalf("unexpected command output=%#v", out)
	}

	// And check that it is all recorded correctly
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns 'Exit Code' git_remote Command`)
	out = tester.RunInteractiveShell(t, `hishtory query -pipefail`)
	testutils.CompareGoldens(t, out, fmt.Sprintf("testCustomColumns-query-isAction=%v", testutils.IsGithubAction()))
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail ENTER"})
	out = stripRequiredPrefix(t, out, "hishtory tquery -pipefail")
	testName := "testCustomColumns-tquery-" + tester.ShellName()
	if testutils.IsGithubAction() {
		testName += "-isAction"
	}
	testutils.CompareGoldens(t, out, testName)

	// And check that we can delete the custom column and that it also gets automatically removed from displayed-columns
	require.Equal(t, `"Exit Code" git_remote Command`, strings.TrimSpace(tester.RunInteractiveShell(t, "hishtory config-get displayed-columns")))
	require.Equal(t, "git_remote:   (git remote -v 2>/dev/null | grep origin 1>/dev/null ) && git remote get-url origin || true", strings.TrimSpace(tester.RunInteractiveShell(t, "hishtory config-get custom-columns")))
	tester.RunInteractiveShell(t, `hishtory config-delete custom-columns git_remote`)
	require.Equal(t, `"Exit Code" Command`, strings.TrimSpace(tester.RunInteractiveShell(t, "hishtory config-get displayed-columns")))
}

func testPresavingDisabled(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Disable the presaving feature
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get presaving`)))
	tester.RunInteractiveShell(t, `hishtory config-set presaving false`)
	require.Equal(t, "false", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get presaving`)))

	// Start a command that will take a long time to execute in the background, so
	// we can check that it wasn't recorded even though it never finished.
	require.NoError(t, os.Chdir("/"))
	require.NoError(t, tester.RunInteractiveShellBackground(t, `sleep 13371338`))
	time.Sleep(time.Millisecond * 500)

	// Test that it shows up in hishtory export
	out := tester.RunInteractiveShell(t, ` hishtory export sleep -export`)
	expectedOutput := ""
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testPresavingOffline(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("HISHTORY_SIMULATE_NETWORK_ERROR")()
	userSecret := installHishtory(t, tester, "")

	// Enable the presaving feature
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get presaving`)))
	tester.RunInteractiveShell(t, `hishtory config-set presaving true`)
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get presaving`)))

	// Simulate a network error when presaving
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	require.NoError(t, os.Chdir("/"))
	require.NoError(t, tester.RunInteractiveShellBackground(t, `sleep 13371336`))

	// Check the exported data locally
	tester.RunInteractiveShell(t, ` hishtory config-set displayed-columns CWD Runtime Command`)
	out := tester.RunInteractiveShell(t, ` hishtory query sleep -tquery -query`)
	testutils.CompareGoldens(t, out, "testPresavingOffline-query-present")

	// And check it on another device where it isn't yet available
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")
	restoreDevice1 := testutils.BackupAndRestoreWithId(t, "device1")
	installHishtory(t, tester, userSecret)
	tester.RunInteractiveShell(t, ` hishtory config-set displayed-columns CWD Runtime Command`)
	out = tester.RunInteractiveShell(t, ` hishtory query sleep -tquery -query`)
	testutils.CompareGoldens(t, out, "testPresavingOffline-query-missing")

	// Then go back to the first device and restore the internet connection so that it uploads the presaved entry
	restoreDevice2 := testutils.BackupAndRestoreWithId(t, "device2")
	restoreDevice1()
	tester.RunInteractiveShell(t, `echo any_command_to_trigger_reupload`)
	out = tester.RunInteractiveShell(t, ` hishtory query sleep -tquery -query`)
	testutils.CompareGoldens(t, out, "testPresavingOffline-query-present")

	// And check that it is now present on the second device
	restoreDevice2()
	out = tester.RunInteractiveShell(t, ` hishtory query sleep -tquery -query`)
	testutils.CompareGoldens(t, out, "testPresavingOffline-query-present")
}

func testPresaving(t *testing.T, tester shellTester, shellName string) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")
	manuallySubmitHistoryEntry(t, userSecret, testutils.MakeFakeHistoryEntry("table_sizing aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	// Enable the presaving feature
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get presaving`)))
	tester.RunInteractiveShell(t, `hishtory config-set presaving true`)
	require.Equal(t, "true", strings.TrimSpace(tester.RunInteractiveShell(t, `hishtory config-get presaving`)))

	// Start a command that will take a long time to execute in the background, so
	// we can check that it was recorded even though it never finished.
	require.NoError(t, os.Chdir("/"))
	if tester.ShellName() == shellName {
		require.NoError(t, tester.RunInteractiveShellBackground(t, `sleep 13371337`))
	} else {
		tmuxCommandToRunInBackground := buildTmuxInputCommands(t, TmuxCaptureConfig{
			tester:              tester,
			overriddenShellName: shellName,
			commands:            []string{`sleep SPACE 13371337 ENTER`},
		})
		tester.RunInteractiveShell(t, tmuxCommandToRunInBackground)
	}
	time.Sleep(time.Millisecond * 500)

	// Test that it shows up in hishtory export
	out := tester.RunInteractiveShell(t, ` hishtory export sleep -export`)
	expectedOutput := "sleep 13371337\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Test that it shows up in hishtory query and that the runtime is displayed correctly
	tester.RunInteractiveShell(t, ` hishtory config-set displayed-columns CWD Runtime Command`)
	out = tester.RunInteractiveShell(t, ` hishtory query sleep 13371337 -export -tquery`)
	testutils.CompareGoldens(t, out, "testPresaving-query")

	// And then record a few other commands, and run an export of all commands, to ensure no funkiness happened
	tester.RunInteractiveShell(t, `ls /`)
	time.Sleep(time.Second)
	tester.RunInteractiveShell(t, `sleep 0.5`)
	out = tester.RunInteractiveShell(t, ` hishtory export -hishtory -table_sizing -pipefail`)
	expectedOutput = "sleep 13371337\nls /\nsleep 0.5\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Create a new device, and confirm it shows up there too
	restoreDevice1 := testutils.BackupAndRestoreWithId(t, "device1")
	installHishtory(t, tester, userSecret)
	tester.RunInteractiveShell(t, ` hishtory config-set displayed-columns CWD Runtime Command`)
	out = tester.RunInteractiveShell(t, ` hishtory query sleep 13371337 -export -tquery`)
	testutils.CompareGoldens(t, out, "testPresaving-query")

	// And that all the other commands do to
	out = tester.RunInteractiveShell(t, ` hishtory export -hishtory -table_sizing -pipefail`)
	expectedOutput = "sleep 13371337\nls /\nsleep 0.5\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// And then redact it from device2
	tester.RunInteractiveShell(t, ` HISHTORY_REDACT_FORCE=true hishtory redact sleep 13371337`)

	// And confirm it was redacted
	out = tester.RunInteractiveShell(t, ` hishtory export sleep -export`)
	require.Equal(t, "sleep 0.5\n", out)

	// Then go back to device1 and confirm it was redacted there too
	restoreDevice1()
	out = tester.RunInteractiveShell(t, ` hishtory export sleep -export`)
	require.Equal(t, "sleep 0.5\n", out)

	// And then record a few commands, and run a final export of all commands, to ensure no funkiness happened
	out = tester.RunInteractiveShell(t, ` hishtory export -hishtory -table_sizing -pipefail`)
	expectedOutput = "ls /\nsleep 0.5\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testTabCompletion(t *testing.T, tester shellTester, shellName string) {
	if shellName == "bash" {
		// TODO: Enable tab completions for bash by adding the below line to config.sh
		//   type _get_comp_words_by_ref &>/dev/null && source <(hishtory completion bash)
		t.Skip()
	}

	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Check that tab completions work to complete a command
	out := captureTerminalOutputWithShellName(t, tester, shellName, []string{"hishtory SPACE config-g Tab"})
	expected := "hishtory config-get"
	require.True(t, strings.HasSuffix(out, expected), fmt.Sprintf("Expected out=%#v to end with %#v", out, expected))

	// Check that tab completions work to view suggestions
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"hishtory SPACE config- Tab"})
	testutils.TestLog(t, "testTabCompletion: Pre-stripping: "+out)
	if shellName == "fish" {
		out = strings.Join(strings.Split(out, "\n")[3:], "\n")
	} else {
		out = strings.Join(strings.Split(out, "\n")[1:], "\n")
	}
	testutils.CompareGoldens(t, out, "testTabCompletion-suggestions-"+shellName)
}

func testUninstall(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Record a few commands and check that they get recorded
	tester.RunInteractiveShell(t, `echo foo
echo baz`)
	out := tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	testutils.CompareGoldens(t, out, "testUninstall-recorded")

	// And then uninstall
	out, err := tester.RunInteractiveShellRelaxed(t, `yes | hishtory uninstall`)
	require.NoError(t, err)
	testutils.CompareGoldens(t, out, "testUninstall-uninstall")

	// And check that hishtory has been uninstalled
	out, err = tester.RunInteractiveShellRelaxed(t, `echo foo
hishtory
echo bar`)
	require.NoError(t, err)
	testutils.CompareGoldens(t, out, "testUninstall-post-uninstall")

	// And check again, but in a way that shows the full terminal output
	if testutils.IsGithubAction() {
		out = captureTerminalOutput(t, tester, []string{
			"echo SPACE foo ENTER",
			"hishtory ENTER",
			"echo SPACE bar ENTER",
		})
		testutils.CompareGoldens(t, out, "testUninstall-post-uninstall-"+tester.ShellName()+"-"+runtime.GOOS)
	}
}

func TestTimestampFormat(t *testing.T) {
	// Setup
	markTestForSharding(t, 6)
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Add an entry just to ensure we get consistent table sizing
	tester.RunInteractiveShell(t, "echo tablesizing")

	// Add some entries with fixed timestamps
	tmz, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	entry1 := testutils.MakeFakeHistoryEntry("table_cmd1 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	entry1.StartTime = time.Unix(1650096186, 0).In(tmz)
	entry1.EndTime = time.Unix(1650096190, 0).In(tmz)
	manuallySubmitHistoryEntry(t, userSecret, entry1)
	entry2 := testutils.MakeFakeHistoryEntry("table_cmd2")
	entry2.StartTime = time.Unix(1650096196, 0).In(tmz)
	entry2.EndTime = time.Unix(1650096220, 0).In(tmz)
	entry2.CurrentWorkingDirectory = "~/foo/"
	entry2.ExitCode = 3
	manuallySubmitHistoryEntry(t, userSecret, entry2)

	// Check the init the timestamp format
	require.Equal(t, "Jan 2 2006 15:04:05 MST", strings.TrimSpace(tester.RunInteractiveShell(t, ` hishtory config-get timestamp-format`)))

	// Set a custom timestamp format
	tester.RunInteractiveShell(t, ` hishtory config-set timestamp-format '2006/Jan/2 15:04'`)

	// And check that it is displayed in both the tui and the classic view
	out := hishtoryQuery(t, tester, "-pipefail -tablesizing")
	testutils.CompareGoldens(t, out, "TestTimestampFormat-query")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail SPACE -tablesizing ENTER"})
	out = stripRequiredPrefix(t, out, "hishtory tquery -pipefail -tablesizing")
	testutils.CompareGoldens(t, out, "TestTimestampFormat-tquery")
}

func TestSortByConsistentTimezone(t *testing.T) {
	// Setup
	markTestForSharding(t, 7)
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Add an entry just to ensure we get consistent table sizing
	tester.RunInteractiveShell(t, "echo tablesizing")

	// Add some entries with timestamps in different timezones
	db := hctx.GetDb(hctx.MakeContext())
	timestamp := int64(1650096186)
	la_time, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	ny_time, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	entry1 := testutils.MakeFakeHistoryEntry("first_entry")
	entry1.StartTime = time.Unix(timestamp, 0).In(ny_time)
	entry1.EndTime = time.Unix(timestamp+1, 0).In(ny_time)
	require.NoError(t, lib.ReliableDbCreate(db, entry1))
	entry2 := testutils.MakeFakeHistoryEntry("second_entry")
	entry2.StartTime = time.Unix(timestamp+1000, 0).In(la_time)
	entry2.EndTime = time.Unix(timestamp+1001, 0).In(la_time)
	require.NoError(t, lib.ReliableDbCreate(db, entry2))
	entry3 := testutils.MakeFakeHistoryEntry("third_entry")
	entry3.StartTime = time.Unix(timestamp+2000, 0).In(ny_time)
	entry3.EndTime = time.Unix(timestamp+2001, 0).In(ny_time)
	require.NoError(t, lib.ReliableDbCreate(db, entry3))

	// And check that they're displayed in the correct order
	out := hishtoryQuery(t, tester, "-pipefail -tablesizing")
	testutils.CompareGoldens(t, out, "TestSortByConsistentTimezone-query")
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail -tablesizing`)
	testutils.CompareGoldens(t, out, "TestSortByConsistentTimezone-export")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail SPACE -tablesizing ENTER"})
	out = stripTuiCommandPrefix(t, out)
	require.Regexp(t, regexp.MustCompile(`Timestamp[\s\S]*Command[\s\S]*Apr 16 2022 01:36:26 PDT[\s\S]*third_entry[\s\S]*Apr 16 2022 01:19:46 PDT[\s\S]*second_entry[\s\S]*Apr 16 2022 01:03:06 PDT[\s\S]*first_entry`), out)
}

func TestZDotDir(t *testing.T) {
	// Setup
	markTestForSharding(t, 8)
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("ZDOTDIR")()
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	zdotdir := path.Join(homedir, "foo")
	require.NoError(t, os.MkdirAll(zdotdir, 0o744))
	os.Setenv("ZDOTDIR", zdotdir)
	userSecret := installHishtory(t, tester, "")
	defer func() {
		require.NoError(t, os.Remove(path.Join(zdotdir, ".zshrc")))
	}()

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Run a command and check that it was recorded
	tester.RunInteractiveShell(t, `echo foo`)
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail -install -status`)
	if out != "echo foo\n" {
		t.Fatalf("hishtory export had unexpected out=%#v", out)
	}

	// Check that hishtory respected ZDOTDIR
	zshrc, err := os.ReadFile(path.Join(zdotdir, ".zshrc"))
	require.NoError(t, err)
	require.Contains(t, string(zshrc), "# Hishtory Config:", "zshrc had unexpected contents")
}

func TestRemoveDuplicateRows(t *testing.T) {
	// Setup
	markTestForSharding(t, 9)
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Check the default
	require.Equal(t, "false", strings.TrimSpace(tester.RunInteractiveShell(t, ` hishtory config-get filter-duplicate-commands`)))

	// Record a few commands and check that they get recorded and all are displayed in a table
	tester.RunInteractiveShell(t, `echo foo
echo foo
echo baz 
echo baz
echo foo`)
	out := tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-export")
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns 'Exit Code' Command`)
	out = tester.RunInteractiveShell(t, `hishtory query -pipefail`)
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-query")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail ENTER"})
	out = stripRequiredPrefix(t, out, "hishtory tquery -pipefail")
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-tquery")

	// And change the config to filter out duplicate rows
	tester.RunInteractiveShell(t, `hishtory config-set filter-duplicate-commands true`)

	// Check export
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-export")

	// Check query
	out = tester.RunInteractiveShell(t, `hishtory query -pipefail`)
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-query")

	// Check tquery
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail ENTER"})
	out = stripRequiredPrefix(t, out, "hishtory tquery -pipefail")
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-tquery")

	// Check actually selecting it with query
	out = captureTerminalOutputWithComplexCommands(t, tester, []TmuxCommand{
		{Keys: "hishtory SPACE tquery SPACE -pipefail ENTER", ExtraDelay: 1.0},
		{Keys: "Down Down"},
		{Keys: "ENTER", ExtraDelay: 1.0},
	})
	out = stripTuiCommandPrefix(t, out)
	require.Contains(t, out, "\necho foo\n")
	require.NotContains(t, out, "echo baz")
	require.NotContains(t, out, "config-set")
}

func TestSetConfigNoCorruption(t *testing.T) {
	// Setup
	markTestForSharding(t, 10)
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// A test that tries writing a config many different times in parallel, and confirms there is no corruption
	conf, err := hctx.GetConfig()
	require.NoError(t, err)
	var doneWg sync.WaitGroup
	for i := 0; i < 10; i++ {
		doneWg.Add(1)
		go func(i int) {
			// Make a new config of a varied length
			c := conf
			c.LastSavedHistoryLine = strings.Repeat("A", i)
			c.DeviceId = strings.Repeat("B", i*2)
			c.HaveMissedUploads = (i % 2) == 0
			// Write it
			require.NoError(t, hctx.SetConfig(&c))
			require.NoError(t, err)
			// Check that we can read
			c2, err := hctx.GetConfig()
			require.NoError(t, err)
			if c2.UserSecret != c.UserSecret {
				panic("user secret mismatch")
			}
			doneWg.Done()
		}(i)
	}
	doneWg.Wait()
}

// Test that the config retrieved from the context is a reference and there are no consistency issues with it getting out of sync
func TestCtxConfigIsReference(t *testing.T) {
	// Setup
	markTestForSharding(t, 11)
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Get two copies of the conifg
	ctx := hctx.MakeContext()
	c1 := hctx.GetConf(ctx)
	c2 := hctx.GetConf(ctx)
	require.Equal(t, *c1, *c2)

	// Change one and check that the other is changed
	c1.LastSavedHistoryLine = "foobar"
	require.Equal(t, c1.LastSavedHistoryLine, "foobar")
	require.Equal(t, c2.LastSavedHistoryLine, "foobar")

	// Persist that one, and then get the config again, and that one should also contain the change
	require.NoError(t, hctx.SetConfig(c1))
	c3 := hctx.GetConf(ctx)
	require.Equal(t, *c1, *c3)
	require.Equal(t, c1.LastSavedHistoryLine, "foobar")
	require.Equal(t, c2.LastSavedHistoryLine, "foobar")
	require.Equal(t, c3.LastSavedHistoryLine, "foobar")
}

func testMultipleUsers(t *testing.T, tester shellTester) {
	defer testutils.BackupAndRestore(t)()

	// Create all our devices
	var deviceMap map[device]deviceOp = make(map[device]deviceOp)
	var devices deviceSet = deviceSet{}
	devices.deviceMap = &deviceMap
	devices.currentDevice = nil
	u1d1 := device{key: "user1", deviceId: "1"}
	createDevice(t, tester, &devices, u1d1.key, u1d1.deviceId)
	u1d2 := device{key: "user1", deviceId: "2"}
	createDevice(t, tester, &devices, u1d2.key, u1d2.deviceId)
	u2d1 := device{key: "user2", deviceId: "1"}
	createDevice(t, tester, &devices, u2d1.key, u2d1.deviceId)
	u2d2 := device{key: "user2", deviceId: "2"}
	createDevice(t, tester, &devices, u2d2.key, u2d2.deviceId)
	u2d3 := device{key: "user2", deviceId: "3"}
	createDevice(t, tester, &devices, u2d3.key, u2d3.deviceId)

	// Run commands on user1
	switchToDevice(&devices, u1d1)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d1`)
	switchToDevice(&devices, u1d2)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d2`)

	// Run commands on user2
	switchToDevice(&devices, u2d1)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u2d1`)
	switchToDevice(&devices, u2d2)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u2d2`)
	switchToDevice(&devices, u2d3)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u2d3`)

	// Run more commands on user1
	switchToDevice(&devices, u1d1)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d1-b`)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d1-c`)
	switchToDevice(&devices, u1d2)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d2-b`)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d2-c`)

	// Check that the right commands were recorded for user1
	for _, d := range []device{u1d1, u1d2} {
		switchToDevice(&devices, d)
		out, err := tester.RunInteractiveShellRelaxed(t, `hishtory export -pipefail -export`)
		require.NoError(t, err)
		expectedOutput := "echo u1d1\necho u1d2\necho u1d1-b\necho u1d1-c\necho u1d2-b\necho u1d2-c\n"
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
		}
	}

	// Run more commands on user2
	switchToDevice(&devices, u2d1)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d1-b`)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u1d1-c`)
	switchToDevice(&devices, u2d3)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u2d3-b`)
	_, _ = tester.RunInteractiveShellRelaxed(t, `echo u2d3-c`)

	// Check that the right commands were recorded for user2
	for _, d := range []device{u2d1, u2d2, u2d3} {
		switchToDevice(&devices, d)
		out, err := tester.RunInteractiveShellRelaxed(t, `hishtory export -export -pipefail`)
		require.NoError(t, err)
		expectedOutput := "echo u2d1\necho u2d2\necho u2d3\necho u1d1-b\necho u1d1-c\necho u2d3-b\necho u2d3-c\n"
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
		}
	}
}

func createSyntheticImportEntries(t testing.TB, numSyntheticEntries int) {
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	f, err := os.OpenFile(path.Join(homedir, ".bash_history"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()
	for i := 1; i <= numSyntheticEntries; i++ {
		_, err := f.WriteString(fmt.Sprintf("echo command-%d\n", i))
		require.NoError(t, err)
	}
	require.NoError(t, f.Close())
}

func TestImportHistory(t *testing.T) {
	// Setup
	markTestForSharding(t, 11)
	tester := bashTester{}
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")
	numSyntheticEntries := 305
	createSyntheticImportEntries(t, numSyntheticEntries)

	// Run the import
	ctx := hctx.MakeContext()
	numImported, err := lib.ImportHistory(ctx, false, true)
	require.NoError(t, err)
	require.Equal(t, numImported, numSyntheticEntries+1)

	// Check that it imported all of them
	out := tester.RunInteractiveShell(t, ` hishtory export -pipefail`)
	testutils.CompareGoldens(t, out, "TestImportHistory-export")

	// Check that it was uploaded so that another user can get it
	testutils.ResetLocalState(t)
	installHishtory(t, tester, userSecret)
	out = strings.TrimSpace(tester.RunInteractiveShell(t, ` hishtory export -pipefail | wc -l`))
	require.Equal(t, "305", out)
	out = tester.RunInteractiveShell(t, ` hishtory export -pipefail`)
	require.Contains(t, out, "echo command-305")
	out = tester.RunInteractiveShell(t, ` hishtory export -pipefail`)
	testutils.CompareGoldens(t, out, "TestImportHistory-export")
}

func BenchmarkImport(b *testing.B) {
	b.StopTimer()
	// Setup
	tester := bashTester{}
	defer testutils.BackupAndRestore(b)()

	// Benchmark it
	for n := 0; n < b.N; n++ {
		// Setup
		testutils.ResetLocalState(b)
		installHishtory(b, tester, "")

		// Create a large history in bash that we will pre-import
		numSyntheticEntries := 100_000
		createSyntheticImportEntries(b, numSyntheticEntries)

		// Benchmarked code:
		b.StartTimer()
		ctx := hctx.MakeContext()
		numImported, err := lib.ImportHistory(ctx, false, true)
		require.NoError(b, err)
		require.GreaterOrEqual(b, numImported, numSyntheticEntries)
		b.StopTimer()
	}
}

func TestAugmentedIsOfflineError(t *testing.T) {
	markTestForSharding(t, 12)
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, zshTester{}, "")
	defer testutils.BackupAndRestoreEnv("HISHTORY_SIMULATE_NETWORK_ERROR")()
	ctx := hctx.MakeContext()

	// By default, when the hishtory server is up, then IsOfflineError checks the error msg
	require.True(t, lib.CanReachHishtoryServer(ctx))
	require.False(t, lib.IsOfflineError(ctx, fmt.Errorf("unchecked error type")))

	// When the hishtory server is down, then all error messages are treated as being due to offline errors
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "1")
	require.False(t, lib.CanReachHishtoryServer(ctx))
	require.True(t, lib.IsOfflineError(ctx, fmt.Errorf("unchecked error type")))
}

func TestWebUi(t *testing.T) {
	markTestForSharding(t, 13)
	defer testutils.BackupAndRestore(t)()
	tester := zshTester{}
	installHishtory(t, tester, "")

	// Run a few commands to search for
	tester.RunInteractiveShell(t, `echo foobar`)

	// Start the server
	require.NoError(t, tester.RunInteractiveShellBackground(t, `hishtory start-web-ui --force-creds hishtory:my_password`))
	time.Sleep(time.Second)
	defer tester.RunInteractiveShell(t, `killall hishtory`)

	// And check that the server seems to be returning valid data
	req, err := http.NewRequest("GET", "http://localhost:8000?q=foobar", nil)
	require.NoError(t, err)
	req.SetBasicAuth("hishtory", "my_password")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(respBody), "echo foobar")

	// And that it rejects requests without auth
	resp, err = http.Get("http://localhost:8000?q=foobar")
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)

	// And requests with incorrect auth
	req, err = http.NewRequest("GET", "http://localhost:8000?q=foobar", nil)
	require.NoError(t, err)
	req.SetBasicAuth("hishtory", "wrong-password")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
}

// TODO: somehow test/confirm that hishtory works even if only bash/only zsh is installed
