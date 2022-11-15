package cmd

import (
	"log"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var configSetCmd = &cobra.Command{
	Use:     "config-set",
	Short:   "Set the value of a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var setEnableControlRCmd = &cobra.Command{
	Use:       "enable-control-r",
	Short:     "Whether hishtory replaces your shell's default control-r",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.ControlRSearchEnabled = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setFilterDuplicateCommandsCmd = &cobra.Command{
	Use:       "filter-duplicate-commands",
	Short:     "Whether hishtory filters out duplicate commands when displaying your history",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.FilterDuplicateCommands = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setDisplayedColumnsCmd = &cobra.Command{
	Use:   "displayed-columns",
	Short: "The list of columns that hishtory displays",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.DisplayedColumns = args
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setTimestampFormatCmd = &cobra.Command{
	Use:   "timestamp-format",
	Short: "The go format string to use for formatting the timestamp",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.TimestampFormat = args[0]
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setCustomColumnsCmd = &cobra.Command{
	Use:   "custom-columns",
	Short: "The list of custom columns that hishtory is tracking",
	Run: func(cmd *cobra.Command, args []string) {
		log.Fatalf("Please use config-add and config-delete to interact with custom-columns")
	},
}

func init() {
	rootCmd.AddCommand(configSetCmd)
	configSetCmd.AddCommand(setEnableControlRCmd)
	configSetCmd.AddCommand(setFilterDuplicateCommandsCmd)
	configSetCmd.AddCommand(setDisplayedColumnsCmd)
	configSetCmd.AddCommand(setTimestampFormatCmd)
	configSetCmd.AddCommand(setCustomColumnsCmd)
}
