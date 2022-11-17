package cmd

import (
	"fmt"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:    "import",
	Hidden: true,
	Short:  "Re-import history entries from your existing shell history",
	Long:   "Note that you must pipe commands to be imported in via stdin. For example `history | hishtory import`.",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		numImported, err := lib.ImportHistory(ctx, true, true)
		lib.CheckFatalError(err)
		if numImported > 0 {
			fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
		}
	},
}

func init() {
	rootCmd.AddCommand(importCmd)
}
