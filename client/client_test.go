package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/shared"
)

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
	if strings.Contains(outStr, "hishtory fatal error:") {
		t.Fatalf("Ran command, but hishtory had a fatal error! out=%#v", outStr)
	}
	return outStr, nil
}

func TestIntegration(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "" {
		// TODO: debug why these tests fail on github actions, the error message is:
		// `bash: cannot set terminal process group (683): Inappropriate ioctl for device\nbash: no job control in this shell`
		t.Skip()
	}

	// Set up
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()

	// Run the test
	testIntegration(t)
}

func TestIntegrationWithNewDevice(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "" {
		// TODO: debug why these tests fail on github actions, the error message is:
		// `bash: cannot set terminal process group (683): Inappropriate ioctl for device\nbash: no job control in this shell`
		t.Skip()
	}

	// Set up
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()

	// Run the test
	userSecret := testIntegration(t)

	// Clear all local state
	shared.ResetLocalState(t)

	// Install it again
	installHishtory(t, userSecret)

	// Querying should show the history from the previous run
	out := RunInteractiveBashCommands(t, "hishtory query")
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
	out = RunInteractiveBashCommands(t, "hishtory query")
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
	out = RunInteractiveBashCommands(t, "hishtory query")
	if !strings.Contains(out, "echo notinthehistory") {
		t.Fatalf("output is missing `echo notinthehistory`")
	}

	// Set the secret key to the previous secret key
	out = RunInteractiveBashCommands(t, `hishtory init `+userSecret)
	if !strings.Contains(out, "Setting secret hishtory key to "+userSecret) {
		t.Fatalf("Failed to re-init with the user secret: %v", out)
	}

	// Querying should show the history from the previous run
	out = RunInteractiveBashCommands(t, "hishtory query")
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
	out = RunInteractiveBashCommands(t, "hishtory query")
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
	out = RunInteractiveBashCommands(t, `hishtory query`)
	if !strings.Contains(out, "othercomputer") {
		t.Fatalf("hishtory query doesn't contain cmd run on another machine! out=%#v", out)
	}

	// Finally, test the export command
	out = RunInteractiveBashCommands(t, `hishtory export`)
	if out != fmt.Sprintf(
		"/tmp/client install\nset -emo pipefail\nset -emo pipefail\nhishtory status\nset -emo pipefail\nhishtory query\nhishtory query\nset -m\nls /a\nls /bar\nls /foo\necho foo\necho bar\nhishtory enable\necho thisisrecorded\nset -emo pipefail\nhishtory query\nset -emo pipefail\nhishtory query foo\n/tmp/client install %s\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mynewcommand\nset -emo pipefail\nhishtory query\nhishtory init %s\nset -emo pipefail\nhishtory query\nset -emo pipefail\necho mynewercommand\nset -emo pipefail\nhishtory query\nothercomputer\nset -emo pipefail\nhishtory query\nset -emo pipefail\n", userSecret, userSecret) {
		t.Fatalf("hishtory export had unexpected output! out=%#v", out)
	}
}

