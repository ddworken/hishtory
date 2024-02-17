package cmd

import (
	"os"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/client/webui"
	"github.com/spf13/cobra"
)

var webUiCmd = &cobra.Command{
	Use:   "web-ui",
	Short: "Serve a basic web UI for interacting with your shell history",
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(webui.StartWebUiServer(hctx.MakeContext()))
		os.Exit(1)
	},
}

func init() {
	rootCmd.AddCommand(webUiCmd)
}
