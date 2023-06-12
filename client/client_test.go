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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"
)

func skipSlowTests() bool {
	return os.Getenv("FAST") != ""
}

func TestMain(m *testing.M) {
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()
	os.Setenv("HISHTORY_TEST", "1")
	defer testutils.BackupAndRestoreEnv("HISHTORY_SKIP_INIT_IMPORT")()
	os.Setenv("HISHTORY_SKIP_INIT_IMPORT", "1")
	defer testutils.RunTestServer()()
	cmd := exec.Command("go", "build", "-o", "/tmp/client")
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
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

type OnlineStatus int64

const (
	Online OnlineStatus = iota
	Offline
)

func TestP(t *testing.T) {
	if skipSlowTests() {
		shellTesters = shellTesters[:1]
	}
	for _, tester := range shellTesters {
		t.Run("testRepeatedCommandThenQuery/"+tester.ShellName(), func(t *testing.T) { testRepeatedCommandThenQuery(t, tester) })
		t.Run("testRepeatedCommandAndQuery/"+tester.ShellName(), func(t *testing.T) { testRepeatedCommandAndQuery(t, tester) })
		t.Run("testRepeatedEnableDisable/"+tester.ShellName(), func(t *testing.T) { testRepeatedEnableDisable(t, tester) })
		t.Run("testExcludeHiddenCommand/"+tester.ShellName(), func(t *testing.T) { testExcludeHiddenCommand(t, tester) })
		t.Run("testUpdate/"+tester.ShellName(), func(t *testing.T) { testUpdate(t, tester) })
		t.Run("testAdvancedQuery/"+tester.ShellName(), func(t *testing.T) { testAdvancedQuery(t, tester) })
		t.Run("testIntegration/"+tester.ShellName(), func(t *testing.T) { testIntegration(t, tester, Online) })
		t.Run("testIntegration/offline/"+tester.ShellName(), func(t *testing.T) { testIntegration(t, tester, Offline) })
		t.Run("testIntegrationWithNewDevice/"+tester.ShellName(), func(t *testing.T) { testIntegrationWithNewDevice(t, tester) })
		t.Run("testHishtoryBackgroundSaving/"+tester.ShellName(), func(t *testing.T) { testHishtoryBackgroundSaving(t, tester) })
		t.Run("testDisplayTable/"+tester.ShellName(), func(t *testing.T) { testDisplayTable(t, tester) })
		t.Run("testTableDisplayCwd/"+tester.ShellName(), func(t *testing.T) { testTableDisplayCwd(t, tester) })
		t.Run("testTimestampsAreReasonablyCorrect/"+tester.ShellName(), func(t *testing.T) { testTimestampsAreReasonablyCorrect(t, tester) })
		t.Run("testRequestAndReceiveDbDump/"+tester.ShellName(), func(t *testing.T) { testRequestAndReceiveDbDump(t, tester) })
		t.Run("testInstallViaPythonScript/"+tester.ShellName(), func(t *testing.T) { testInstallViaPythonScript(t, tester) })
		t.Run("testExportWithQuery/"+tester.ShellName(), func(t *testing.T) { testExportWithQuery(t, tester) })
		t.Run("testHelpCommand/"+tester.ShellName(), func(t *testing.T) { testHelpCommand(t, tester) })
		t.Run("testReuploadHistoryEntries/"+tester.ShellName(), func(t *testing.T) { testReuploadHistoryEntries(t, tester) })
		t.Run("testHishtoryOffline/"+tester.ShellName(), func(t *testing.T) { testHishtoryOffline(t, tester) })
		t.Run("testInitialHistoryImport/"+tester.ShellName(), func(t *testing.T) { testInitialHistoryImport(t, tester) })
		t.Run("testLocalRedaction/"+tester.ShellName(), func(t *testing.T) { testLocalRedaction(t, tester, Online) })
		t.Run("testLocalRedaction/offline/"+tester.ShellName(), func(t *testing.T) { testLocalRedaction(t, tester, Offline) })
		t.Run("testRemoteRedaction/"+tester.ShellName(), func(t *testing.T) { testRemoteRedaction(t, tester) })
		t.Run("testMultipleUsers/"+tester.ShellName(), func(t *testing.T) { testMultipleUsers(t, tester) })
		t.Run("testConfigGetSet/"+tester.ShellName(), func(t *testing.T) { testConfigGetSet(t, tester) })
		t.Run("testControlR/"+tester.ShellName(), func(t *testing.T) { testControlR(t, tester, tester.ShellName(), Online) })
		t.Run("testHandleUpgradedFeatures/"+tester.ShellName(), func(t *testing.T) { testHandleUpgradedFeatures(t, tester) })
		t.Run("testCustomColumns/"+tester.ShellName(), func(t *testing.T) { testCustomColumns(t, tester) })
		t.Run("testUninstall/"+tester.ShellName(), func(t *testing.T) { testUninstall(t, tester) })
	}
	t.Run("testControlR/offline/bash", func(t *testing.T) { testControlR(t, bashTester{}, "bash", Offline) })
	t.Run("testControlR/fish", func(t *testing.T) { testControlR(t, bashTester{}, "fish", Online) })

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

	// Clear all local state
	testutils.ResetLocalState(t)

	// Install it again
	installHishtory(t, tester, userSecret)

	// Querying should show the history from the previous run
	out := tester.RunInteractiveShell(t, `hishtory query`)
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
	testutils.ResetLocalState(t)

	// Install it a 3rd time
	installHishtory(t, tester, "adifferentsecret")

	// Run a command that shouldn't be in the hishtory later on
	tester.RunInteractiveShell(t, `echo notinthehistory`)
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "echo notinthehistory") {
		t.Fatalf("output is missing `echo notinthehistory`")
	}

	// Set the secret key to the previous secret key
	out, err := tester.RunInteractiveShellRelaxed(t, ` export HISHTORY_SKIP_INIT_IMPORT=1
yes | hishtory init `+userSecret)
	testutils.Check(t, err)
	if !strings.Contains(out, "Setting secret hishtory key to "+userSecret) {
		t.Fatalf("Failed to re-init with the user secret: %v", out)
	}

	// Querying shouldn't show the entry from the previous account
	out = hishtoryQuery(t, tester, "")
	if strings.Contains(out, "notinthehistory") {
		t.Fatalf("output contains the unexpected item: notinthehistory: \n%s", out)
	}

	// And it should show the history from the previous run on this account
	expected = []string{"echo thisisrecorded", "echo mynewcommand", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
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
	newEntry := testutils.MakeFakeHistoryEntry("othercomputer")
	newEntry.StartTime = time.Now()
	newEntry.EndTime = time.Now()
	manuallySubmitHistoryEntry(t, userSecret, newEntry)

	// Now check if that is in there when we do hishtory query
	out = hishtoryQuery(t, tester, "")
	if !strings.Contains(out, "othercomputer") {
		t.Fatalf("hishtory query doesn't contain cmd run on another machine! out=%#v", out)
	}

	// Run a reupload just to test that flow
	tester.RunInteractiveShell(t, "hishtory reupload")

	// Finally, test the export command
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail | grep -v '/tmp/client install'`)
	if strings.Contains(out, "thisisnotrecorded") {
		t.Fatalf("hishtory export contains a command that should not have been recorded, out=%#v", out)
	}
	expectedOutputWithoutKey := "hishtory status\nhishtory query\nls /a\nls /bar\nls /foo\necho foo\necho bar\nhishtory enable\necho thisisrecorded\nhishtory query\nhishtory query foo\necho hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\" # mycommand\nhishtory query complex\nhishtory query\necho mynewcommand\nhishtory query\nyes | hishtory init %s\nhishtory query\necho mynewercommand\nhishtory query\nothercomputer\nhishtory query\nhishtory reupload\n"
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

func installHishtory(t *testing.T, tester shellTester, userSecret string) string {
	out := tester.RunInteractiveShell(t, ` /tmp/client install `+userSecret)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output=%#v: matches=%#v", out, matches)
	}
	return matches[1]
}

func installWithOnlineStatus(t *testing.T, tester shellTester, onlineStatus OnlineStatus) string {
	if onlineStatus == Online {
		return installHishtory(t, tester, "")
	} else {
		return installHishtory(t, tester, "--offline")
	}
}

func assertOnlineStatus(t *testing.T, onlineStatus OnlineStatus) {
	config := hctx.GetConf(hctx.MakeContext())
	if onlineStatus == Online && config.IsOffline == true {
		t.Fatalf("We're supposed to be online, yet config.IsOffline=%#v (config=%#v)", config.IsOffline, config)
	}
	if onlineStatus == Offline && config.IsOffline == false {
		t.Fatalf("We're supposed to be offline, yet config.IsOffline=%#v (config=%#v)", config.IsOffline, config)
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
	if err != nil {
		t.Fatalf("failed to get homedir: %v", err)
	}
	dat, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"))
	if err != nil {
		t.Fatalf("failed to read config.sh: %v", err)
	}
	if strings.Contains(string(dat), "# Background Run") {
		t.Fatalf("config.sh is the prod version when it shouldn't be, config.sh=%#v", string(dat))
	}

	// Test the banner
	if onlineStatus == Online {
		os.Setenv("FORCED_BANNER", "HELLO_FROM_SERVER")
		defer os.Setenv("FORCED_BANNER", "")
		out = hishtoryQuery(t, tester, "")
		if !strings.Contains(out, "HELLO_FROM_SERVER\nHostname") {
			t.Fatalf("hishtory query didn't show the banner message! out=%#v", out)
		}
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
	datetimeMatcher := `[a-zA-Z]{3}\s\d{1,2}\s\d{4}\s[0-9:]+\s([A-Z]{3}|[+-]\d{4})`
	runtimeMatcher := `[0-9.ms]+`
	exitCodeMatcher := `0`
	pipefailMatcher := `set -em?o pipefail`
	line1Matcher := `Hostname` + tableDividerMatcher + `CWD` + tableDividerMatcher + `Timestamp` + tableDividerMatcher + `Runtime` + tableDividerMatcher + `Exit Code` + tableDividerMatcher + `Command\s*\n`
	line2Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + runtimeMatcher + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + pipefailMatcher + tableDividerMatcher + `\n`
	line3Matcher := hostnameMatcher + tableDividerMatcher + pathMatcher + tableDividerMatcher + datetimeMatcher + tableDividerMatcher + runtimeMatcher + tableDividerMatcher + exitCodeMatcher + tableDividerMatcher + `echo thisisrecorded` + tableDividerMatcher + `\n`
	match, err := regexp.MatchString(line3Matcher, out)
	testutils.Check(t, err)
	if !match {
		t.Fatalf("output is missing the row for `echo thisisrecorded`: %v", out)
	}
	match, err = regexp.MatchString(line1Matcher, out)
	testutils.Check(t, err)
	if !match {
		t.Fatalf("output is missing the headings: %v", out)
	}
	match, err = regexp.MatchString(line2Matcher, out)
	testutils.Check(t, err)
	if !match {
		t.Fatalf("output is missing the pipefail: %v", out)
	}
	match, err = regexp.MatchString(line1Matcher+line2Matcher+line3Matcher, out)
	testutils.Check(t, err)
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
	complexCommand := "echo hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\" # mycommand"
	_, _ = tester.RunInteractiveShellRelaxed(t, complexCommand)

	// Query for it
	out = hishtoryQuery(t, tester, "complex")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	if !strings.Contains(out, complexCommand) {
		t.Fatalf("hishtory query doesn't contain the expected complex command, out=%#v", out)
	}

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
	entry := testutils.MakeFakeHistoryEntry("cmd_with_diff_hostname_and_username")
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
	out = hishtoryQuery(t, tester, `-echo -pipefail`)
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
	entry = testutils.MakeFakeHistoryEntry("foo -echo")
	manuallySubmitHistoryEntry(t, userSecret, entry)
	out = hishtoryQuery(t, tester, `-echo -install -pipefail`)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 4 {
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

func testUpdate(t *testing.T, tester shellTester) {
	if !testutils.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		t.Skip("skipping on linux/arm64 which is unsupported")
	}
	if skipSlowTests() {
		t.Skip("skipping slow tests")
	}
	// Set up
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Record a command before the update
	tester.RunInteractiveShell(t, "echo hello")

	// Check the status command
	out := tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
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
	if strings.Contains(out, "skipping SLSA validation") {
		t.Fatalf("SLSA validation was skipped, out=%#v", out)
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
	out = tester.RunInteractiveShell(t, "hishtory export -pipefail | grep -v '/tmp/client install'")
	expectedOutput := "echo hello\nhishtory status\nhishtory update\nhishtory update\nhishtory status\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// TODO: write a test that updates from v.prev to latest rather than v.Unknown to latest
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
	if strings.Count(out, "echo mycommand") != 25 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}

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

func hishtoryQuery(t *testing.T, tester shellTester, query string) string {
	return tester.RunInteractiveShell(t, "hishtory query "+query)
}

func manuallySubmitHistoryEntry(t *testing.T, userSecret string, entry data.HistoryEntry) {
	encEntry, err := data.EncryptHistoryEntry(userSecret, entry)
	testutils.Check(t, err)
	if encEntry.Date != entry.EndTime {
		t.Fatalf("encEntry.Date does not match the entry")
	}
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	testutils.Check(t, err)
	resp, err := http.Post("http://localhost:8080/api/v1/submit", "application/json", bytes.NewBuffer(jsonValue))
	testutils.Check(t, err)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to submit result to backend, status_code=%d", resp.StatusCode)
	}
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
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has unexpected number of lines: out=%#v", out)
	}
	if !strings.Contains(out, "~/"+data.GetHishtoryPath()) {
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
	if runtime.GOOS == "darwin" {
		t.Skip("skip testing background saving since it is flakey on MacOs")
	}

	// Setup
	defer testutils.BackupAndRestore(t)()

	// Check that we can find the go binary
	_, err := exec.LookPath("go")
	testutils.Check(t, err)

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
	if err != nil {
		t.Fatalf("failed to get homedir: %v", err)
	}
	dat, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"))
	if err != nil {
		t.Fatalf("failed to read config.sh: %v", err)
	}
	if strings.Contains(string(dat), "except it doesn't run the save process in the background") {
		t.Fatalf("config.sh is the testing version when it shouldn't be, config.sh=%#v", dat)
	}

	// Test the status subcommand
	out = tester.RunInteractiveShell(t, `hishtory status`)
	if out != fmt.Sprintf("hiSHtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
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
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Submit two fake entries
	tmz, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}
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
	testutils.Check(t, os.Chdir("/"))
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
	config := hctx.GetConf(hctx.MakeContext())
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
	restoreFirstInstallation := testutils.BackupAndRestoreWithId(t, "-install1")

	// Wipe the DB to simulate entries getting deleted because they've already been read and expired
	_, err = lib.ApiGet("/api/v1/wipe-db-entries")
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
	restoreSecondInstallation := testutils.BackupAndRestoreWithId(t, "-install2")
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

func TestInstallViaPythonScriptWithCustomHishtoryPath(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("HISHTORY_PATH")()
	os.Setenv("HISHTORY_PATH", ".other-path")
	testInstallViaPythonScriptChild(t, bashTester{})
}

func testInstallViaPythonScript(t *testing.T, tester shellTester) {
	defer testutils.BackupAndRestore(t)()
	testInstallViaPythonScriptChild(t, tester)
}

func testInstallViaPythonScriptChild(t *testing.T, tester shellTester) {
	if !testutils.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}

	// Set up
	defer testutils.BackupAndRestoreEnv("HISHTORY_TEST")()

	// Install via the python script
	out := tester.RunInteractiveShell(t, `curl https://hishtory.dev/install.py | python3 -`)
	if !strings.Contains(out, "Succesfully installed hishtory") {
		t.Fatalf("unexpected output when installing hishtory, out=%#v", out)
	}
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output=%#v: matches=%#v", out, matches)
	}
	userSecret := matches[1]

	// Test the status subcommand
	downloadData, err := lib.GetDownloadData()
	if err != nil {
		t.Fatal(err)
	}
	out = tester.RunInteractiveShell(t, `hishtory status`)
	expectedOut := fmt.Sprintf("hiSHtory: %s\nEnabled: true\nSecret Key: %s\nCommit Hash: ", downloadData.Version, userSecret)
	if !strings.Contains(out, expectedOut) {
		t.Fatalf("status command has unexpected output: actual=%#v, expected=%#v", out, expectedOut)
	}

	// And test that it recorded that command
	time.Sleep(time.Second)
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	if out != "hishtory status\n" {
		t.Fatalf("unexpected output from hishtory export=%#v", out)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if out != "foo\nbar\nthisisnotrecorded\nthisisrecorded\nbar\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = hishtoryQuery(t, tester, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a", "echo bar &", "sleep 1"}
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
	defer testutils.BackupAndRestore(t)()
	tester := bashTester{}
	installHishtory(t, tester, "")

	// Add a HISTTIMEFORMAT to the bashrc
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"),
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
	f, err = os.OpenFile(path.Join(homedir, data.GetHishtoryPath(), "config.sh"),
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
	defer testutils.BackupAndRestore(t)()

	// Init an initial device
	userSecret := installHishtory(t, tester, "")

	// Set up a second device
	restoreFirstProfile := testutils.BackupAndRestoreWithId(t, "-install1")
	installHishtory(t, tester, userSecret)

	// Device 2: Record a command
	tester.RunInteractiveShell(t, `echo 1`)

	// Device 2: Record a command with a simulated network error
	tester.RunInteractiveShell(t, `echo 2; export HISHTORY_SIMULATE_NETWORK_ERROR=1; echo 3`)

	// Device 1: Run an export and confirm that the network only contains the first command
	restoreSecondProfile := testutils.BackupAndRestoreWithId(t, "-install2")
	restoreFirstProfile()
	out := tester.RunInteractiveShell(t, "hishtory export | grep -v pipefail")
	expectedOutput := "echo 1\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Device 2: Run another command but with the network re-enabled
	restoreFirstProfile = testutils.BackupAndRestoreWithId(t, "-install1")
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
	out := tester.RunInteractiveShell(t, `export HISHTORY_SIMULATE_NETWORK_ERROR=1; hishtory export | grep -v pipefail`)
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
	out = tester.RunInteractiveShell(t, `export HISHTORY_SIMULATE_NETWORK_ERROR=1; hishtory export | grep -v pipefail`)
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

// TODO: tests for hishtory import

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
	expectedOutput := strings.ReplaceAll(`echo UUID-foo
echo UUID-bar
echo UUID-fishcommand
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
	if out != "Permanently deleting 2 entries\n" {
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
	testutils.Check(t, err)
	if out != "This will permanently delete 1 entries, are you sure? [y/N]" {
		t.Fatalf("hishtory redact gave unexpected output=%#v", out)
	}

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
	if out != "Permanently deleting 2 entries\n" {
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
}

func clearControlRSearchFromConfig(t *testing.T) {
	configContents, err := hctx.GetConfigContents()
	testutils.Check(t, err)
	configContents = []byte(strings.ReplaceAll(string(configContents), "enable_control_r_search", "something-else"))
	homedir, err := os.UserHomeDir()
	testutils.Check(t, err)
	err = os.WriteFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH), configContents, 0o644)
	testutils.Check(t, err)
}

func testHandleUpgradedFeatures(t *testing.T, tester shellTester) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// Install, and there is no prompt since the config already mentions control-r
	_, err := tester.RunInteractiveShellRelaxed(t, `/tmp/client install`)
	testutils.Check(t, err)
	_, err = tester.RunInteractiveShellRelaxed(t, `hishtory disable`)
	testutils.Check(t, err)

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
	defer testutils.BackupAndRestore(t)()
	tester := bashTester{}
	installHishtory(t, tester, "")

	// Test recording in fish
	testutils.Check(t, os.Chdir("/"))
	out := captureTerminalOutputWithShellName(t, tester, "fish", []string{
		"echo SPACE foo ENTER",
		"ENTER",
		"SPACE echo SPACE baz ENTER",
		"echo SPACE bar ENTER",
		"echo SPACE '\"foo\"' ENTER",
		"SPACE echo SPACE foobar ENTER",
		"ls SPACE /tmp/ SPACE '&' ENTER",
	})
	if !strings.Contains(out, "Welcome to fish, the friendly interactive shell") || !strings.Contains(out, "foo") || !strings.Contains(out, "bar") || !strings.Contains(out, "baz") {
		t.Fatalf("fish output looks wrong")
	}

	// Check export
	out = tester.RunInteractiveShell(t, `hishtory export | grep -v pipefail | grep -v ps`)
	expectedOutput := "echo foo\necho bar\necho \"foo\"\nls /tmp/ &\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}

	// Check a table to see some other metadata
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns CWD Hostname 'Exit Code' Command`)
	out = hishtoryQuery(t, tester, "-pipefail")
	testutils.CompareGoldens(t, out, "TestFish-table")
}

// TODO(ddworken): Run TestTui in online and offline mode

func TestTui(t *testing.T) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	tester := zshTester{}
	installHishtory(t, tester, "")

	// Disable recording so that all our testing commands don't get recorded
	_, _ = tester.RunInteractiveShellRelaxed(t, ` hishtory disable`)

	// Insert a couple hishtory entries
	db := hctx.GetDb(hctx.MakeContext())
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("ls ~/")).Error)
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("echo 'aaaaaa bbbb'")).Error)

	// Check the initial output when there is no search
	out := captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery ENTER"})
	if len(strings.Split(out, "hishtory tquery")) != 2 {
		t.Fatalf("failed to split out=%#v", out)
	}
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-Initial")

	// Check the output when there is a search
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"ls",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-Search")

	// Check the output when there is a selected result
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"ls ENTER",
	})
	out = strings.Split(strings.TrimSpace(strings.Split(out, "hishtory tquery")[1]), "\n")[0]
	expected := `ls ~/`
	if diff := cmp.Diff(expected, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s", diff)
	}

	// Check the output when the initial search is invalid
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery SPACE foo: ENTER",
		"ls",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-InitialInvalidSearch")

	// Check the output when the search is invalid
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"ls:",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-InvalidSearch")

	// Check the output when the search is invalid and then edited to become valid
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"ls: BSpace",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-InvalidSearchBecomesValid")

	// Check the output when the size is smaller
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 20, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-SmallTerminal")

	// Check that we can use left arrow keys to scroll
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"s",
		"Left",
		"l",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-LeftScroll")

	// Check that we can exit the TUI via pressing esc
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"Escape",
	})
	if strings.Contains(out, "Search Query:") {
		t.Fatalf("unexpected out=\n%s", out)
	}
	if !testutils.IsGithubAction() {
		testutils.CompareGoldens(t, out, "TestTui-Exit")
	}

	// Check that it resizes after the terminal size is adjusted
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("echo 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'")).Error)
	out = captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 100, 20, []TmuxCommand{
		{Keys: "hishtory SPACE tquery ENTER"},
		{ResizeX: 300, ResizeY: 100},
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-Resize")

	// Check that we can delete an entry
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"aaaaaa",
		"C-K",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-Delete")

	// And that it stays deleted
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-DeleteStill")

	// And that we can then delete another entry
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-K",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-DeleteAgain")

	// And that it stays deleted
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-DeleteAgainStill")

	// Test horizontal scrolling by one to the right
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("echo '1234567890qwertyuiopasdfghjklzxxcvbnm0987654321_0_1234567890qwertyuiopasdfghjklzxxcvbnm0987654321_1_1234567890qwertyuiopasdfghjklzxxcvbnm0987654321_2_1234567890qwertyuiopasdfghjklzxxcvbnm0987654321'")).Error)
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"S-Left S-Right S-Right S-Left",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-RightScroll")

	// Test horizontal scrolling by two
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"S-Right S-Right",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-RightScrollTwo")

	// Test opening the help page
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-h",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-HelpPage")

	// Test closing the help page
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-h C-h",
	})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "TestTui-HelpPageClosed")

	// Test selecting and cd-ing
	out = captureTerminalOutput(t, tester, []string{
		"hishtory SPACE tquery ENTER",
		"C-x",
	})
	out = strings.Split(strings.TrimSpace(strings.Split(out, "hishtory tquery")[1]), "\n")[0]
	testutils.CompareGoldens(t, out, "TestTui-SelectAndCd")

	// Assert there are no leaked connections
	assertNoLeakedConnections(t)
}

