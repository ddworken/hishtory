package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/ddworken/hishtory/client/ctx"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
)

func TestMain(m *testing.M) {
	defer shared.RunTestServer()()
	cmd := exec.Command("go", "build", "-o", "/tmp/client")
	err := cmd.Run()
	if err != nil {
		panic(fmt.Sprintf("failed to build client: %v", err))
	}
	m.Run()
}

type shellTester interface {
	RunInteractiveShell(t *testing.T, script string) string
	RunInteractiveShellRelaxed(t *testing.T, script string) (string, error)
	ShellName() string
}
type bashTester struct {
	shellTester
}

func (b bashTester) RunInteractiveShell(t *testing.T, script string) string {
	out, err := b.RunInteractiveShellRelaxed(t, "set -emo pipefail\n"+script)
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		t.Fatalf("error when running command at %s:%d: %v", filename, line, err)
	}
	return out
}

func (b bashTester) RunInteractiveShellRelaxed(t *testing.T, script string) (string, error) {
	cmd := exec.Command("bash", "-i")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("unexpected error when running commands, out=%#v, err=%#v: %v", stdout.String(), stderr.String(), err)
	}
	outStr := stdout.String()
	if strings.Contains(outStr, "hishtory fatal error") {
		t.Fatalf("Ran command, but hishtory had a fatal error! out=%#v", outStr)
	}
	return outStr, nil
}

func (b bashTester) ShellName() string {
	return "bash"
}

type zshTester struct {
	shellTester
}

func (z zshTester) RunInteractiveShell(t *testing.T, script string) string {
	res, err := z.RunInteractiveShellRelaxed(t, "set -eo pipefail\n"+script)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (z zshTester) RunInteractiveShellRelaxed(t *testing.T, script string) (string, error) {
	cmd := exec.Command("zsh", "-is")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("unexpected error when running command=%#v, out=%#v, err=%#v: %v", script, stdout.String(), stderr.String(), err)
	}
	outStr := stdout.String()
	if strings.Contains(outStr, "hishtory fatal error") {
		t.Fatalf("Ran command, but hishtory had a fatal error! out=%#v", outStr)
	}
	return outStr, nil
}

func (z zshTester) ShellName() string {
	return "zsh"
}

var shellTesters []shellTester = []shellTester{bashTester{}, zshTester{}}

func TestParameterized(t *testing.T) {
	for _, tester := range shellTesters {
		t.Run("testRepeatedCommandThenQuery/"+tester.ShellName(), func(t *testing.T) { testRepeatedCommandThenQuery(t, tester) })
		t.Run("testRepeatedCommandAndQuery/"+tester.ShellName(), func(t *testing.T) { testRepeatedCommandAndQuery(t, tester) })
		t.Run("testRepeatedEnableDisable/"+tester.ShellName(), func(t *testing.T) { testRepeatedEnableDisable(t, tester) })
		t.Run("testExcludeHiddenCommand/"+tester.ShellName(), func(t *testing.T) { testExcludeHiddenCommand(t, tester) })
		t.Run("testUpdate/"+tester.ShellName(), func(t *testing.T) { testUpdate(t, tester) })
		t.Run("testAdvancedQuery/"+tester.ShellName(), func(t *testing.T) { testAdvancedQuery(t, tester) })
		t.Run("testIntegration/"+tester.ShellName(), func(t *testing.T) { testIntegration(t, tester) })
		t.Run("testIntegrationWithNewDevice/"+tester.ShellName(), func(t *testing.T) { testIntegrationWithNewDevice(t, tester) })
		t.Run("testHishtoryBackgroundSaving/"+tester.ShellName(), func(t *testing.T) { testHishtoryBackgroundSaving(t, tester) })
		t.Run("testDisplayTable/"+tester.ShellName(), func(t *testing.T) { testDisplayTable(t, tester) })
		t.Run("testTableDisplayCwd/"+tester.ShellName(), func(t *testing.T) { testTableDisplayCwd(t, tester) })
		t.Run("testTimestampsAreReasonablyCorrect/"+tester.ShellName(), func(t *testing.T) { testTimestampsAreReasonablyCorrect(t, tester) })
		t.Run("testRequestAndReceiveDbDump/"+tester.ShellName(), func(t *testing.T) { testRequestAndReceiveDbDump(t, tester) })
		t.Run("testInstallViaPythonScript/"+tester.ShellName(), func(t *testing.T) { testInstallViaPythonScript(t, tester) })
		t.Run("testExportWithQuery/"+tester.ShellName(), func(t *testing.T) { testExportWithQuery(t, tester) })
		t.Run("testHelpCommand/"+tester.ShellName(), func(t *testing.T) { testHelpCommand(t, tester) })
		t.Run("testStripBashTimePrefix/"+tester.ShellName(), func(t *testing.T) { testStripBashTimePrefix(t, tester) })
		t.Run("testReuploadHistoryEntries/"+tester.ShellName(), func(t *testing.T) { testReuploadHistoryEntries(t, tester) })
		t.Run("testInitialHistoryImport/"+tester.ShellName(), func(t *testing.T) { testInitialHistoryImport(t, tester) })
		t.Run("testLocalRedaction/"+tester.ShellName(), func(t *testing.T) { testLocalRedaction(t, tester) })
		t.Run("testRemoteRedaction/"+tester.ShellName(), func(t *testing.T) { testRemoteRedaction(t, tester) })
	}
}

