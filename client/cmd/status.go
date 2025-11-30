package cmd

import (
	"fmt"
	"strings"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	verbose    *bool
	configFlag *bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "View status info including the secret key which is needed to sync shell history from another machine",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Printf("hiSHtory: v0.%s\nEnabled: %v\n", lib.Version, config.IsEnabled)
		fmt.Printf("Secret Key: %s\n", config.UserSecret)
		if *verbose {
			fmt.Printf("User ID: %s\n", data.UserId(config.UserSecret))
			fmt.Printf("Device ID: %s\n", config.DeviceId)
			printOnlineStatus(config)
		}
		fmt.Printf("Commit Hash: %s\n", lib.GitCommit)
		if *configFlag {
			y, err := yaml.Marshal(config)
			if err != nil {
				lib.CheckFatalError(fmt.Errorf("failed to marshal config to yaml: %w", err))
			}
			indented := "\t" + strings.ReplaceAll(string(y), "\n", "\n\t")
			fmt.Printf("Full Config:\n%s\n", indented)
		}
	},
}

func printOnlineStatus(config *hctx.ClientConfig) {
	if config.IsOffline {
		fmt.Println("Sync Mode: Disabled")
	} else {
		fmt.Println("Sync Mode: Enabled")
		if config.BackendType != "" && config.BackendType != "http" {
			fmt.Printf("Sync Backend: %s", config.BackendType)
			if config.S3Config != nil {
				fmt.Printf(" (bucket: %s, region: %s)", config.S3Config.Bucket, config.S3Config.Region)
			}
			fmt.Println()
		} else if lib.GetServerHostname() != lib.DefaultServerHostname {
			fmt.Println("Sync Server: " + lib.GetServerHostname())
		}
		if config.HaveMissedUploads || len(config.PendingDeletionRequests) > 0 {
			fmt.Println("Sync Status: Unsynced (device is offline?)")
			fmt.Printf("  HaveMissedUploads=%v MissedUploadTimestamp=%v len(PendingDeletionRequests)=%v\n", config.HaveMissedUploads, config.MissedUploadTimestamp, len(config.PendingDeletionRequests))
		} else {
			fmt.Println("Sync Status: Synced")
		}
	}
}

func init() {
	rootCmd.AddCommand(statusCmd)
	verbose = statusCmd.Flags().BoolP("verbose", "v", false, "Display verbose hiSHtory information")
	configFlag = statusCmd.Flags().Bool("full-config", false, "Display hiSHtory's full config")
}
