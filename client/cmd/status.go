package cmd

import (
	"fmt"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var verbose *bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "View status info including the secret key which is needed to sync shell	history from another machine",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Printf("hiSHtory: v0.%s\nEnabled: %v\n", lib.Version, config.IsEnabled)
		fmt.Printf("Secret Key: %s\n", config.UserSecret)
		if *verbose {
			fmt.Printf("User ID: %s\n", data.UserId(config.UserSecret))
			fmt.Printf("Device ID: %s\n", config.DeviceId)
			printDumpStatus(config)
		}
		fmt.Printf("Commit Hash: %s\n", lib.GitCommit)
	},
}

func printDumpStatus(config hctx.ClientConfig) {
	dumpRequests, err := lib.GetDumpRequests(config)
	lib.CheckFatalError(err)
	fmt.Printf("Dump Requests: ")
	for _, d := range dumpRequests {
		fmt.Printf("%#v, ", *d)
	}
	fmt.Print("\n")
}

func init() {
	rootCmd.AddCommand(statusCmd)
	verbose = statusCmd.Flags().BoolP("verbose", "v", false, "Display verbose hiSHtory information")
}
