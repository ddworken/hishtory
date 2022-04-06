package main

import (
	"bytes"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/ddworken/hishtory/shared"
)

func RunInteractiveBashCommands(t *testing.T, script string) string {
	shared.Check(t, ioutil.WriteFile("/tmp/hishtory-test-in.sh", []byte("set -euo pipefail\n" + script), 0600))
	cmd := exec.Command("bash", "-i")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout = &out
	var err bytes.Buffer
	cmd.Stderr = &err
	shared.CheckWithInfo(t, cmd.Run(), out.String()+err.String())
	return out.String()
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
	/tmp/client install`)
	match, err := regexp.MatchString(`Setting secret hishtory key to .*`, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("unexpected output from install: %v", out)
	}

	// Set the secret key to the previous secret key 
	out = RunInteractiveBashCommands(t, `hishtory init ` + userSecret)
	if !strings.Contains(out, "Setting secret hishtory key to " + userSecret) {
		t.Fatalf("Failed to re-init with the user secret: %v", out)
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

	// TODO: Set up a third client and check it gets commands from both previous ones

	// TODO: Test the live update flow
}

func testIntegration(t *testing.T) string {
	// TODO(ddworken): Test the status subcommand

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

// TODO(ddworken): Test export