func captureTerminalOutput(t *testing.T, tester shellTester, commands []string) string {
	return captureTerminalOutputWithShellName(t, tester, tester.ShellName(), commands)
}

type TmuxCommand struct {
	Keys    string
	ResizeX int
	ResizeY int
}

func captureTerminalOutputWithShellName(t *testing.T, tester shellTester, overriddenShellName string, commands []string) string {
	sCommands := make([]TmuxCommand, 0)
	for _, command := range commands {
		sCommands = append(sCommands, TmuxCommand{Keys: command})
	}
	return captureTerminalOutputWithShellNameAndDimensions(t, tester, overriddenShellName, 200, 50, sCommands)
}

func captureTerminalOutputWithShellNameAndDimensions(t *testing.T, tester shellTester, overriddenShellName string, width, height int, commands []TmuxCommand) string {
	sleepAmount := "0.1"
	if runtime.GOOS == "linux" {
		sleepAmount = "0.2"
	}
	if overriddenShellName == "fish" {
		// Fish is considerably slower so this is sadly necessary
		sleepAmount = "0.5"
	}
	if testutils.IsGithubAction() && runtime.GOOS == "darwin" {
		sleepAmount = "0.5"
	}
	fullCommand := ""
	fullCommand += " tmux kill-session -t foo || true\n"
	fullCommand += fmt.Sprintf(" tmux -u new-session -d -x %d -y %d -s foo %s\n", width, height, overriddenShellName)
	fullCommand += " sleep 1\n"
	if overriddenShellName == "bash" {
		fullCommand += " tmux send -t foo SPACE source SPACE ~/.bashrc ENTER\n"
	}
	fullCommand += " sleep " + sleepAmount + "\n"
	for _, cmd := range commands {
		if cmd.Keys != "" {
			fullCommand += " tmux send -t foo -- "
			fullCommand += cmd.Keys
			fullCommand += "\n"
		}
		if cmd.ResizeX != 0 && cmd.ResizeY != 0 {
			fullCommand += fmt.Sprintf(" tmux resize-window -t foo -x %d -y %d\n", cmd.ResizeX, cmd.ResizeY)
		}
		fullCommand += " sleep " + sleepAmount + "\n"
	}
	fullCommand += " sleep 0.5\n"
	fullCommand += " tmux capture-pane -t foo -p\n"
	fullCommand += " tmux kill-session -t foo\n"
	testutils.TestLog(t, "Running tmux command: "+fullCommand)
	return strings.TrimSpace(tester.RunInteractiveShell(t, fullCommand))
}