func installHishtory(t *testing.T, userSecret string) string {
	out := RunInteractiveBashCommands(t, `
	go build -o /tmp/client
	/tmp/client install `+userSecret)
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
	out := RunInteractiveBashCommands(t, `
		hishtory status
	`)
	if out != fmt.Sprintf("Hishtory: v0.Unknown\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
		t.Fatalf("status command has unexpected output: %#v", out)
	}

	// Test the banner
	os.Setenv("FORCED_BANNER", "HELLO_FROM_SERVER")
	out = RunInteractiveBashCommands(t, `hishtory query`)
	if !strings.Contains(out, "HELLO_FROM_SERVER") {
		t.Fatalf("hishtory query didn't show the banner message! out=%#v", out)
	}
	os.Setenv("FORCED_BANNER", "")

	// Test recording commands
	out, err := RunInteractiveBashCommandsWithoutStrictMode(t, `
		set -m
		ls /a
		ls /bar
		ls /foo
		echo foo
		echo bar
		hishtory disable
		echo thisisnotrecorded
		hishtory enable
		echo thisisrecorded
		`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "foo\nbar\nthisisnotrecorded\nthisisrecorded\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = RunInteractiveBashCommands(t, "hishtory query")
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
	out = RunInteractiveBashCommands(t, "hishtory query foo")
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

	return userSecret
}

func TestAdvancedQuery(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()

	// Install hishtory
	userSecret := installHishtory(t, "")

	// Run some commands we can query for
	_, err := RunInteractiveBashCommandsWithoutStrictMode(t, `
	set -m 
	echo nevershouldappear
	notacommand
	cd /tmp/
	echo querybydir
	hishtory disable
	`)
	if err != nil {
		t.Fatal(err)
	}

	// A super basic query just to ensure the basics are working
	out := RunInteractiveBashCommands(t, `hishtory query echo`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching echo, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd
	out = RunInteractiveBashCommands(t, `hishtory query cwd:/tmp`)
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
	out = RunInteractiveBashCommands(t, `hishtory query cwd:tmp`)
	if !strings.Contains(out, "echo querybydir") {
		t.Fatalf("hishtory query doesn't contain result matching cwd:tmp, out=%#v", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on cwd and another term
	out = RunInteractiveBashCommands(t, `hishtory query cwd:/tmp querybydir`)
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
	out = RunInteractiveBashCommands(t, `hishtory query exit_code:127`)
	if !strings.Contains(out, "notacommand") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on exit_code and something else that matches nothing
	out = RunInteractiveBashCommands(t, `hishtory query exit_code:127 foo`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on before: and cwd:
	out = RunInteractiveBashCommands(t, `hishtory query before:2025-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = RunInteractiveBashCommands(t, `hishtory query before:2025-07-02 cwd:tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
	out = RunInteractiveBashCommands(t, `hishtory query before:2025-07-02 cwd:mp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: and cwd:
	out = RunInteractiveBashCommands(t, `hishtory query after:2020-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on after: that returns no results
	out = RunInteractiveBashCommands(t, `hishtory query after:2120-07-02 cwd:/tmp`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Manually submit an entry with a different hostname and username so we can test those atoms
	entry := data.MakeFakeHistoryEntry("cmd_with_diff_hostname_and_username")
	entry.LocalUsername = "otheruser"
	entry.Hostname = "otherhostname"
	manuallySubmitHistoryEntry(t, userSecret, entry)

	// Query based on the username that exists
	out = RunInteractiveBashCommands(t, `hishtory query user:otheruser`)
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on the username that doesn't exist
	out = RunInteractiveBashCommands(t, `hishtory query user:noexist`)
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Query based on the hostname
	out = RunInteractiveBashCommands(t, `hishtory query hostname:otherhostname`)
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item
	out = RunInteractiveBashCommands(t, `hishtory query`)
	if !strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query doesn't contain expected result, out=%#v", out)
	}
	out = RunInteractiveBashCommands(t, `hishtory query -cmd_with_diff_hostname_and_username`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = RunInteractiveBashCommands(t, `hishtory query -echo `)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if os.Getenv("GITHUB_ACTIONS") == "" {
		// For some reason, this fails on github actions and it only has 6 lines (it is missing the install line)
		if strings.Count(out, "\n") != 7 {
			t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
		}
	}

	// Test filtering out with an atom
	out = RunInteractiveBashCommands(t, `hishtory query -hostname:otherhostname`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = RunInteractiveBashCommands(t, `hishtory query -user:otheruser`)
	if strings.Contains(out, "cmd_with_diff_hostname_and_username") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	out = RunInteractiveBashCommands(t, `hishtory query -exit_code:0`)
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}

	// Test filtering out a search item that also looks like it could be a search for a flag
	entry = data.MakeFakeHistoryEntry("foo -echo")
	manuallySubmitHistoryEntry(t, userSecret, entry)
	out = RunInteractiveBashCommands(t, `hishtory query -echo`)
	if strings.Contains(out, "echo") {
		t.Fatalf("hishtory query contains unexpected result, out=%#v", out)
	}
	if strings.Count(out, "\n") != 7 {
		t.Fatalf("hishtory query has the wrong number of lines=%d, out=%#v", strings.Count(out, "\n"), out)
	}
}

func TestUpdate(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "" {
		// TODO: debug why these tests fail on github actions, the error message is:
		// `bash: cannot set terminal process group (683): Inappropriate ioctl for device\nbash: no job control in this shell`
		t.Skip()
	}

	// Set up
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()
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
