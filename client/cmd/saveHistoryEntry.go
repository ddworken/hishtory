package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/spf13/cobra"
)

var saveHistoryEntryCmd = &cobra.Command{
	Use:    "saveHistoryEntry",
	Hidden: true,
	Short:  "[Internal-only] The command used to save history entries",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(maybeUploadSkippedHistoryEntries(ctx))
		saveHistoryEntry(ctx)
	},
}

func maybeUploadSkippedHistoryEntries(ctx *context.Context) error {
	config := hctx.GetConf(ctx)
	if !config.HaveMissedUploads {
		return nil
	}
	if config.IsOffline {
		return nil
	}

	// Upload the missing entries
	db := hctx.GetDb(ctx)
	query := fmt.Sprintf("after:%s", time.Unix(config.MissedUploadTimestamp, 0).Format("2006-01-02"))
	entries, err := lib.Search(ctx, db, query, 0)
	if err != nil {
		return fmt.Errorf("failed to retrieve history entries that haven't been uploaded yet: %v", err)
	}
	hctx.GetLogger().Infof("Uploading %d history entries that previously failed to upload (query=%#v)\n", len(entries), query)
	jsonValue, err := lib.EncryptAndMarshal(config, entries)
	if err != nil {
		return err
	}
	_, err = lib.ApiPost("/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
	if err != nil {
		// Failed to upload the history entry, so we must still be offline. So just return nil and we'll try again later.
		return nil
	}

	// Mark down that we persisted it
	config.HaveMissedUploads = false
	config.MissedUploadTimestamp = 0
	err = hctx.SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to mark a history entry as uploaded: %v", err)
	}
	return nil
}

func saveHistoryEntry(ctx *context.Context) {
	config := hctx.GetConf(ctx)
	if !config.IsEnabled {
		hctx.GetLogger().Infof("Skipping saving a history entry because hishtory is disabled\n")
		return
	}
	entry, err := lib.BuildHistoryEntry(ctx, os.Args)
	lib.CheckFatalError(err)
	if entry == nil {
		hctx.GetLogger().Infof("Skipping saving a history entry because we did not build a history entry (was the command prefixed with a space and/or empty?)\n")
		return
	}

	// Persist it locally
	db := hctx.GetDb(ctx)
	err = lib.ReliableDbCreate(db, *entry)
	lib.CheckFatalError(err)

	// Persist it remotely
	if !config.IsOffline {
		jsonValue, err := lib.EncryptAndMarshal(config, []*data.HistoryEntry{entry})
		lib.CheckFatalError(err)
		_, err = lib.ApiPost("/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
		if err != nil {
			if lib.IsOfflineError(err) {
				hctx.GetLogger().Infof("Failed to remotely persist hishtory entry because we failed to connect to the remote server! This is likely because the device is offline, but also could be because the remote server is having reliability issues. Original error: %v", err)
				if !config.HaveMissedUploads {
					config.HaveMissedUploads = true
					config.MissedUploadTimestamp = time.Now().Unix()
					lib.CheckFatalError(hctx.SetConfig(config))
				}
			} else {
				lib.CheckFatalError(err)
			}
		}
	}

	// Check if there is a pending dump request and reply to it if so
	dumpRequests, err := lib.GetDumpRequests(config)
	if err != nil {
		if lib.IsOfflineError(err) {
			// It is fine to just ignore this, the next command will retry the API and eventually we will respond to any pending dump requests
			dumpRequests = []*shared.DumpRequest{}
			hctx.GetLogger().Infof("Failed to check for dump requests because we failed to connect to the remote server!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	if len(dumpRequests) > 0 {
		lib.CheckFatalError(lib.RetrieveAdditionalEntriesFromRemote(ctx))
		entries, err := lib.Search(ctx, db, "", 0)
		lib.CheckFatalError(err)
		var encEntries []*shared.EncHistoryEntry
		for _, entry := range entries {
			enc, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
			lib.CheckFatalError(err)
			encEntries = append(encEntries, &enc)
		}
		reqBody, err := json.Marshal(encEntries)
		lib.CheckFatalError(err)
		for _, dumpRequest := range dumpRequests {
			if !config.IsOffline {
				_, err := lib.ApiPost("/api/v1/submit-dump?user_id="+dumpRequest.UserId+"&requesting_device_id="+dumpRequest.RequestingDeviceId+"&source_device_id="+config.DeviceId, "application/json", reqBody)
				lib.CheckFatalError(err)
			}
		}
	}

	// Handle deletion requests
	lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
}

func init() {
	rootCmd.AddCommand(saveHistoryEntryCmd)
}