func testControlR(t *testing.T, tester shellTester, shellName string, onlineStatus OnlineStatus) {
	// Setup
	defer testutils.BackupAndRestore(t)()
	installWithOnlineStatus(t, tester, onlineStatus)
	assertOnlineStatus(t, onlineStatus)

	// Disable recording so that all our testing commands don't get recorded
	_, _ = tester.RunInteractiveShellRelaxed(t, ` hishtory disable`)
	_, _ = tester.RunInteractiveShellRelaxed(t, `hishtory config-set enable-control-r true`)

	// Insert a few hishtory entries
	db := hctx.GetDb(hctx.MakeContext())
	e1 := testutils.MakeFakeHistoryEntry("ls ~/")
	e1.CurrentWorkingDirectory = "/etc/"
	e1.Hostname = "server"
	e1.ExitCode = 127
	testutils.Check(t, db.Create(e1).Error)
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("ls ~/foo/")).Error)
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("ls ~/bar/")).Error)
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("echo 'aaaaaa bbbb'")).Error)
	testutils.Check(t, db.Create(testutils.MakeFakeHistoryEntry("echo 'bar' &")).Error)

	// Check that they're there
	var historyEntries []*data.HistoryEntry
	db.Model(&data.HistoryEntry{}).Find(&historyEntries)
	if len(historyEntries) != 5 {
		t.Fatalf("expected to find 5 history entries, actual found %d: %#v", len(historyEntries), historyEntries)
	}

	// And check that the control-r binding brings up the search
	out := captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R"})
	if !strings.Contains(out, "\n\n\n") {
		t.Fatalf("failed to find separator in %#v", out)
	}
	out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	testutils.CompareGoldens(t, out, "testControlR-Initial")

	// And check that we can scroll down and select an option
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Down Down", "Enter"})
	if !strings.HasSuffix(out, " ls ~/bar/") {
		t.Fatalf("hishtory tquery returned the wrong result, out=%#v", out)
	}

	// And that the above works, but also with an ENTER to actually execute the selected command
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Down", "Enter", "Enter"})
	if !strings.Contains(out, "echo 'aaaaaa bbbb'\naaaaaa bbbb\n") {
		t.Fatalf("hishtory tquery executed the wrong result, out=%#v", out)
	}

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
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "echo"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-Search")

	// An advanced search and check that the table is updated
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "cwd:/tmp/ SPACE ls"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-AdvancedSearch")

	// Set some different columns to be displayed and check that the table displays those
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns Hostname 'Exit Code' Command`)
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-displayedColumns")

	// Add a custom column
	tester.RunInteractiveShell(t, `hishtory config-add custom-columns foo "echo foo"`)
	tester.RunInteractiveShell(t, ` hishtory enable`)
	tester.RunInteractiveShell(t, `ls /`)
	tester.RunInteractiveShell(t, ` hishtory disable`)

	// And run a query and confirm it is displayed
	tester.RunInteractiveShell(t, `hishtory config-add displayed-columns foo`)
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "-pipefail"})
	out = strings.TrimSpace(out)
	if tester.ShellName() == "bash" {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-customColumn")

	// Start with a search query, and then press control-r and it shows results for that query
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"ls", "C-R"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-InitialSearch")

	// Start with a search query, and then press control-r, then make the query more specific
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"e", "C-R", "cho"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-InitialSearchExpanded")

	// Start with a search query for which there are no results
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"asdf", "C-R"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-InitialSearchNoResults")

	// Start with a search query for which there are no results
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"asdf", "C-R", "BSpace BSpace BSpace BSpace echo"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-InitialSearchNoResultsThenFoundResults")

	// Search, hit control-c, and the table should be cleared
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"echo", "C-R", "c", "C-C"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	if strings.Contains(out, "Search Query") || strings.Contains(out, "─────") || strings.Contains(out, "Exit Code") {
		t.Fatalf("hishtory is showing a table even after control-c? out=%#v", out)
	}
	if !testutils.IsGithubAction() {
		// This bit is broken on actions since actions run as a different user
		testutils.CompareGoldens(t, out, "testControlR-ControlC-"+shellName)
	}

	// Disable control-r
	_, _ = tester.RunInteractiveShellRelaxed(t, `hishtory config-set enable-control-r false`)
	// And it shouldn't pop up
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R"})
	if strings.Contains(out, "Search Query") || strings.Contains(out, "─────") || strings.Contains(out, "Exit Code") {
		t.Fatalf("hishtory overrode control-r even when this was disabled? out=%#v", out)
	}
	if !testutils.IsGithubAction() {
		// This bit is broken on actions since actions run as a different user
		testutils.CompareGoldens(t, out, "testControlR-"+shellName+"-Disabled")
	}

	// Re-enable control-r
	_, err := tester.RunInteractiveShellRelaxed(t, `hishtory config-set enable-control-r true`)
	testutils.Check(t, err)

	// And check that the control-r bindings work again
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "-pipefail SPACE -exit_code:0"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-Final")

	// Record a multi-line command
	tester.RunInteractiveShell(t, ` hishtory enable`)
	tester.RunInteractiveShell(t, `ls \
