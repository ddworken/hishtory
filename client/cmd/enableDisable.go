package cmd

import (
	"context"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var enableCmd = &cobra.Command{
	Use:     "enable",
	Short:   "Enable hiSHtory recording",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(Enable(ctx))
	},
}

var disableCmd = &cobra.Command{
	Use:     "disable",
	Short:   "Disable hiSHtory recording",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(Disable(ctx))
	},
}

func Enable(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	config.IsEnabled = true
	return hctx.SetConfig(config)
}

func Disable(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	config.IsEnabled = false
	return hctx.SetConfig(config)
}

func init() {
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(disableCmd)
}
