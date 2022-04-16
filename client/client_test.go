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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/ddworken/hishtory/client/data"
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

func RunInteractiveBashCommands(t *testing.T, script string) string {
	out, err := RunInteractiveBashCommandsWithoutStrictMode(t, "set -emo pipefail\n"+script)
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		t.Fatalf("error when running command at %s:%d: %v", filename, line, err)
	}
	return out
}

func RunInteractiveBashCommandsWithoutStrictMode(t *testing.T, script string) (string, error) {
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

func TestIntegration(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Run the test
	testIntegration(t)
}

func TestIntegrationWithNewDevice(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Run the test
	userSecret := testIntegration(t)

	// Clear all local state
	shared.ResetLocalState(t)

	// Install it again
	installHishtory(t, userSecret)

	// Querying should show the history from the previous run
	out := hishtoryQuery(t, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
		if strings.Count(out, item) != 1 {
			t.Fatalf("output has %#v in it multiple times! out=%#v", item, out)
		}
	}

	RunInteractiveBashCommands(t, "echo mynewcommand")
	out = hishtoryQuery(t, "")
	if !strings.Contains(out, "echo mynewcommand") {
		t.Fatalf("output is missing `echo mynewcommand`")
	}
	if strings.Count(out, "echo mynewcommand") != 1 {
		t.Fatalf("output has `echo mynewcommand` the wrong number of times")
	}

	// Clear local state again
	shared.ResetLocalState(t)

	// Install it a 3rd time
	installHishtory(t, "adifferentsecret")

	// Run a command that shouldn't be in the hishtory later on
	RunInteractiveBashCommands(t, `echo notinthehistory`)
	out = hishtoryQuery(t, "")
	if !strings.Contains(out, "echo notinthehistory") {
		t.Fatalf("output is missing `echo notinthehistory`")
	}

	// Set the secret key to the previous secret key
	out = RunInteractiveBashCommands(t, `hishtory init `+userSecret)
	if !strings.Contains(out, "Setting secret hishtory key to "+userSecret) {
		t.Fatalf("Failed to re-init with the user secret: %v", out)
	}

	// Querying should show the history from the previous run
	out = hishtoryQuery(t, "")
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

	RunInteractiveBashCommands(t, "echo mynewercommand")
	out = hishtoryQuery(t, "")
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
	out = hishtoryQuery(t, "")
	if !strings.Contains(out, "othercomputer") {
		t.Fatalf("hishtory query doesn't contain cmd run on another machine! out=%#v", out)
	}

	// Finally, test the export command
	out = RunInteractiveBashCommands(t, `hishtory export`)
	if strings.Contains(out, "thisisnotrecorded") {
		t.Fatalf("hishtory export contains a command that should not have been recorded, out=%#v", out)
	}
	expectedOutputWithoutKey := "set -emo pipefail\nhishtory status\nset -emo pipefail\nhishtory query\nset -m\nls /a\nls /bar\nls /foo\necho foo\necho bar\nhishtory enable\necho thisisrecorded\nset -emo pipefail\nhishtory query\nset -emo pipefail\nhishtory query foo\necho hello | grep complex | sed s/h/i/g; echo baz && echo \"fo 'o\"\nset -emo pipefail\nhishtory query complex\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mynewcommand\nset -emo pipefail\nhishtory query\nhishtory init %s\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mynewercommand\nset -emo pipefail\nhishtory query\nothercomputer\nset -emo pipefail\nhishtory query\nset -emo pipefail\n"
	expectedOutput := fmt.Sprintf(expectedOutputWithoutKey, userSecret)
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func installHishtory(t *testing.T, userSecret string) string {
	out := RunInteractiveBashCommands(t, `/tmp/client install `+userSecret)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output: matches=%#v", matches)
	}
	return matches[1]
}

func testIntegration(t *testing.T) string {
	// Test install
	userSecret := installHishtory(t, "")

	// Test the status subcommand
	out := RunInteractiveBashCommands(t, `hishtory status`)
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
	out = hishtoryQuery(t, "")
	if !strings.Contains(out, "HELLO_FROM_SERVER\nHostname") {
		t.Fatalf("hishtory query didn't show the banner message! out=%#v", out)
	}
	os.Setenv("FORCED_BANNER", "")

	// Test recording commands
	out, err = RunInteractiveBashCommandsWithoutStrictMode(t, `set -m
ls /a
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
	out = hishtoryQuery(t, "")
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}
	// match, err = regexp.MatchString(`.*~/.*\s+[a-zA-Z]{3} \d+ 2022 \d\d:\d\d:\d\d PST\s+\d{1,2}ms\s+0\s+echo thisisrecorded.*`, out)
	// shared.Check(t, err)
	// if !match {
	// 	t.Fatalf("output is missing the row for `echo thisisrecorded`: %v", out)
	// }

	// Test querying for a specific command
	out = hishtoryQuery(t, "foo")
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
	_, _ = RunInteractiveBashCommandsWithoutStrictMode(t, complexCommand)

	// Query for it
	out = hishtoryQuery(t, "complex")
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	if !strings.Contains(out, complexCommand) {
		t.Fatalf("hishtory query doesn't contain the expected complex command, out=%#v", out)
	}

	return userSecret
}

func TestAdvancedQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()

	// Install hishtory
	userSecret := installHishtory(t, "")

	// Run some commands we can query for
	_, err := RunInteractiveBashCommandsWithoutStrictMode(t, `set -m 
echo nevershouldappear
notacommand
cd /tmp/
echo querybydir
hishtory disable`)
	if err != nil {
		t.Fatal(err)
	}

	// A super basic query just to ensure the basics are working
	out := hishtoryQuery(t, `echo`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching echo querybydir, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd
	out = hishtoryQuery(t, `cwd:/tmp`)
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
	out = hishtoryQuery(t, `cwd:tmp`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching cwd:tmp, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd and another term
	out = hishtoryQuery(t, `cwd:/tmp querybydir`)
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
	out = hishtoryQuery(t, `exit_code:127`)
	if !strings.Contains(out, "notacommand") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on exit_code and something else that matches nothing
	out = hishtoryQuery(t, `exit_code:127 foo`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on before: and cwd:
	out = hishtoryQuery(t, `before:2025-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = hishtoryQuery(t, `before:2025-07-02 cwd:tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = hishtoryQuery(t, `before:2025-07-02 cwd:mp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: and cwd:
	out = hishtoryQuery(t, `after:2020-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: that returns no results
	out = hishtoryQuery(t, `after:2120-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Manually submit an entry with a different hostname and username so we can test those atoms
	entry := data.MakeFakeHistoryEntry("cmd_with_diff_hostname_and_username")
	entry.LocalUsername = "otheruser"
	entry.Hostname = "otherhostname"
	manuallySubmitHistoryEntry(t, userSecret, entry)

	// Query based on the username that exists
	out = hishtoryQuery(t, `user:otheruser`)
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on the username that doesn't exist
	out = hishtoryQuery(t, `user:noexist`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on the hostname
	out = hishtoryQuery(t, `hostname:otherhostname`)
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item
	out = hishtoryQuery(t, "")
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	out = hishtoryQuery(t, `-cmd_with_diff_hostname_and_username`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = hishtoryQuery(t, `-echo`)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 5 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out with an atom
	out = hishtoryQuery(t, `-hostname:otherhostname`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = hishtoryQuery(t, `-user:otheruser`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = hishtoryQuery(t, `-exit_code:0`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item that also looks like it could be a search for a flag
	entry = data.MakeFakeHistoryEntry("foo -echo")
	manuallySubmitHistoryEntry(t, userSecret, entry)
	out = hishtoryQuery(t, `-echo -install`)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 5 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
}

func TestUpdate(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, "")

	// Check the status command
	out := RunInteractiveBashCommands(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Update
	RunInteractiveBashCommands(t, `hishtory update`)

	// Then check the status command again to confirm the update worked
	out = RunInteractiveBashCommands(t, `hishtory status`)
	if !strings.Contains(out, fmt.Sprintf("\nEnabled: true\nSecret Key: %s\nCommit Hash: ", userSecret)) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}
	if strings.Contains(out, "\nCommit Hash: Unknown\n") {
		t.Fatalf("status command has unexpected output: %#v", out)
	}
}

func TestRepeatedCommandThenQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, "")

	// Check the status command
	out := RunInteractiveBashCommands(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Run a command many times
	for i := 0; i < 25; i++ {
		RunInteractiveBashCommands(t, fmt.Sprintf("echo mycommand-%d", i))
	}

	// Check that it shows up correctly
	out = hishtoryQuery(t, `mycommand`)
	if strings.Count(out, "\n") != 26 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	if strings.Count(out, "echo mycommand") != 25 {
		t.Fatalf("hishtory query has the wrong number of commands=%d, out=%#v", strings.Count(out, "echo mycommand"), out)
	}

	RunInteractiveBashCommands(t, `echo mycommand-30
echo mycommand-31
echo mycommand-3`)

	out = RunInteractiveBashCommands(t, "hishtory export")
	expectedOutput := "set -emo pipefail\nhishtory status\nset -emo pipefail\necho mycommand-0\nset -emo pipefail\necho mycommand-1\nset -emo pipefail\necho mycommand-2\nset -emo pipefail\necho mycommand-3\nset -emo pipefail\necho mycommand-4\nset -emo pipefail\necho mycommand-5\nset -emo pipefail\necho mycommand-6\nset -emo pipefail\necho mycommand-7\nset -emo pipefail\necho mycommand-8\nset -emo pipefail\necho mycommand-9\nset -emo pipefail\necho mycommand-10\nset -emo pipefail\necho mycommand-11\nset -emo pipefail\necho mycommand-12\nset -emo pipefail\necho mycommand-13\nset -emo pipefail\necho mycommand-14\nset -emo pipefail\necho mycommand-15\nset -emo pipefail\necho mycommand-16\nset -emo pipefail\necho mycommand-17\nset -emo pipefail\necho mycommand-18\nset -emo pipefail\necho mycommand-19\nset -emo pipefail\necho mycommand-20\nset -emo pipefail\necho mycommand-21\nset -emo pipefail\necho mycommand-22\nset -emo pipefail\necho mycommand-23\nset -emo pipefail\necho mycommand-24\nset -emo pipefail\nhishtory query mycommand\nset -emo pipefail\necho mycommand-30\necho mycommand-31\necho mycommand-3\nset -emo pipefail\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func TestRepeatedCommandAndQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	userSecret := installHishtory(t, "")

	// Check the status command
	out := RunInteractiveBashCommands(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Run a command many times
	for i := 0; i < 25; i++ {
		RunInteractiveBashCommands(t, fmt.Sprintf("echo mycommand-%d", i))
		out = hishtoryQuery(t, fmt.Sprintf("mycommand-%d", i))
		if strings.Count(out, "\n") != 2 {
			t.Fatalf("hishtory query #%d has the wrong number of lines=%d, out=%#v", i, strings.Count(out, "\n"), out)
		}
		if strings.Count(out, "echo mycommand") != 1 {
			t.Fatalf("hishtory query #%d has the wrong number of commands=%d, out=%#v", i, strings.Count(out, "echo mycommand"), out)
		}

	}
}

func TestRepeatedEnableDisable(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	installHishtory(t, "")

	// Run a command many times
	for i := 0; i < 25; i++ {
		RunInteractiveBashCommands(t, fmt.Sprintf(`echo mycommand-%d
hishtory disable
echo shouldnotshowup
sleep 0.5
hishtory enable`, i))
		out := hishtoryQuery(t, fmt.Sprintf("mycommand-%d", i))
		if strings.Count(out, "\n") != 2 {
			t.Fatalf("hishtory query #%d has the wrong number of lines=%d, out=%#v", i, strings.Count(out, "\n"), out)
		}
		if strings.Count(out, "echo mycommand") != 1 {
			t.Fatalf("hishtory query #%d has the wrong number of commands=%d, out=%#v", i, strings.Count(out, "echo mycommand"), out)
		}
		out = hishtoryQuery(t, "")
		if strings.Contains(out, "shouldnotshowup") {
			t.Fatalf("hishtory query contains a result that should not have been recorded, out=%#v", out)
		}
	}

	out := RunInteractiveBashCommands(t, "hishtory export")
	expectedOutput := "set -emo pipefail\necho mycommand-0\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-0\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-1\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-1\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-2\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-2\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-3\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-3\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-4\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-4\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-5\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-5\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-6\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-6\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-7\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-7\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-8\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-8\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-9\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-9\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-10\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-10\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-11\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-11\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-12\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-12\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-13\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-13\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-14\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-14\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-15\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-15\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-16\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-16\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-17\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-17\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-18\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-18\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-19\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-19\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-20\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-20\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-21\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-21\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-22\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-22\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-23\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-23\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mycommand-24\nhishtory enable\nset -emo pipefail\nhishtory query mycommand-24\nset -emo pipefail\nhishtory query\nset -emo pipefail\n"
	if diff := cmp.Diff(expectedOutput, out); diff != "" {
		t.Fatalf("hishtory export mismatch (-expected +got):\n%s\nout=%#v", diff, out)
	}
}

func TestExcludeHiddenCommand(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	installHishtory(t, "")

	RunInteractiveBashCommands(t, `echo hello1
 echo hidden
echo hello2
 echo hidden`)
	RunInteractiveBashCommands(t, " echo hidden")
	out := hishtoryQuery(t, "")
	if strings.Count(out, "\n") != 5 && strings.Count(out, "\n") != 6 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
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

	out = RunInteractiveBashCommands(t, "hishtory export | grep -v pipefail")
	expectedOutput := "echo hello1\necho hello2\nhishtory query\n"
	if out != expectedOutput {
		t.Fatalf("hishtory export has unexpected output=%#v", out)
	}
}

func waitForBackgroundSavesToComplete(t *testing.T) {
	for i := 0; i < 20; i++ {
		cmd := exec.Command("pidof", "hishtory")
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil && err.Error() != "exit status 1" {
			t.Fatalf("failed to check if hishtory was running: %v, stdout=%#v, stderr=%#v", err, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "\n") {
			// pidof had no output, so hishtory isn't running and we're done waitng
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("failed to wait until hishtory wasn't running")
}

func hishtoryQuery(t *testing.T, query string) string {
	return RunInteractiveBashCommands(t, "hishtory query "+query)
}

// TODO: Maybe a dedicated unit test for retrieveAdditionalEntriesFromRemote

func manuallySubmitHistoryEntry(t *testing.T, userSecret string, entry data.HistoryEntry) {
	encEntry, err := data.EncryptHistoryEntry(userSecret, entry)
	shared.Check(t, err)
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	shared.Check(t, err)
	resp, err := http.Post("http://localhost:8080/api/v1/esubmit", "application/json", bytes.NewBuffer(jsonValue))
	shared.Check(t, err)
	if resp.StatusCode != 200 {
		t.Fatalf("failed to submit result to backend, status_code=%d", resp.StatusCode)
	}
}

func TestHishtoryBackgroundSaving(t *testing.T) {
	// Setup
	defer shared.BackupAndRestore(t)()

	// Test install with an unset HISHTORY_TEST var so that we save in the background (this is likely to be flakey!)
	out := RunInteractiveBashCommands(t, `unset HISHTORY_TEST
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
	out = RunInteractiveBashCommands(t, `hishtory status`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Test recording commands
	out, err = RunInteractiveBashCommandsWithoutStrictMode(t, `set -m
ls /a
echo foo`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "foo\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	waitForBackgroundSavesToComplete(t)
	out = hishtoryQuery(t, "")
	expected := []string{"echo foo", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}

	// Test querying for a specific command
	waitForBackgroundSavesToComplete(t)
	out = hishtoryQuery(t, "foo")
	if !strings.Contains(out, "echo foo") {
		t.Fatalf("output doesn't contain the expected item, out=%#v", out)
	}
	if strings.Contains(out, "ls /a") {
		t.Fatalf("output contains unexpected item, out=%#v", out)
	}
}
