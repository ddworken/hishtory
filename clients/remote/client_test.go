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

	// Test init
	out := RunInteractiveBashCommands(t, `
	gvm use go1.17
	cd ../../
	go build -o /tmp/client clients/remote/client.go
	go build -o /tmp/server server/server.go
	/tmp/client install`)
	match, err := regexp.MatchString(`Setting secret hishtory key to .*`, out)
	shared.Check(t, err)
	if !match {
		t.Fatalf("unexpected output from install: %v", out)
	}

	// Test recording commands
	out = RunInteractiveBashCommands(t, `/tmp/server &
		sleep 2 # to give the server time to start
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
	if out != "Listening on localhost:8080\nfoo\nbar\nthisisnotrecorded\nthisisrecorded\n" {
		t.Fatalf("unexpected output from running commands: %#v", out)
	}

	// Test querying for all commands
	out = RunInteractiveBashCommands(t, `/tmp/server & hishtory query`)
	expected := []string{"echo thisisrecorded", "hishtory enable", "echo bar", "echo foo", "ls /foo", "ls /bar", "ls /a"}
	for _, item := range expected {
		if !strings.Contains(out, item) {
			t.Fatalf("output is missing expected item %#v: %#v", item, out)
		}
	}
}