func testIntegration(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Run the test
	testBasicUserFlow(t, tester)
}

func testIntegrationWithNewDevice(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Run the test
	userSecret := testBasicUserFlow(t, tester)

	// Clear all local state
	shared.ResetLocalState(t)

	// Install it again
	installHishtory(t, tester, userSecret)

	// Querying should show the history from the previous run
	out := hishtoryQuery(t, tester, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}

	tester.RunInteractiveShell(t, "echo mynewcommand")
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "echo mynewcommand") {
		t.Fatalf("output is missing `echo mynewcommand`")
	}
	if strings.Count(out, "echo mynewcommand") != 1 {
		t.Fatalf("output has `echo mynewcommand` the wrong number of times")
	}

	// Clear local state again
	shared.ResetLocalState(t)

	// Install it a 3rd time
	installHishtory(t, tester, "adifferentsecret")

	// Run a command that shouldn't be in the hishtory later on
	tester.RunInteractiveShell(t, `echo notinthehistory`)
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "echo notinthehistory") {
		t.Fatalf("output is missing `echo notinthehistory`")
	}

	// Set the secret key to the previous secret key
	out = tester.RunInteractiveShell(t, `hishtory init `+userSecret)
	if !strings.Contains(out, "Setting secret hishtory key to "+userSecret) {
		t.Fatalf("Failed to re-init with the user secret: %v", out)
	}

	// Querying should show the history from the previous run
	out = hishtoryQuery(t, tester, "")
	expected = []string{"echo thisisrecorded", "echo mynewcommand", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}
	// But not from the previous account
	if strings.Contains(out, "notinthehistory") {
		t.Fatalf("output contains the unexpected item: notinthehistory")
	}

	tester.RunInteractiveShell(t, "echo mynewercommand")
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "echo mynewercommand") {
		t.Fatalf("output is missing `echo mynewercommand`")
	}
	if strings.Count(out, "echo mynewercommand") != 1 {
		t.Fatalf("output has `echo mynewercommand` the wrong number of times")
	}

	// Manually submit an event that isn't in the local DB, and then we'll
	// check if we see it when we do a query without ever having done an init
	newEntry := data.MakeFakeHistoryEntry("othercomputer")
	manuallySubmitHistoryEntry(t, userSecret, newEntry)

	// Now check if that is in there when we do hishtory query
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "othercomputer") {
		t.Fatalf("hishtory query doesn't contain cmd run on another machine! out=%#v", out)
	}

	// Finally, test the export command
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail | grep -v '/tmp/client install'`)
	if strings.Contains(out, "thisisnotrecorded") {
		t.Fatalf("hishtory export contains a command that should not have been recorded, out=%#v", out)
	}
	expectedOutputWithoutKey := "hishtory status\nhishtory query\nls /a\nls /bar\nls /foo\necho foo\necho bar\nhishtory enable\necho thisisrecorded\nhishtory query\nhishtory query foo\necho hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\"\nhishtory query complex\nhishtory query\necho mynewcommand\nhishtory query\nhishtory init %s\nhishtory query\necho mynewercommand\nhishtory query\nothercomputer\nhishtory query\n"
	expectedOutput := fmt.Sprintf(expectedOutputWithoutKey, userSecret)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func installHishtory(t *testing.T, tester shellTester, userSecret string) string {
	out := tester.RunInteractiveShell(t, `/tmp/client install `+userSecret)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output: matches=%#v", matches)
	}
	return matches[1]
}

func testBasicUserFlow(t *testing.T, tester shellTester) string {
	// Test install
	userSecret := installHishtory(t, tester, "")

	// Test the status subcommand
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Assert that hishtory is correctly using the dev config.sh
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get homedir: %v", err)
	}
	dat, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, "config.sh"))
	if err != nil {
		t.Fatalf("failed to read config.sh: %v", err)
	}
	if !strings.Contains(string(dat), "except it doesn't run the save process in the background") {
		t.Fatalf("config.sh is the prod version when it shouldn't be, config.sh=%#v", dat)
	}

	// Test the banner
	os.Setenv("FORCED_BANNER", "HELLO_FROM_SERVER")
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "HELLO_FROM_SERVER\nHostname") {
		t.Fatalf("hishtory query didn't show the banner message! out=%#v", out)
	}
	os.Setenv("FORCED_BANNER", "")

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
	if err != nil {
		t.Fatal(err)
	}
	if out != "foo\nbar\nthisisnotrecorded\nthisisrecorded\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}

	// Test the actual table output
	hostnameMatcher := `\S+`
	tableDividerMatcher := `\s+`
	pathMatcher := `~?/[a-zA-Z_0-9/-]+`
	datetimeMatcher := `[a-zA-Z]{3}\s\d{2}\s\d{4}\s[0-9:]+\s[A-Z]{3}`
	runtimeMatcher := `[0-9.ms]+`
	exitCodeMatcher := `0`
	pipefailMatcher := `set -em?o pipefail`
	line1Matcher := `Hostname` + tableDividerMatcher + `CWD` + tableDividerMatcher + `Timestamp` + tableDividerMatcher + `Runtime` + tableDividerMatcher + `Exit Code` + tableDividerMatcher + `Command` + tableDividerMatcher + `\n`
	line2Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + runtimeMatcher + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + pipefailMatcher + tableDividerMatcher + `\n`
	line3Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + runtimeMatcher + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + `echo thisisrecorded` + tableDividerMatcher + `\n`
	match, err := regexp.MatchString(line3Matcher, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("output is missing the row for `echo thisisrecorded`: %v", out)
	}
	match, err = regexp.MatchString(line1Matcher+line2Matcher+line3Matcher, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("output doesn't match the expected table: %v", out)
	}

	// Test querying for a specific command
	out = hishtoryQuery(t, tester, "foo")
	expected = []string{"echo foo", "ls /foo"}
	unexpected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}
	for _, item := range unexpected {
		if strings.Contains(out, item) {
			t.Fatalf("output is containing unexpected item %#v: %#v", item, out)
		}
	}

	// Add a complex command
	complexCommand := "echo hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\""
	_, _ = tester.RunInteractiveShellRelaxed(t, complexCommand)

	// Query for it
	out = hishtoryQuery(t, tester, "complex")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	if !strings.Contains(out, complexCommand) {
		t.Fatalf("hishtory query doesn't contain the expected complex command, out=%#v", out)
	}

	return userSecret
}

func testAdvancedQuery(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Install hishtory
	userSecret := installHishtory(t, tester, "")

	// Run some commands we can query for
	_, err := tester.RunInteractiveShellRelaxed(t, `echo nevershouldappear