-Slah \
/`)
	tester.RunInteractiveShell(t, ` hishtory disable`)

	// Check that we display it in the table reasonably
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Slah"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	testutils.CompareGoldens(t, out, "testControlR-DisplayMultiline-"+shellName)

	// Check that we can select it correctly
	out = captureTerminalOutputWithShellName(t, tester, shellName, []string{"C-R", "Slah", "Enter"})
	if strings.Contains(out, "\n\n\n") {
		out = strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	if !strings.Contains(out, "-Slah") {
		t.Fatalf("out has unexpected output missing the selected row: \n%s", out)
	}
	if !testutils.IsGithubAction() {
		testutils.CompareGoldens(t, out, "testControlR-SelectMultiline-"+shellName)
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
	tester.RunInteractiveShell(t, `hishtory config-set displayed-columns 'Exit Code' git_remote Command `)
	out = tester.RunInteractiveShell(t, `hishtory query -pipefail`)
	testutils.CompareGoldens(t, out, fmt.Sprintf("testCustomColumns-query-isAction=%v", testutils.IsGithubAction()))
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail ENTER"})
	testName := "testCustomColumns-tquery-" + tester.ShellName()
	if testutils.IsGithubAction() {
		testName += "-isAction"
		testName += "-" + runtime.GOOS
	}
	testutils.CompareGoldens(t, out, testName)
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
	testutils.Check(t, err)
	testutils.CompareGoldens(t, out, "testUninstall-uninstall")

	// And check that hishtory has been uninstalled
	out, err = tester.RunInteractiveShellRelaxed(t, `echo foo
