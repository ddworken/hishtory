package cmd

import (
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Securely update hishtory to the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(lib.Update(hctx.MakeContext()))
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
