package cmd

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/client/tui"
	"github.com/spf13/cobra"
)

var EXAMPLE_QUERIES string = `Example queries:
'hishtory SUBCOMMAND apt-get'  		# Find shell commands containing 'apt-get'
'hishtory SUBCOMMAND apt-get install'  	# Find shell commands containing 'apt-get' and 'install'
'hishtory SUBCOMMAND curl cwd:/tmp/'  	# Find shell commands containing 'curl' run in '/tmp/'
'hishtory SUBCOMMAND curl user:david'	# Find shell commands containing 'curl' run by 'david'
'hishtory SUBCOMMAND curl host:x1'		# Find shell commands containing 'curl' run on 'x1'
'hishtory SUBCOMMAND exit_code:1'		# Find shell commands that exited with status code 1
'hishtory SUBCOMMAND before:2022-02-01'	# Find shell commands run before 2022-02-01
`

var GROUP_ID_QUERYING string = "group_id:querying"

var queryCmd = &cobra.Command{
	Use:                "query",
	Short:              "Query your shell history and display the results in an ASCII art table",
	GroupID:            GROUP_ID_QUERYING,
	Long:               strings.ReplaceAll(EXAMPLE_QUERIES, "SUBCOMMAND", "query"),
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		query(ctx, strings.Join(args, " "))
	},
}

var tqueryCmd = &cobra.Command{
	Use:                "tquery",
	Short:              "Interactively query your shell history in a TUI interface",
	GroupID:            GROUP_ID_QUERYING,
	Long:               strings.ReplaceAll(EXAMPLE_QUERIES, "SUBCOMMAND", "tquery"),
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(tui.TuiQuery(ctx, strings.Join(args, " ")))
	},
}

var exportCmd = &cobra.Command{
	Use:                "export",
	Short:              "Export your shell history and display just the raw commands",
	GroupID:            GROUP_ID_QUERYING,
	Long:               strings.ReplaceAll(EXAMPLE_QUERIES, "SUBCOMMAND", "export"),
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		export(ctx, strings.Join(args, " "))
	},
}

var updateLocalDbFromRemoteCmd = &cobra.Command{
	Use:     "updateLocalDbFromRemote",
	Hidden:  true,
	Short:   "[Internal-only] Update local DB from remote",
	GroupID: GROUP_ID_QUERYING,
	Run: func(cmd *cobra.Command, args []string) {
		// Periodically, run a query so as to ensure that the local DB stays mostly up to date and that we don't
		// accumulate a large number of entries that this device doesn't know about. This ensures that queries
		// are always reasonably complete and fast (even when offline).
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		if config.IsOffline {
			return
		}
		// Do it a random percent of the time, which should be approximately often enough.
		if rand.Intn(20) == 0 && !config.IsOffline {
			err := lib.RetrieveAdditionalEntriesFromRemote(ctx, "preload")
			if config.BetaMode {
				lib.CheckFatalError(err)
			} else if err != nil {
				hctx.GetLogger().Infof("updateLocalDbFromRemote: Failed to RetrieveAdditionalEntriesFromRemote: %v", err)
			}
			err = lib.ProcessDeletionRequests(ctx)
			if config.BetaMode {
				lib.CheckFatalError(err)
			} else if err != nil {
				hctx.GetLogger().Infof("updateLocalDbFromRemote: Failed to ProcessDeletionRequests: %v", err)
			}
		}
	},
}

func export(ctx context.Context, query string) {
	db := hctx.GetDb(ctx)
	err := lib.RetrieveAdditionalEntriesFromRemote(ctx, "export")
	if err != nil {
		if lib.IsOfflineError(ctx, err) {
			fmt.Println("Warning: hishtory is offline so this may be missing recent results from your other machines!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	data, err := lib.Search(ctx, db, query, 0)
	lib.CheckFatalError(err)
	for i := len(data) - 1; i >= 0; i-- {
		fmt.Println(data[i].Command)
	}
}

func query(ctx context.Context, query string) {
	db := hctx.GetDb(ctx)
	err := lib.RetrieveAdditionalEntriesFromRemote(ctx, "query")
	if err != nil {
		if lib.IsOfflineError(ctx, err) {
			fmt.Println("Warning: hishtory is offline so this may be missing recent results from your other machines!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	lib.CheckFatalError(displayBannerIfSet(ctx))
	numResults := 25
	data, err := lib.Search(ctx, db, query, numResults*5)
	lib.CheckFatalError(err)
	lib.CheckFatalError(lib.DisplayResults(ctx, data, numResults))
}

func displayBannerIfSet(ctx context.Context) error {
	respBody, err := lib.GetBanner(ctx)
	if lib.IsOfflineError(ctx, err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(respBody) > 0 {
		fmt.Println(string(respBody))
	}
	return nil
}

func init() {
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(tqueryCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(updateLocalDbFromRemoteCmd)
}
