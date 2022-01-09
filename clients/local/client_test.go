package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"testing"

	"github.com/ddworken/hishtory/shared"
)

func TestIntegration(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)

	// Run the test
	cmd := exec.Command("bash", "--init-file", "test_interaction.sh")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("unexpected error when running test script: %v", err)
	}
	fmt.Printf("%q\n", out.String())
}
