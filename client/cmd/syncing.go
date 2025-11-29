package cmd

import (
	"context"
	"fmt"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/spf13/cobra"
)

var syncingCmd = &cobra.Command{
	Use:       "syncing",
	Short:     "Configure syncing to enable or disable syncing with the hishtory backend",
	Long:      "Run `hishtory syncing disable` to disable syncing and `hishtory syncing enable` to enable syncing.",
	ValidArgs: []string{"disable", "enable"},
	Args:      cobra.MatchAll(cobra.OnlyValidArgs, cobra.ExactArgs(1)),
	Run: func(cmd *cobra.Command, args []string) {
		syncingStatus := false
		if args[0] == "disable" {
			syncingStatus = false
		} else if args[0] == "enable" {
			syncingStatus = true
		} else {
			lib.CheckFatalError(fmt.Errorf("unexpected syncing argument %q", args[0]))
		}

		ctx := hctx.MakeContext()
		conf := hctx.GetConf(ctx)
		if syncingStatus {
			if conf.IsOffline {
				lib.CheckFatalError(switchToOnline(ctx))
				fmt.Println("Enabled syncing successfully")
			} else {
				lib.CheckFatalError(fmt.Errorf("device is already online"))
			}
		} else {
			if conf.IsOffline {
				lib.CheckFatalError(fmt.Errorf("device is already offline"))
			} else {
				lib.CheckFatalError(switchToOffline(ctx))
				fmt.Println("Disabled syncing successfully")
			}
		}
	},
}

func switchToOnline(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	config.IsOffline = false
	err := hctx.SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to switch device to online due to error while setting config: %w", err)
	}
	err = registerAndBootstrapDevice(ctx, config, hctx.GetDb(ctx), config.UserSecret)
	if err != nil {
		return fmt.Errorf("failed to register device with backend: %w", err)
	}
	err = lib.Reupload(ctx)
	if err != nil {
		return fmt.Errorf("failed to switch device to online due to error while uploading history entries: %w", err)
	}
	return nil
}

func switchToOffline(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	config.IsOffline = true
	err := hctx.SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to switch device to offline due to error while setting config: %w", err)
	}
	b, ctx := lib.GetSyncBackend(ctx)
	err = b.Uninstall(ctx, data.UserId(config.UserSecret), config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to switch device to offline due to error while deleting sync state: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(syncingCmd)
}