notacommand
cd /tmp/
echo querybydir
hishtory disable`)
	if err != nil {
		t.Fatal(err)
	}

	// A super basic query just to ensure the basics are working
	out := hishtoryQuery(t, tester, `echo`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching echo querybydir, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd
	out = hishtoryQuery(t, tester, `cwd:/tmp`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching cwd:/tmp, out=%#v", out)
	}
	if strings.Contains(out, "nevershouldappear") {
		t.Fatalf("hishtory query contains unexpected entry, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	// And again, but with a strailing slash
	out = hishtoryQuery(t, tester, `cwd:/tmp/`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching cwd:/tmp, out=%#v", out)
	}
	if strings.Contains(out, "nevershouldappear") {
		t.Fatalf("hishtory query contains unexpected entry, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd without the slash
	out = hishtoryQuery(t, tester, `cwd:tmp`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching cwd:tmp, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd and another term
	out = hishtoryQuery(t, tester, `cwd:/tmp querybydir`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching cwd:/tmp, out=%#v", out)
	}
	if strings.Contains(out, "nevershouldappear") {
		t.Fatalf("hishtory query contains unexpected entry, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on exit_code
	out = hishtoryQuery(t, tester, `exit_code:127`)
	if !strings.Contains(out, "notacommand") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
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
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = hishtoryQuery(t, tester, `before:2125-07-02 cwd:tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = hishtoryQuery(t, tester, `before:2125-07-02 cwd:mp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: and cwd:
	out = hishtoryQuery(t, tester, `after:1980-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: that returns no results
	out = hishtoryQuery(t, tester, `after:2120-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Manually submit an entry with a different hostname and username so we can test those atoms
	entry := data.MakeFakeHistoryEntry("cmd_with_diff_hostname_and_username")
	entry.LocalUsername = "otheruser"
	entry.Hostname = "otherhostname"
	manuallySubmitHistoryEntry(t, userSecret, entry)

	// Query based on the username that exists
	out = hishtoryQuery(t, tester, `user:otheruser`)
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
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
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	out = hishtoryQuery(t, tester, `-cmd_with_diff_hostname_and_username`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = hishtoryQuery(t, tester, `-echo`)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out with an atom
	out = hishtoryQuery(t, tester, `-hostname:otherhostname`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = hishtoryQuery(t, tester, `-user:otheruser`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = hishtoryQuery(t, tester, `-exit_code:0`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item that also looks like it could be a search for a flag
	entry = data.MakeFakeHistoryEntry("foo -echo")
	manuallySubmitHistoryEntry(t, userSecret, entry)
	out = hishtoryQuery(t, tester, `-echo -install`)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Search for a cwd based on the home directory
	entry = data.MakeFakeHistoryEntry("foobar")
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
}

func testUpdate(t *testing.T, tester shellTester) {
	if !shared.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		t.Skip("skipping on linux/arm64 which is unsupported")
	}
	// Set up
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Record a command before the update
	tester.RunInteractiveShell(t, "echo hello")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Update
	out = tester.RunInteractiveShell(t, `hishtory update`)
	isExpected, err := regexp.MatchString(`Successfully updated hishtory from v0[.]Unknown to v0.\d+`, out)
	if err != nil {
		t.Fatalf("regex failure: %v", err)
	}
	if !isExpected {
		t.Fatalf("hishtory update returned unexpected out=%#v", out)
	}

	// Update again and assert that it skipped the update
	out = tester.RunInteractiveShell(t, `hishtory update`)
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "is already installed") {
		t.Fatalf("repeated hishtory update didn't skip the update, out=%#v", out)
	}

	// Then check the status command again to confirm the update worked
	out = tester.RunInteractiveShell(t, `hishtory status`)
	if !strings.Contains(out, fmt.Sprintf("\nEnabled: true\nSecret Key: %s\nCommit Hash: ", userSecret)) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}
	if strings.Contains(out, "\nCommit Hash: Unknown\n") {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Check that the history was preserved after the update
	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo hello\nhishtory status\nhishtory update\nhishtory update\nhishtory status\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testRepeatedCommandThenQuery(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
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
	if strings.Count(out, "echo mycommand") != 25 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}

	// Run a few more commands
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
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Run a command many times
	for i := 0; i < 25; i++ {
		tester.RunInteractiveShell(t, fmt.Sprintf("echo mycommand-%d", i))
		out = hishtoryQuery(t, tester, fmt.Sprintf("mycommand-%d", i))
		if strings.Count(out, "\n") != 2 {
			t.Fatalf("hishtory query #%d has the wrong number of lines=%d, out=%#v", i, strings.Count(out, "\n"), out)
		}
		if strings.Count(out, "echo mycommand") != 1 {
			t.Fatalf("hishtory query #%d has the wrong number of commands=%d, out=%#v", i, strings.Count(out, "echo mycommand"), out)
		}

	}
}

func testRepeatedEnableDisable(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Run a command many times
	for i := 0; i < 25; i++ {
		tester.RunInteractiveShell(t, fmt.Sprintf(`echo mycommand-%d
hishtory disable
echo shouldnotshowup
sleep 0.5
hishtory enable`, i))
		out := hishtoryQuery(t, tester, fmt.Sprintf("mycommand-%d", i))
		if strings.Count(out, "\n") != 2 {
			t.Fatalf("hishtory query #%d has the wrong number of lines=%d, out=%#v", i, strings.Count(out, "\n"), out)
		}
		if strings.Count(out, "echo mycommand") != 1 {
			t.Fatalf("hishtory query #%d has the wrong number of commands=%d, out=%#v", i, strings.Count(out, "echo mycommand"), out)
		}
		out = hishtoryQuery(t, tester, "")
		if strings.Contains(out, "shouldnotshowup") {
			t.Fatalf("hishtory query contains a result that should not have been recorded, out=%#v", out)
		}
	}

	out := tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo mycommand-0\nhishtory enable\nhishtory query mycommand-0\nhishtory query\necho mycommand-1\nhishtory enable\nhishtory query mycommand-1\nhishtory query\necho mycommand-2\nhishtory enable\nhishtory query mycommand-2\nhishtory query\necho mycommand-3\nhishtory enable\nhishtory query mycommand-3\nhishtory query\necho mycommand-4\nhishtory enable\nhishtory query mycommand-4\nhishtory query\necho mycommand-5\nhishtory enable\nhishtory query mycommand-5\nhishtory query\necho mycommand-6\nhishtory enable\nhishtory query mycommand-6\nhishtory query\necho mycommand-7\nhishtory enable\nhishtory query mycommand-7\nhishtory query\necho mycommand-8\nhishtory enable\nhishtory query mycommand-8\nhishtory query\necho mycommand-9\nhishtory enable\nhishtory query mycommand-9\nhishtory query\necho mycommand-10\nhishtory enable\nhishtory query mycommand-10\nhishtory query\necho mycommand-11\nhishtory enable\nhishtory query mycommand-11\nhishtory query\necho mycommand-12\nhishtory enable\nhishtory query mycommand-12\nhishtory query\necho mycommand-13\nhishtory enable\nhishtory query mycommand-13\nhishtory query\necho mycommand-14\nhishtory enable\nhishtory query mycommand-14\nhishtory query\necho mycommand-15\nhishtory enable\nhishtory query mycommand-15\nhishtory query\necho mycommand-16\nhishtory enable\nhishtory query mycommand-16\nhishtory query\necho mycommand-17\nhishtory enable\nhishtory query mycommand-17\nhishtory query\necho mycommand-18\nhishtory enable\nhishtory query mycommand-18\nhishtory query\necho mycommand-19\nhishtory enable\nhishtory query mycommand-19\nhishtory query\necho mycommand-20\nhishtory enable\nhishtory query mycommand-20\nhishtory query\necho mycommand-21\nhishtory enable\nhishtory query mycommand-21\nhishtory query\necho mycommand-22\nhishtory enable\nhishtory query mycommand-22\nhishtory query\necho mycommand-23\nhishtory enable\nhishtory query mycommand-23\nhishtory query\necho mycommand-24\nhishtory enable\nhishtory query mycommand-24\nhishtory query\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testExcludeHiddenCommand(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()
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
	if strings.Contains(out, "hidden") {
		t.Fatalf("hishtory query contains a result that should not have been recorded, out=%#v", out)
	}

	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo hello1\necho hello2\n"
	if out != expectedOutput {
		t.Fatalf("hishtory export has unexpected output=%#v", out)
	}
}

func getPidofCommand() string {
	if runtime.GOOS == "darwin" {
		// MacOS doesn't have pidof by default
		return "pgrep"
	}
	return "pidof"
}

func waitForBackgroundSavesToComplete(t *testing.T) {
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
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("failed to wait until hishtory wasn't running")
}

func hishtoryQuery(t *testing.T, tester shellTester, query string) string {
	return tester.RunInteractiveShell(t, "hishtory query "+query)
}

func manuallySubmitHistoryEntry(t *testing.T, userSecret string, entry data.HistoryEntry) {
	encEntry, err := data.EncryptHistoryEntry(userSecret, entry)
	shared.Check(t, err)
	if encEntry.Date != entry.EndTime {
		t.Fatalf("encEntry.Date does not match the entry")
	}
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	shared.Check(t, err)
	resp, err := http.Post("http://localhost:8080/api/v1/submit", "application/json", bytes.NewBuffer(jsonValue))
	shared.Check(t, err)
	if resp.StatusCode != 200 {
		t.Fatalf("failed to submit result to backend, status_code=%d", resp.StatusCode)
	}
}

func testTimestampsAreReasonablyCorrect(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Record a command
	out := tester.RunInteractiveShell(t, "echo hello")
	if out != "hello\n" {
		t.Fatalf("running echo hello had unexpected out=%#v", out)
	}

	// Query for it and check that the timestamp that gets recorded looks reasonable
	out = hishtoryQuery(t, tester, "echo hello")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	expectedDate := time.Now().Format("Jan 2 2006")
	if !strings.Contains(out, expectedDate) {
		t.Fatalf("hishtory query has an incorrect date: out=%#v", out)
	}
}

func testTableDisplayCwd(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()
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
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	if !strings.Contains(out, "~/.hishtory") {
		t.Fatalf("hishtory query has an incorrect CWD: out=%#v", out)
	}
	out = hishtoryQuery(t, tester, "echo other")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	if !strings.Contains(out, "/tmp") {
		t.Fatalf("hishtory query has an incorrect CWD: out=%#v", out)
	}
}

func testHishtoryBackgroundSaving(t *testing.T, tester shellTester) {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		t.Skip("skip testing background saving since it is too flakey on M1")
	}

	// Setup
	defer shared.BackupAndRestore(t)()

	// Test install with an unset HISHTORY_TEST var so that we save in the background (this is likely to be flakey!)
	out := tester.RunInteractiveShell(t, `unset HISHTORY_TEST
go build -o /tmp/client
/tmp/client install`)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output: matches=%#v", matches)
	}
	userSecret := matches[1]

	// Assert that config.sh isn't the dev version
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get homedir: %v", err)
	}
	dat, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, "config.sh"))
	if err != nil {
		t.Fatalf("failed to read config.sh: %v", err)
	}
	if strings.Contains(string(dat), "except it doesn't run the save process in the background") {
		t.Fatalf("config.sh is the testing version when it shouldn't be, config.sh=%#v", dat)
	}

	// Test the status subcommand
	out = tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Test recording commands
	out, err = tester.RunInteractiveShellRelaxed(t, `ls /a
echo foo`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "foo\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	waitForBackgroundSavesToComplete(t)
	time.Sleep(time.Second)
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo foo", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}

	// Test querying for a specific command
	waitForBackgroundSavesToComplete(t)
	out = hishtoryQuery(t, tester, "foo")
	if !strings.Contains(out, "echo foo") {
		t.Fatalf("output doesn't contain the expected item, out=%#v", out)
	}
	if strings.Contains(out, "ls /a") {
		t.Fatalf("output contains unexpected item, out=%#v", out)
	}
}

func testDisplayTable(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Submit two fake entries
	tmz, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}
	entry1 := data.MakeFakeHistoryEntry("table_cmd1")
	entry1.StartTime = time.Unix(1650096186, 0).In(tmz)
	entry1.EndTime = time.Unix(1650096190, 0).In(tmz)
	manuallySubmitHistoryEntry(t, userSecret, entry1)
	entry2 := data.MakeFakeHistoryEntry("table_cmd2")
	entry2.StartTime = time.Unix(1650096196, 0).In(tmz)
	entry2.EndTime = time.Unix(1650096220, 0).In(tmz)
	entry2.CurrentWorkingDirectory = "~/foo/"
	entry2.ExitCode = 3
	manuallySubmitHistoryEntry(t, userSecret, entry2)

	// Query and check the table
	out := hishtoryQuery(t, tester, "table")
	expectedOutput1 := "Hostname   CWD     Timestamp                   Runtime  Exit Code  Command     \nlocalhost  ~/foo/  Apr 16 2022 01:03:16 -0700  24s      3          table_cmd2  \nlocalhost  /tmp/   Apr 16 2022 01:03:06 -0700  4s       2          table_cmd1  \n"
	expectedOutput2 := "Hostname   CWD     Timestamp                 Runtime  Exit Code  Command     \nlocalhost  ~/foo/  Apr 16 2022 01:03:16 PDT  24s      3          table_cmd2  \nlocalhost  /tmp/   Apr 16 2022 01:03:06 PDT  4s       2          table_cmd1  \n"
	if out != expectedOutput1 && out != expectedOutput2 {
		t.Fatalf("hishtory query table test mismatch out=%#v", out)
	}
}

func testRequestAndReceiveDbDump(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()
	secretKey := installHishtory(t, tester, "")

	// Confirm there are no pending dump requests
	config, err := ctx.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	deviceId1 := config.DeviceId
	resp, err := lib.ApiGet("/api/v1/get-dump-requests?user_id=" + data.UserId(secretKey) + "&device_id=" + deviceId1)
	if err != nil {
		t.Fatalf("failed to get pending dump requests: %v", err)
	}
	if string(resp) != "[]" {
		t.Fatalf("There are pending dump requests! user_id=%#v, resp=%#v", data.UserId(secretKey), string(resp))
	}

	// Record two commands and then query for them
	out := tester.RunInteractiveShell(t, `echo hello
echo other`)
	if out != "hello\nother\n" {
		t.Fatalf("running echo had unexpected out=%#v", out)
	}

	// Query for it and check that the directory gets recorded correctly
	out = hishtoryQuery(t, tester, "echo")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	if !strings.Contains(out, "echo hello") {
		t.Fatalf("hishtory query doesn't contain expected command, out=%#v", out)
	}
	if !strings.Contains(out, "echo other") {
		t.Fatalf("hishtory query doesn't contain expected command, out=%#v", out)
	}

	// Back up this copy
	restoreFirstInstallation := shared.BackupAndRestoreWithId(t, "-install1")

	// Wipe the DB to simulate entries getting deleted because they've already been read and expired
	_, err = lib.ApiGet("/api/v1/wipe-db")
	if err != nil {
		t.Fatalf("failed to wipe the DB: %v", err)
	}

	// Install a new one (with the same secret key but a diff device id)
	installHishtory(t, tester, secretKey)

	// Confirm there is now a pending dump requests that the first device should respond to
	resp, err = lib.ApiGet("/api/v1/get-dump-requests?user_id=" + data.UserId(secretKey) + "&device_id=" + deviceId1)
	if err != nil {
		t.Fatalf("failed to get pending dump requests: %v", err)
	}
	if string(resp) == "[]" {
		t.Fatalf("There are no pending dump requests! user_id=%#v, resp=%#v", data.UserId(secretKey), string(resp))
	}

	// Check that the new one doesn't have the commands yet
	out = hishtoryQuery(t, tester, "echo")
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has unexpected number of lines, should contain no entries: out=%#v", out)
	}
	if strings.Contains(out, "echo hello") || strings.Contains("echo other", out) {
		t.Fatalf("hishtory query contains unexpected command, out=%#v", out)
	}
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	if out != "hishtory query echo\n" {
		t.Fatalf("hishtory export has unexpected out=%#v", out)
	}

	// Restore the first copy
	restoreSecondInstallation := shared.BackupAndRestoreWithId(t, "-install2")
	restoreFirstInstallation()

	// Confirm it still has the correct entries via hishtory export (and this runs a command to trigger it to dump the DB)
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput := "echo hello\necho other\nhishtory query echo\nhishtory query echo\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Confirm there are no pending dump requests for the first device
	resp, err = lib.ApiGet("/api/v1/get-dump-requests?user_id=" + data.UserId(secretKey) + "&device_id=" + deviceId1)
	if err != nil {
		t.Fatalf("failed to get pending dump requests: %v", err)
	}
	if string(resp) != "[]" {
		t.Fatalf("There are pending dump requests! user_id=%#v, resp=%#v", data.UserId(secretKey), string(resp))
	}

	// Restore the second copy and confirm it has the commands
	restoreSecondInstallation()
	out = hishtoryQuery(t, tester, "ech")
	if strings.Count(out, "\n") != 5 {
		t.Fatalf("hishtory query has unexpected number of lines=%d: out=%#v", strings.Count(out, "\n"), out)
	}
	expected := []string{"echo hello", "echo other"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}

	// And check hishtory export too for good measure
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = "echo hello\necho other\nhishtory query echo\nhishtory query echo\nhishtory query ech\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testInstallViaPythonScript(t *testing.T, tester shellTester) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Install via the python script
	out := tester.RunInteractiveShell(t, `curl https://hishtory.dev/install.py | python3 -`)
	if !strings.Contains(out, "Succesfully installed hishtory") {
		t.Fatalf("unexpected output when installing hishtory, out=%#v", out)
	}
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output: matches=%#v", matches)
	}
	userSecret := matches[1]

	// Test the status subcommand
	downloadData, err := lib.GetDownloadData()
	if err != nil {
		t.Fatal(err)
	}
	out = tester.RunInteractiveShell(t, `hishtory status`)
	expectedOut := fmt.Sprintf("Hishtory: %s\nEnabled: true\nSecret Key: %s\nCommit Hash: ", downloadData.Version, userSecret)
	if !strings.Contains(out, expectedOut) {
		t.Fatalf("status command has unexpected output: actual=%#v, expected=%#v", out, expectedOut)
	}

	// And test that it recorded that command
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	if out != "hishtory status\n" {
		t.Fatalf("unexpected output from hishtory export=%#v", out)
	}
}

