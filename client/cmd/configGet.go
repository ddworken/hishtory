package cmd

import (
	"fmt"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var GROUP_ID_CONFIG string = "group_id_config"

var configGetCmd = &cobra.Command{
	Use:     "config-get",
	Short:   "Get the value of a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(cmd.Help())
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

var getHighlightMatchesCmd = &cobra.Command{
	Use:   "highlight-matches",
	Short: "Whether hishtory highlights matches in the search results",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.HighlightMatches)
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

var getEnableAiCompletion = &cobra.Command{
	Use:   "ai-completion",
	Short: "Enable AI completion for searches starting with '?'",
	Long:  "Note that AI completion requests are sent to the shared hiSHtory backend and then to OpenAI. Requests are not logged, but still be careful not to put anything sensitive in queries.",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.AiCompletion)
	},
}

var getBetaModeCmd = &cobra.Command{
	Use:   "beta-mode",
	Short: "Enable beta-mode to opt-in to unreleased features",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.BetaMode)
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
	configGetCmd.AddCommand(getBetaModeCmd)
	configGetCmd.AddCommand(getHighlightMatchesCmd)
	configGetCmd.AddCommand(getEnableAiCompletion)
}
