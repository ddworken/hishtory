package lib

import (
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"
)

func TestSetup(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	homedir, err := os.UserHomeDir()
	shared.Check(t, err)
	if _, err := os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	shared.Check(t, Setup([]string{}))
	if _, err := os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	shared.Check(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
}

func TestBuildHistoryEntry(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	shared.Check(t, Setup([]string{}))
	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	shared.Check(t, err)
	if entry.ExitCode != 120 {
		t.Fatalf("history entry has unexpected exit code: %v", entry.ExitCode)
	}
	if entry.LocalUsername != "david" {
		t.Fatalf("history entry has unexpected user name: %v", entry.LocalUsername)
	}
	if !strings.HasPrefix(entry.CurrentWorkingDirectory, "/") && !strings.HasPrefix(entry.CurrentWorkingDirectory, "~/") {
		t.Fatalf("history entry has unexpected cwd: %v", entry.CurrentWorkingDirectory)
	}
	if entry.Command != "ls /" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if entry.StartTime.Format(time.RFC3339) != "2022-01-09T16:35:58-08:00" {
		t.Fatalf("history entry has incorrect start time: %v", entry.StartTime.Format(time.RFC3339))
	}
}

func TestGetUserSecret(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	shared.Check(t, Setup([]string{}))
	secret1, err := GetUserSecret()
	shared.Check(t, err)
	if len(secret1) < 10 || strings.Contains(secret1, " ") || strings.Contains(secret1, "\n") {
		t.Fatalf("unexpected secret: %v", secret1)
	}

	shared.Check(t, Setup([]string{}))
	secret2, err := GetUserSecret()
	shared.Check(t, err)

	if secret1 == secret2 {
		t.Fatalf("GetUserSecret() returned the same values for different setups! val=%v", secret1)
	}
}
