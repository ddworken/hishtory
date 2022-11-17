package cmd

import (
	"fmt"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/spf13/cobra"
)

var GROUP_ID_CONFIG string = "group_id_config"

var configGetCmd = &cobra.Command{
	Use:     "config-get",
	Short:   "Get the value of a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var getEnableControlRCmd = &cobra.Command{
	Use:   "enable-control-r",
	Short: "Whether hishtory replaces your shell's default control-r",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.ControlRSearchEnabled)
	},
}

var getFilterDuplicateCommandsCmd = &cobra.Command{
	Use:   "filter-duplicate-commands",
	Short: "Whether hishtory filters out duplicate commands when displaying your history",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.FilterDuplicateCommands)
	},
}

var getDisplayedColumnsCmd = &cobra.Command{
	Use:   "displayed-columns",
	Short: "The list of columns that hishtory displays",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		for _, col := range config.DisplayedColumns {
			if strings.Contains(col, " ") {
				fmt.Printf("%q ", col)
			} else {
				fmt.Print(col + " ")
			}
		}
		fmt.Print("\n")
	},
}

var getTimestampFormatCmd = &cobra.Command{
	Use:   "timestamp-format",
	Short: "The go format string to use for formatting the timestamp",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.TimestampFormat)
	},
}

var getCustomColumnsCmd = &cobra.Command{
	Use:   "custom-columns",
	Short: "The list of custom columns that hishtory is tracking",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		for _, cc := range config.CustomColumns {
			fmt.Println(cc.ColumnName + ":   " + cc.ColumnCommand)
		}
	},
}

func init() {
	rootCmd.AddCommand(configGetCmd)
	configGetCmd.AddCommand(getEnableControlRCmd)
	configGetCmd.AddCommand(getFilterDuplicateCommandsCmd)
	configGetCmd.AddCommand(getDisplayedColumnsCmd)
	configGetCmd.AddCommand(getTimestampFormatCmd)
	configGetCmd.AddCommand(getCustomColumnsCmd)
}
