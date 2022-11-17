package cmd

import (
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var reuploadCmd = &cobra.Command{
	Use:    "reupload",
	Hidden: true,
	Short:  "[Debug Only] Reupload your entire hiSHtory to all other devices",
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(lib.Reupload(hctx.MakeContext()))
	},
}

func init() {
	rootCmd.AddCommand(reuploadCmd)
}
