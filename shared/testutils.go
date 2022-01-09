package shared

import (
	"os"
	"path"
	"testing"
	"time"
)

func Check(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func BackupAndRestore(t *testing.T) func() {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	os.Rename(path.Join(homedir, DB_PATH), path.Join(homedir, DB_PATH+".bak"))
	os.Rename(path.Join(homedir, SECRET_PATH), path.Join(homedir, SECRET_PATH+".bak"))
	return func() {
		Check(t, os.Rename(path.Join(homedir, DB_PATH+".bak"), path.Join(homedir, DB_PATH)))
		Check(t, os.Rename(path.Join(homedir, SECRET_PATH+".bak"), path.Join(homedir, SECRET_PATH)))
	}
}

func EntryEquals(entry1, entry2 HistoryEntry) bool {
	return entry1.UserSecret == entry2.UserSecret &&
		entry1.LocalUsername == entry2.LocalUsername &&
		entry1.Hostname == entry2.Hostname &&
		entry1.Command == entry2.Command &&
		entry1.CurrentWorkingDirectory == entry2.CurrentWorkingDirectory &&
		entry1.ExitCode == entry2.ExitCode &&
		entry1.StartTime.Format(time.RFC3339) == entry2.StartTime.Format(time.RFC3339) &&
		entry1.EndTime.Format(time.RFC3339) == entry2.EndTime.Format(time.RFC3339)

}
