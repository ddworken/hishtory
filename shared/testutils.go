package shared

import (
	"testing"
	"time"
	"os"
	"path"
)

func BackupAndRestore(t *testing.T) func() {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH), path.Join(homedir, HISHTORY_PATH, DB_PATH+".bak"))
	os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+".bak"))
	return func() {
		os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH+".bak"), path.Join(homedir, HISHTORY_PATH, DB_PATH))
		os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+".bak"), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH))
	}
}

func Check(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func CheckWithInfo(t *testing.T, err error, additionalInfo string) {
	if err != nil {
		t.Fatalf("Unexpected error: %v! Additional info: %v", err, additionalInfo)
	}
}

func EntryEquals(entry1, entry2 HistoryEntry) bool {
	return 		entry1.LocalUsername == entry2.LocalUsername &&
		entry1.Hostname == entry2.Hostname &&
		entry1.Command == entry2.Command &&
		entry1.CurrentWorkingDirectory == entry2.CurrentWorkingDirectory &&
		entry1.ExitCode == entry2.ExitCode &&
		entry1.StartTime.Format(time.RFC3339) == entry2.StartTime.Format(time.RFC3339) &&
		entry1.EndTime.Format(time.RFC3339) == entry2.EndTime.Format(time.RFC3339)
}

func MakeFakeHistoryEntry(command string) HistoryEntry {
	return HistoryEntry{
		LocalUsername: "david",
		Hostname: "localhost",
		Command: command,
		CurrentWorkingDirectory: "/tmp/",
		ExitCode: 2,
		StartTime: time.Now(),
		EndTime: time.Now(),
	}
}