func testExportWithQuery(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()
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
echo thisisrecorded`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "foo\nbar\nthisisnotrecorded\nthisisrecorded\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}

	// Test querying for a specific command
	out = hishtoryQuery(t, tester, "foo")
	expected = []string{"echo foo", "ls /foo"}
	unexpected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}
	for _, item := range unexpected {
		if strings.Contains(out, item) {
			t.Fatalf("output is containing unexpected item %#v: %#v", item, out)
		}
	}

	// Test using export with a query
	out = tester.RunInteractiveShell(t, `hishtory export foo`)
	if out != "ls /foo\necho foo\nhishtory query foo\n" {
		t.Fatalf("expected hishtory export to equal out=%#v", out)
	}

	// Test a more complex query with export
	out = tester.RunInteractiveShell(t, `hishtory export cwd:/tmp/`)
	if out != "hishtory enable\necho thisisrecorded\n" {
		t.Fatalf("expected hishtory export to equal out=%#v", out)
	}
}

func testHelpCommand(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Test the help command
	out := tester.RunInteractiveShell(t, `hishtory help`)
	if !strings.HasPrefix(out, "hishtory: Better shell history\n\nSupported commands:\n") {
		t.Fatalf("expected hishtory help to contain intro, actual=%#v", out)
	}
	out2 := tester.RunInteractiveShell(t, `hishtory -h`)
	if out != out2 {
		t.Fatalf("expected hishtory -h to equal help")
	}
}

func testStripBashTimePrefix(t *testing.T, tester shellTester) {
	if tester.ShellName() != "bash" {
		t.Skip()
	}

	// Setup
	defer shared.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Add a HISTTIMEFORMAT to the bashrc
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path.Join(homedir, ".hishtory", "config.sh"),
		os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString("\nexport HISTTIMEFORMAT='%F %T '\n")
	if err != nil {
		t.Fatal(err)
	}

	// Record a command
	tester.RunInteractiveShell(t, `ls -Slah`)

	// Check it shows up correctly
	out := tester.RunInteractiveShell(t, "hishtory export ls")
	if out != "ls -Slah\n" {
		t.Fatalf("hishtory had unexpected output=%#v", out)
	}

	// Update it to another complex one
	homedir, err = os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	f, err = os.OpenFile(path.Join(homedir, ".hishtory", "config.sh"),
		os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString("\nexport HISTTIMEFORMAT='[%c] '\n")
	if err != nil {
		t.Fatal(err)
	}

	// Record a command
	tester.RunInteractiveShell(t, `echo foo`)

	// Check it shows up correctly
	out = tester.RunInteractiveShell(t, "hishtory export echo")
	if out != "echo foo\n" {
		t.Fatalf("hishtory had unexpected output=%#v", out)
	}
}

func testReuploadHistoryEntries(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()

	// Init an initial device
	userSecret := installHishtory(t, tester, "")

	// Set up a second device
	restoreFirstProfile := shared.BackupAndRestoreWithId(t, "-install1")
	installHishtory(t, tester, userSecret)

	// Device 2: Record a command
	tester.RunInteractiveShell(t, `echo 1`)

	// Device 2: Record a command with a simulated network error
	tester.RunInteractiveShell(t, `echo 2; export HISHTORY_SIMULATE_NETWORK_ERROR=1; echo 3`)

	// Device 1: Run an export and confirm that the network only contains the first command
	restoreSecondProfile := shared.BackupAndRestoreWithId(t, "-install2")
	restoreFirstProfile()
	out := tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput := "echo 1\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 2: Run another command but with the network re-enabled
	restoreFirstProfile = shared.BackupAndRestoreWithId(t, "-install1")
	restoreSecondProfile()
	tester.RunInteractiveShell(t, `unset HISHTORY_SIMULATE_NETWORK_ERROR; echo 4`)

	// Device 2: Run export which contains all results (as it did all along since it is stored offline)
	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput = "echo 1\necho 2; export HISHTORY_SIMULATE_NETWORK_ERROR=1; echo 3\nunset HISHTORY_SIMULATE_NETWORK_ERROR; echo 4\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 1: Now it too sees all the results
	restoreFirstProfile()
	out = tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput = "echo 1\necho 2; export HISHTORY_SIMULATE_NETWORK_ERROR=1; echo 3\nunset HISHTORY_SIMULATE_NETWORK_ERROR; echo 4\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testInitialHistoryImport(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()

	// Record some commands before installing hishtory
	randomCmdUuid := uuid.Must(uuid.NewRandom()).String()
	randomCmd := fmt.Sprintf(`echo %v-foo
echo %v-bar`, randomCmdUuid, randomCmdUuid)
	tester.RunInteractiveShell(t, randomCmd)

	// Install hishtory
	installHishtory(t, tester, "")

	// Check that hishtory export doesn't have the commands yet
	out := tester.RunInteractiveShell(t, `hishtory export `+randomCmdUuid)
	expectedOutput := ""
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Trigger an import
	out = tester.RunInteractiveShell(t, "hishtory import")
	r := regexp.MustCompile(`Imported (.+) history entries from your existing shell history`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract history entries count from output: matches=%#v, out=%#v", matches, out)
	}
	num, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatal(err)
	}
	if num <= 2 {
		t.Fatalf("hishtory didn't import enough entries, only found %v entries", num)
	}

	// Check that the previously recorded commands are in hishtory
	// TODO: change the below to | grep -v pipefail and see that it fails weirdly with zsh
	out = tester.RunInteractiveShell(t, `hishtory export `+randomCmdUuid)
	expectedOutput = fmt.Sprintf("hishtory export %s\necho %s-foo\necho %s-bar\nhishtory export %s\n", randomCmdUuid, randomCmdUuid, randomCmdUuid, randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testLocalRedaction(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()

	// Install hishtory
	installHishtory(t, tester, "")

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

	// Redact foo
	out = tester.RunInteractiveShell(t, `hishtory redact --force foo`)
	if out != "Permanently deleting 2 entries" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

	// Check that the commands are redacted
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = fmt.Sprintf("echo %s-bas\nls /tmp\nhishtory redact --force foo\n", randomCmdUuid)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Redact s
	out = tester.RunInteractiveShell(t, `hishtory redact --force s`)
	if out != "Permanently deleting 10 entries" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

	// Check that the commands are redacted
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = "hishtory redact --force s\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func testRemoteRedaction(t *testing.T, tester shellTester) {
	// Setup
	defer shared.BackupAndRestore(t)()

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
	restoreInstall1 := shared.BackupAndRestoreWithId(t, "-1")
	installHishtory(t, tester, userSecret)

	// And confirm that it has the commands too
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Restore the first client, and redact some commands
	restoreInstall2 := shared.BackupAndRestoreWithId(t, "-2")
	restoreInstall1()
	out = tester.RunInteractiveShell(t, `hishtory redact --force `+randomCmdUuid)
	if out != "Permanently deleting 2 entries" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

	// Confirm that client1 doesn't have the commands
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail`)
	expectedOutput = fmt.Sprintf("echo foo\nls /tmp\nhishtory redact --force %s\n", randomCmdUuid)
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

// TODO: some tests for offline behavior
