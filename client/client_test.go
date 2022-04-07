package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"encoding/json"
	"net/http"

	"github.com/ddworken/hishtory/shared"
)

func RunInteractiveBashCommands(t *testing.T, script string) string {
	shared.Check(t, ioutil.WriteFile("/tmp/hishtory-test-in.sh", []byte("set -euo pipefail\n"+script), 0600))
	cmd := exec.Command("bash", "-i")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout = &out
	var err bytes.Buffer
	cmd.Stderr = &err
	shared.CheckWithInfo(t, cmd.Run(), out.String()+err.String())
	outStr := out.String()
	if strings.Contains(outStr, "hishtory fatal error:") {
		t.Fatalf("Ran command, but hishtory had a fatal error! out=%#v", outStr)
	}
	return outStr 
}

func TestIntegration(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()

	// Run the test
	testIntegration(t)
}

func TestIntegrationWithNewDevice(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer(t)()

	// Run the test
	userSecret := testIntegration(t)

	// Clear all local state
	shared.ResetLocalState(t)

	// Install it again
	out := RunInteractiveBashCommands(t, `
	gvm use go1.17
	cd /home/david/code/hishtory/
	go build -o /tmp/client client/client.go
	/tmp/client install `+userSecret)
	match, err := regexp.MatchString(`Setting secret hishtory key to .*`, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("unexpected output from install: %v", out)
	}

	// Querying should show the history from the previous run
	out = RunInteractiveBashCommands(t, "hishtory query")
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
	out = RunInteractiveBashCommands(t, `
	gvm use go1.17
	cd /home/david/code/hishtory/
	go build -o /tmp/client client/client.go
	/tmp/client install`)
	match, err = regexp.MatchString(`Setting secret hishtory key to .*`, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("unexpected output from install: %v", out)
	}

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
	newEntry := shared.MakeFakeHistoryEntry("othercomputer")
	encEntry, err := shared.EncryptHistoryEntry(userSecret, newEntry)
	shared.Check(t, err)
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	shared.Check(t, err)
	resp, err := http.Post("http://localhost:8080/api/v1/esubmit", "application/json", bytes.NewBuffer(jsonValue))
	shared.Check(t, err)
	if resp.StatusCode != 200 {
		t.Fatalf("failed to submit result to backend, status_code=%d", resp.StatusCode)
	}
	// Now check if that is in there when we do hishtory query 
	out = RunInteractiveBashCommands(t, `hishtory query`)
	if !strings.Contains(out, "othercomputer") {
		t.Fatalf("hishtory query doesn't contain cmd run on another machine! out=%#v", out)
	}

	// Finally, test the export command
	out = RunInteractiveBashCommands(t, `hishtory export`)
	if out != fmt.Sprintf("/tmp/client install\n/tmp/client install\nhishtory status\nhishtory query\nhishtory query\nls /a\nls /bar\nls /foo\necho foo\necho bar\nhishtory enable\necho thisisrecorded\nhishtory query\nhishtory query foo\n/tmp/client install %s\nhishtory query\necho mynewcommand\nhishtory query\nhishtory init %s\nhishtory query\necho mynewercommand\nhishtory query\nothercomputer\nhishtory query\n", userSecret, userSecret) {
		t.Fatalf("hishtory export had unexpected output! out=%#v", out)
	}
}

func testIntegration(t *testing.T) string {
	// Test install
	out := RunInteractiveBashCommands(t, `
	gvm use go1.17
	cd /home/david/code/hishtory
	go build -o /tmp/client client/client.go
	/tmp/client install`)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output: matches=%#v", matches)
	}
	userSecret := matches[1]

	// Test the status subcommand
	out = RunInteractiveBashCommands(t, `
		hishtory status
	`)
	if out != fmt.Sprintf("Hishtory: e2e sync\nEnabled: true\nSecret Key: %s\nCommit Hash: Unknown\n", userSecret) {
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
	out = RunInteractiveBashCommands(t, `
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

