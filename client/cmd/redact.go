package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"

	"github.com/spf13/cobra"
)

var GROUP_ID_MANAGEMENT string = "group_id_management"

var redactCmd = &cobra.Command{
	Use:                "redact",
	Aliases:            []string{"delete"},
	Short:              "Query for matching commands and remove them from your shell history",
	Long:               "This removes history entries on the current machine and on all remote machines. Supports the same query format as 'hishtory query'.",
	GroupID:            GROUP_ID_MANAGEMENT,
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		skipOnlineRedaction := false
		if !hctx.GetConf(ctx).IsOffline && !lib.CanReachBackend(ctx) {
			fmt.Printf("Cannot reach hishtory backend (is this device offline?) so redaction will only apply to this device and not other synced devices. Would you like to continue with a local-only redaction anyways? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			resp, err := reader.ReadString('\n')
			lib.CheckFatalError(err)
			if strings.TrimSpace(resp) != "y" {
				fmt.Printf("Aborting redact per user response of %#v\n", strings.TrimSpace(resp))
				return
			}
			skipOnlineRedaction = true
		}
		lib.CheckFatalError(lib.RetrieveAdditionalEntriesFromRemote(ctx, "redact"))
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		query := strings.Join(args, " ")
		lib.CheckFatalError(redact(ctx, query, os.Getenv("HISHTORY_REDACT_FORCE") != "", skipOnlineRedaction))
	},
}

func redact(ctx context.Context, query string, skipUserConfirmation, skipOnlineRedaction bool) error {
	tx, err := lib.MakeWhereQueryFromSearch(ctx, hctx.GetDb(ctx), query)
	if err != nil {
		return err
	}
	var historyEntries []*data.HistoryEntry
	res := tx.Find(&historyEntries)
	if res.Error != nil {
		return res.Error
	}
	if skipUserConfirmation {
		fmt.Printf("Permanently deleting %d entries\n", len(historyEntries))
	} else {
		fmt.Printf("This will permanently delete %d entries, are you sure? [y/N] ", len(historyEntries))
		reader := bufio.NewReader(os.Stdin)
		resp, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		if strings.TrimSpace(resp) != "y" {
			fmt.Printf("Aborting redact per user response of %#v\n", strings.TrimSpace(resp))
			return nil
		}
	}
	tx, err = lib.MakeWhereQueryFromSearch(ctx, hctx.GetDb(ctx), query)
	if err != nil {
		return err
	}
	res = tx.Delete(&data.HistoryEntry{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > int64(len(historyEntries))+1 || res.RowsAffected < int64(len(historyEntries))-1 {
		return fmt.Errorf("DB deleted %d rows, when we only expected to delete %d rows, something may have gone wrong", res.RowsAffected, len(historyEntries))
	}
	err = deleteOnRemoteInstances(ctx, historyEntries)
	if err != nil && !skipOnlineRedaction {
		return err
	}
	return nil
}

func deleteOnRemoteInstances(ctx context.Context, historyEntries []*data.HistoryEntry) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}

	var deletionRequest shared.DeletionRequest
	deletionRequest.SendTime = time.Now()
	deletionRequest.UserId = data.UserId(config.UserSecret)

	for _, entry := range historyEntries {
		deletionRequest.Messages.Ids = append(deletionRequest.Messages.Ids,
			shared.MessageIdentifier{DeviceId: entry.DeviceId, EndTime: entry.EndTime, EntryId: entry.EntryId},
		)
	}
	return lib.SendDeletionRequest(ctx, deletionRequest)
}

func init() {
	rootCmd.AddCommand(redactCmd)
}
