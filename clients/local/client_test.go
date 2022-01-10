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
	shared.Check(t, ioutil.WriteFile("/tmp/hishtory-test-in.sh", []byte(script), 0600))
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
	defer shared.BackupAndRestore(t)

	// Test install
	out := RunInteractiveBashCommands(t, `
	gvm use go1.17
	cd ../../
	go build -o /tmp/client clients/local/client.go
	/tmp/client install`)
	match, err := regexp.MatchString(`Setting secret hishtory key to .*`, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("unexpected output from install: %v", out)
	}

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
	match, err = regexp.MatchString(`.*~/.*\s+[a-zA-Z]{3} \d+ 2022 \d\d:\d\d:\d\d PST\s+\d{1,2}ms\s+0\s+echo thisisrecorded.*`, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("output is missing the row for `echo thisisrecorded`: %v", out)
	}

	// Test querying for a specific command
	out = RunInteractiveBashCommands(t, "hishtory query foo")
	expected = []string{"echo foo", "ls /foo"}
	unexpected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}
	for _, item := range unexpected {
		if strings.Contains(out, item) {
			t.Fatalf("output is containing unexpected item %#v: %#v", item, out)
		}
	}
}
