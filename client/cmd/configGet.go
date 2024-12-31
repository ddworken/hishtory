package cmd

import (
	"fmt"
	"os"
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
		os.Exit(1)
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

var getDefaultFilterCmd = &cobra.Command{
	Use:   "default-filter",
	Short: "The default filter that is applied to all search queries",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Printf("%#v", config.DefaultFilter)
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

var getPresavingCmd = &cobra.Command{
	Use:   "presaving",
	Short: "Enable 'presaving' of shell entries that never finish running",
	Long:  "If enabled, there is a slight risk of duplicate history entries. If disabled, non-terminating history entries will not be recorded.",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.EnablePresaving)
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
	Use:     "displayed-columns",
	Aliases: []string{"displayed-column"},
	Short:   "The list of columns that hishtory displays",
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
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.TimestampFormat)
	},
}

var getCustomColumnsCmd = &cobra.Command{
	Use:     "custom-columns",
	Aliases: []string{"custom-column"},
	Short:   "The list of custom columns that hishtory is tracking",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		for _, cc := range config.CustomColumns {
			fmt.Println(cc.ColumnName + ":   " + cc.ColumnCommand)
		}
	},
}

var getColorScheme = &cobra.Command{
	Use:   "color-scheme",
	Short: "Get the currently configured color scheme for selected text in the TUI",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println("selected-text: " + config.ColorScheme.SelectedText)
		fmt.Println("selected-background: " + config.ColorScheme.SelectedBackground)
		fmt.Println("border-color: " + config.ColorScheme.BorderColor)
	},
}

var getCompactMode = &cobra.Command{
	Use:   "compact-mode",
	Short: "Get whether the TUI is running in compact mode to minimize wasted terminal space",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.ForceCompactMode)
	},
}

var getAiCompletionEndpoint = &cobra.Command{
	Use:   "ai-completion-endpoint",
	Short: "The AI endpoint to use for AI completions",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.AiCompletionEndpoint)
	},
}

var getExcludedDefaultSearchColumns = &cobra.Command{
	Use:   "excluded-default-search-columns",
	Short: "Get the list of columns that are excluded from default search queries and are only searchable via search atoms",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		for _, col := range config.ExcludedDefaultSearchColumns {
			fmt.Print(col + " ")
		}
		fmt.Print("\n")
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
	configGetCmd.AddCommand(getPresavingCmd)
	configGetCmd.AddCommand(getColorScheme)
	configGetCmd.AddCommand(getDefaultFilterCmd)
	configGetCmd.AddCommand(getAiCompletionEndpoint)
	configGetCmd.AddCommand(getCompactMode)
	configGetCmd.AddCommand(getLogLevelCmd)
	configGetCmd.AddCommand(getFullScreenCmd)
	configGetCmd.AddCommand(getExcludedDefaultSearchColumns)
}

var getLogLevelCmd = &cobra.Command{
	Use:   "log-level",
	Short: "Get the current log level for hishtory logs",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.LogLevel.String())
	},
}

var getFullScreenCmd = &cobra.Command{
	Use:   "full-screen",
	Short: "Get whether or not hishtory is configured to run in full-screen mode",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println(config.FullScreenRendering)
	},
}
