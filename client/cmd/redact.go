package cmd

import (
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var force *bool

var GROUP_ID_MANAGEMENT string = "group_id_management"

var redactCmd = &cobra.Command{
	Use:     "redact",
	Aliases: []string{"delete"},
	Short:   "Query for matching commands and remove them from your shell history",
	Long:    "This removes history entries on the current machine and on all remote machines. Supports the same query format as 'hishtory query'.",
	GroupID: GROUP_ID_MANAGEMENT,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.RetrieveAdditionalEntriesFromRemote(ctx))
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		query := strings.Join(args, " ")
		lib.CheckFatalError(lib.Redact(ctx, query, *force))
	},
}

func init() {
	rootCmd.AddCommand(redactCmd)
	force = redactCmd.Flags().Bool("force", false, "Force redaction with no confirmation prompting")
}
