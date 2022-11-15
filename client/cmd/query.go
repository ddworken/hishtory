package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var EXAMPLE_QUERIES string = `Example queries:
'hishtory SUBCOMMAND apt-get'  			# Find shell commands containing 'apt-get'
'hishtory SUBCOMMAND apt-get install'  	# Find shell commands containing 'apt-get' and 'install'
'hishtory SUBCOMMAND curl cwd:/tmp/'  	# Find shell commands containing 'curl' run in '/tmp/'
'hishtory SUBCOMMAND curl user:david'	# Find shell commands containing 'curl' run by 'david'
'hishtory SUBCOMMAND curl host:x1'		# Find shell commands containing 'curl' run on 'x1'
'hishtory SUBCOMMAND exit_code:1'		# Find shell commands that exited with status code 1
'hishtory SUBCOMMAND before:2022-02-01'	# Find shell commands run before 2022-02-01
`

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query your shell history",
	Long:  strings.ReplaceAll(EXAMPLE_QUERIES, "SUBCOMMAND", "query"),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		query(ctx, strings.Join(args, " "))
	},
}

var tqueryCmd = &cobra.Command{
	Use:   "tquery",
	Short: "Interactively query your shell history",
	Long:  strings.ReplaceAll(EXAMPLE_QUERIES, "SUBCOMMAND", "tquery"),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.TuiQuery(ctx, strings.Join(args, " ")))
	},
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export your shell history",
	Long:  strings.ReplaceAll(EXAMPLE_QUERIES, "SUBCOMMAND", "export"),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		export(ctx, strings.Join(os.Args[2:], " "))
	},
}

func export(ctx *context.Context, query string) {
	db := hctx.GetDb(ctx)
	err := lib.RetrieveAdditionalEntriesFromRemote(ctx)
	if err != nil {
		if lib.IsOfflineError(err) {
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

func query(ctx *context.Context, query string) {
	db := hctx.GetDb(ctx)
	err := lib.RetrieveAdditionalEntriesFromRemote(ctx)
	if err != nil {
		if lib.IsOfflineError(err) {
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

func displayBannerIfSet(ctx *context.Context) error {
	respBody, err := lib.GetBanner(ctx)
	if lib.IsOfflineError(err) {
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
}