hishtory
echo bar`)
	testutils.Check(t, err)
	testutils.CompareGoldens(t, out, "testUninstall-post-uninstall")

	// And check again, but in a way that shows the full terminal output
	if !testutils.IsGithubAction() {
		out = captureTerminalOutput(t, tester, []string{
			"echo SPACE foo ENTER",
			"hishtory ENTER",
			"echo SPACE bar ENTER",
		})
		testutils.CompareGoldens(t, out, "testUninstall-post-uninstall-"+tester.ShellName())
	}
}

func TestTimestampFormat(t *testing.T) {
	// Setup
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	userSecret := installHishtory(t, tester, "")

	// Add an entry just to ensure we get consistent table sizing
	tester.RunInteractiveShell(t, "echo tablesizing")

	// Add some entries with fixed timestamps
	tmz, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}
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

	// Set a custom timestamp format
	tester.RunInteractiveShell(t, ` hishtory config-set timestamp-format '2006/Jan/2 15:04'`)

	// And check that it is displayed in both the tui and the classic view
	out := hishtoryQuery(t, tester, "-pipefail -tablesizing")
	testutils.CompareGoldens(t, out, "TestTimestampFormat-query")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail SPACE -tablesizing ENTER"})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	goldenName := "TestTimestampFormat-tquery"
	if testutils.IsGithubAction() {
		goldenName += "-isAction"
	}
	testutils.CompareGoldens(t, out, goldenName)
}

func TestZDotDir(t *testing.T) {
	// Setup
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	defer testutils.BackupAndRestoreEnv("ZDOTDIR")()
	homedir, err := os.UserHomeDir()
	testutils.Check(t, err)
	zdotdir := path.Join(homedir, "foo")
	testutils.Check(t, os.MkdirAll(zdotdir, 0o744))
	os.Setenv("ZDOTDIR", zdotdir)
	userSecret := installHishtory(t, tester, "")
	defer func() {
		testutils.Check(t, os.Remove(path.Join(zdotdir, ".zshrc")))
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
	testutils.Check(t, err)
	if !strings.Contains(string(zshrc), "# Hishtory Config:") {
		t.Fatalf("zshrc had unexpected contents=%#v", string(zshrc))
	}
}

func TestRemoveDuplicateRows(t *testing.T) {
	// Setup
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

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
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-tquery")

	// And change the config to filter out duplicate rows
	tester.RunInteractiveShell(t, `hishtory config-set filter-duplicate-commands true`)
	out = tester.RunInteractiveShell(t, `hishtory export -pipefail`)
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-export")
	out = tester.RunInteractiveShell(t, `hishtory query -pipefail`)
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-query")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail ENTER"})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-tquery")
	out = captureTerminalOutput(t, tester, []string{"hishtory SPACE tquery SPACE -pipefail ENTER", "Down Down", "ENTER"})
	out = strings.TrimSpace(strings.Split(out, "hishtory tquery")[1])
	out = strings.Split(out, "\n")[1]
	testutils.CompareGoldens(t, out, "testRemoveDuplicateRows-enabled-tquery-select")
}

func TestSetConfigNoCorruption(t *testing.T) {
	// Setup
	tester := zshTester{}
	defer testutils.BackupAndRestore(t)()
	installHishtory(t, tester, "")

	// A test that tries writing a config many different times in parallel, and confirms there is no corruption
	conf, err := hctx.GetConfig()
	testutils.Check(t, err)
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
			err := hctx.SetConfig(c)
			if err != nil {
				panic(err)
			}
			// Check that we can read
			c2, err := hctx.GetConfig()
			if err != nil {
				panic(err)
			}
			if c2.UserSecret != c.UserSecret {
				panic("user secret mismatch")
			}
			doneWg.Done()
		}(i)
	}
	doneWg.Wait()
}

type deviceSet struct {
	deviceMap     *map[device]deviceOp
	currentDevice *device
}

type device struct {
	key      string
	deviceId string
}

type deviceOp struct {
	backup  func()
	restore func()
}

func createDevice(t *testing.T, tester shellTester, devices *deviceSet, key, deviceId string) {
	d := device{key, deviceId}
	_, ok := (*devices.deviceMap)[d]
	if ok {
		t.Fatal(fmt.Errorf("cannot create device twice for key=%s deviceId=%s", key, deviceId))
	}
	installHishtory(t, tester, key)
	(*devices.deviceMap)[d] = deviceOp{
		backup:  func() { testutils.BackupAndRestoreWithId(t, key+deviceId) },
		restore: testutils.BackupAndRestoreWithId(t, key+deviceId),
	}
}

func switchToDevice(devices *deviceSet, d device) {
	if devices.currentDevice != nil && d == *devices.currentDevice {
		return
	}
	if devices.currentDevice != nil {
		(*devices.deviceMap)[*devices.currentDevice].backup()
	}
	devices.currentDevice = &d
	(*devices.deviceMap)[d].restore()
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
		testutils.Check(t, err)
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
		testutils.Check(t, err)
		expectedOutput := "echo u2d1\necho u2d2\necho u2d3\necho u1d1-b\necho u1d1-c\necho u2d3-b\necho u2d3-c\n"
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
		}
	}
}

type operation struct {
	device      device
	cmd         string
	redactQuery string
}

var tmp int = 0
var runCounter *int = &tmp

func fuzzTest(t *testing.T, tester shellTester, input string) {
	*runCounter += 1
	// Parse the input
	if len(input) > 1_000 {
		return
	}
	input = strings.TrimSpace(input)
	ops := make([]operation, 0)
	for _, line := range strings.Split(input, "\n") {
		split1 := strings.SplitN(line, "|", 2)
		if len(split1) != 2 {
			panic("malformed: split1")
		}
		split2 := strings.SplitN(split1[0], ";", 2)
		if len(split2) != 2 {
			panic("malformed: split2")
		}
		thingToDo := split1[1]
		cmd := ""
		redactQuery := ""
		if strings.HasPrefix(thingToDo, "!") {
			redactQuery = thingToDo[1:]
		} else {
			cmd = "echo " + thingToDo
		}
		re := regexp.MustCompile(`[a-zA-Z]+`)
		if !re.MatchString(cmd) && cmd != "" {
			panic("malformed: re")
		}
		key := split2[0]
		if strings.Contains(key, "-") {
			panic("malformed: key-")
		}
		op := operation{device: device{key: key + "-" + strconv.Itoa(*runCounter), deviceId: split2[1]}, cmd: cmd, redactQuery: redactQuery}
		ops = append(ops, op)
	}

	// Set up and create the devices
	defer testutils.BackupAndRestore(t)()
	var deviceMap map[device]deviceOp = make(map[device]deviceOp)
	var devices deviceSet = deviceSet{}
	devices.deviceMap = &deviceMap
	devices.currentDevice = nil
	for _, op := range ops {
		_, ok := (*devices.deviceMap)[op.device]
		if ok {
			continue
		}
		createDevice(t, tester, &devices, op.device.key, op.device.deviceId)
	}

	// Persist our basic in-memory copy of expected shell commands
	keyToCommands := make(map[string]string)

	// Run the commands
	for _, op := range ops {
		// Run the command
		switchToDevice(&devices, op.device)
		if op.cmd != "" {
			_, err := tester.RunInteractiveShellRelaxed(t, op.cmd)
			testutils.Check(t, err)
		}
		if op.redactQuery != "" {
			_, err := tester.RunInteractiveShellRelaxed(t, `HISHTORY_REDACT_FORCE=1 hishtory redact `+op.redactQuery)
			testutils.Check(t, err)
		}

		// Calculate the expected output of hishtory export
		val, ok := keyToCommands[op.device.key]
		if !ok {
			val = ""
		}
		if op.cmd != "" {
			val += op.cmd
			val += "\n"
		}
		if op.redactQuery != "" {
			lines := strings.Split(val, "\n")
			filteredLines := make([]string, 0)
			for _, line := range lines {
				if strings.Contains(line, op.redactQuery) {
					continue
				}
				filteredLines = append(filteredLines, line)
			}
			val = strings.Join(filteredLines, "\n")
			val += `HISHTORY_REDACT_FORCE=1 hishtory redact ` + op.redactQuery + "\n"
		}
		keyToCommands[op.device.key] = val

		// Run hishtory export and check the output
		out, err := tester.RunInteractiveShellRelaxed(t, `hishtory export -export -pipefail`)
		testutils.Check(t, err)
		expectedOutput := keyToCommands[op.device.key]
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch for input=%#v key=%s (-expected +got):\n%s\nout=%#v", input, op.device.key, diff, out)
		}
	}

	// Check that hishtory export has the expected results
	for _, op := range ops {
		switchToDevice(&devices, op.device)
		out, err := tester.RunInteractiveShellRelaxed(t, `hishtory export -export -pipefail`)
		testutils.Check(t, err)
		expectedOutput := keyToCommands[op.device.key]
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch for key=%s (-expected +got):\n%s\nout=%#v", op.device.key, diff, out)
		}
	}
}

func FuzzTestMultipleUsers(f *testing.F) {
	if skipSlowTests() {
		f.Skip("skipping slow tests")
	}
	defer testutils.RunTestServer()()
	// Format:
	//   $Op = $Key;$Device|$Command\n
	//         $Key;$Device|$Command\n$Op
	//   $Command = !$ThingToRedact
	//              $CommandToRun
	//
	// Running repeated commands
	f.Add("a;b|2\n")
	f.Add("a;b|aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	f.Add("a;b|aaaBBcccDD\n")
	f.Add("a;a|hello\na;a|world")
	f.Add("a;a|hello\na;a|world\na;b|3")
	f.Add("a;a|1\na;a|2\na;b|3\nb;a|4\na;b|5")
	f.Add("a;a|1\na;a|2\na;b|1\n")
	f.Add("a;a|1\na;a|2\na;b|1\nz;z|1\na;a|1\n")
	f.Add("a;a|hello\na;a|wobld")
	f.Add("a;a|hello\na;a|hello")
	f.Add("a;a|1\nb;a|2\nc;a|2\nd;a|2\na;b|2\na;b|3\na;b|4\na;b|8\na;d|2\nb;a|1")
	f.Add("a;a|1\na;b|1\na;c|1\na;d|1\na;e|1\na;f|1\na;g|1\na;b|1\na;b|1\na;b|1\na;b|1")
	f.Add("a;a|1\nb;b|1\na;c|1\na;d|1\na;e|1\na;f|1\na;g|1\na;b|1\na;b|1\na;b|1\na;b|1")
	f.Add("a;a|1\na;a|1\na;c|1\na;d|1\na;e|1\na;f|1\na;g|1\na;b|1\na;b|1\na;b|1\na;b|1")
	// Running repeated commands with redaction
	f.Add("a;b|!hello\n")
	f.Add("a;b|hello\na;b|world\na;b|!hello\n")
	f.Add("a;b|hello\na;b|world\na;a|hello2\na;b|!hello\na;b|hello3\na;b|hello4\n")
	f.Add("a;b|hello\na;b|world\na;a|hello2\na;b|!h\na;b|!h\na;b|hello3\na;b|hello4\n")
	f.Fuzz(func(t *testing.T, input string) {
		fuzzTest(t, bashTester{}, input)
		fuzzTest(t, zshTester{}, input)
	})
}

func assertNoLeakedConnections(t *testing.T) {
	resp, err := lib.ApiGet("/api/v1/get-num-connections")
	testutils.Check(t, err)
	numConnections, err := strconv.Atoi(string(resp))
	testutils.Check(t, err)
	if numConnections > 1 {
		t.Fatalf("DB has %d open connections, expected to have 1 or less", numConnections)
	}
}

// TODO: somehow test/confirm that hishtory works even if only bash/only zsh is installed
