package shared

import (
	"os"
	"path"
	"strings"
	"testing"
)

func TestSetup(t *testing.T) {
	defer BackupAndRestore(t)
	homedir, err := os.UserHomeDir()
	Check(t, err)
	if _, err := os.Stat(path.Join(homedir, SECRET_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	Check(t, Setup([]string{}))
	if _, err := os.Stat(path.Join(homedir, SECRET_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, SECRET_PATH))
	Check(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
}

func TestBuildHistoryEntry(t *testing.T) {
	defer BackupAndRestore(t)
	Check(t, Setup([]string{}))
	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  "})
	Check(t, err)
	if entry.UserSecret == "" || len(entry.UserSecret) < 10 || strings.TrimSpace(entry.UserSecret) != entry.UserSecret {
		t.Fatalf("history entry has unexpected user secret: %v", entry.UserSecret)
	}
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
}

func TestGetUserSecret(t *testing.T) {
	defer BackupAndRestore(t)
	Check(t, Setup([]string{}))
	secret1, err := GetUserSecret()
	Check(t, err)
	if len(secret1) < 10 || strings.Contains(secret1, " ") || strings.Contains(secret1, "\n") {
		t.Fatalf("unexpected secret: %v", secret1)
	}

	Check(t, Setup([]string{}))
	secret2, err := GetUserSecret()
	Check(t, err)

	if secret1 == secret2 {
		t.Fatalf("GetUserSecret() returned the same values for different setups! val=%v", secret1)
	}
}
