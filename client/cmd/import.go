package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:     "import",
	GroupID: GROUP_ID_MANAGEMENT,
	Hidden:  true,
	Short:   "Re-import history entries from your existing shell history",
	Long:    "Note that you may also pipe commands to be imported in via stdin. For example `history | hishtory import`.",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		numImported, err := lib.ImportHistory(ctx, true, true)
		lib.CheckFatalError(err)
		if numImported > 0 {
			fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
		}
	},
}

var importJsonCmd = &cobra.Command{
	Use:     "import-json",
	GroupID: GROUP_ID_MANAGEMENT,
	Short:   "Import history entries formatted in JSON lines format into hiSHtory",
	Long: "Data is read from stdin. For example: `cat data.txt | hishtory import-json`.\n\nExample JSON format:\n\n```\n" +
		"{\"command\":\"echo foo\"}\n" +
		"{\"command\":\"echo bar\", \"current_working_directory\": \"/tmp/\"}\n" +
		"{\"command\":\"ls\",\"current_working_directory\":\"/tmp/\",\"local_username\":\"david\",\"hostname\":\"foo\",\"home_directory\":\"/Users/david\",\"exit_code\":0,\"start_time\":\"2024-12-30T01:14:34.656407Z\",\"end_time\":\"2024-12-30T01:14:34.657407Z\"}\n```\n",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		numImported, err := importFromJson(ctx)
		lib.CheckFatalError(err)
		fmt.Printf("Imported %v history entries\n", numImported)
	},
}

func importFromJson(ctx context.Context) (int, error) {
	// Get the data needed for filling in any missing columns
	currentUser, err := user.Current()
	if err != nil {
		return 0, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return 0, err
	}
	homedir := hctx.GetHome(ctx)

	// Build the entries
	lines, err := lib.ReadStdin()
	if err != nil {
		return 0, fmt.Errorf("failed to read stdin for import: %w", err)
	}
	var entries []data.HistoryEntry
	importEntryId := uuid.Must(uuid.NewRandom()).String()
	importTimestamp := time.Now().UTC()
	for i, line := range lines {
		var entry data.HistoryEntry
		err := json.Unmarshal([]byte(line), &entry)
		if err != nil {
			return 0, fmt.Errorf("failed to parse JSON line %#v: %w", line, err)
		}
		if entry.Command == "" {
			return 0, fmt.Errorf("cannot import history entries without a command, JSON line: %#v", line)
		}
		if entry.LocalUsername == "" {
			entry.LocalUsername = currentUser.Username
		}
		if entry.Hostname == "" {
			entry.Hostname = hostname
		}
		if entry.CurrentWorkingDirectory == "" {
			entry.CurrentWorkingDirectory = "Unknown"
		}
		if entry.HomeDirectory == "" {
			entry.HomeDirectory = homedir
		}
		// Set the timestamps so that they are monotonically increasing
		startTime := importTimestamp.Add(time.Millisecond * time.Duration(i*2))
		endTime := startTime.Add(time.Millisecond)
		if entry.StartTime == *new(time.Time) {
			entry.StartTime = startTime
		}
		if entry.EndTime == *new(time.Time) {
			entry.EndTime = endTime
		}
		entry.DeviceId = hctx.GetConf(ctx).DeviceId
		entry.EntryId = fmt.Sprintf("%s-%d", importEntryId, i)
		entries = append(entries, entry)
	}

	// Insert the entries into the DB
	db := hctx.GetDb(ctx)
	err = db.CreateInBatches(entries, lib.ImportBatchSize).Error
	if err != nil {
		return 0, fmt.Errorf("failed to insert entries into DB: %w", err)
	}

	// Trigger a checkpoint so that these bulk entries are added from the WAL to the main DB
	err = db.Exec("PRAGMA wal_checkpoint").Error
	if err != nil {
		return 0, fmt.Errorf("failed to checkpoint imported history: %w", err)
	}
	return len(entries), nil
}

func init() {
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(importJsonCmd)
}
