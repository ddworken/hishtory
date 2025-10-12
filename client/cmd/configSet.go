package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var configSetCmd = &cobra.Command{
	Use:     "config-set",
	Short:   "Set the value of a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(cmd.Help())
		os.Exit(1)
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
		fmt.Println("Updated the control-r integration, please restart your shell for this to take effect...")
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

var setBetaModeCommand = &cobra.Command{
	Use:       "beta-mode",
	Short:     "Enable beta-mode to opt-in to unreleased features",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.BetaMode = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setDefaultFilterCommand = &cobra.Command{
	Use:   "default-filter",
	Short: "Add a default filter that will be applied to all search queries (e.g. `exit_code:0` to filter to only commands that executed successfully)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.DefaultFilter = args[0]
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setEnableAiCompletionCmd = &cobra.Command{
	Use:       "ai-completion",
	Short:     "Enable AI completion for searches starting with '?'",
	Long:      "Note that AI completion requests are sent to the shared hiSHtory backend and then to OpenAI. Requests are not logged, but still be careful not to put anything sensitive in queries.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.AiCompletion = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setPresavingCmd = &cobra.Command{
	Use:       "presaving",
	Short:     "Enable 'presaving' of shell entries that never finish running",
	Long:      "If enabled, there is a slight risk of duplicate history entries. If disabled, non-terminating history entries will not be recorded.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.EnablePresaving = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setFilterWhitespacePrefixCmd = &cobra.Command{
	Use:       "filter-whitespace-prefix",
	Short:     "Whether to filter out commands that start with whitespace (space, tab, etc.)",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.FilterWhitespacePrefix = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setHighlightMatchesCmd = &cobra.Command{
	Use:       "highlight-matches",
	Short:     "Enable highlight-matches to enable highlighting of matches in the search results",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		val := args[0]
		if val != "true" && val != "false" {
			log.Fatalf("Unexpected config value %s, must be one of: true, false", val)
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.HighlightMatches = (val == "true")
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setDisplayedColumnsCmd = &cobra.Command{
	Use:     "displayed-columns",
	Aliases: []string{"displayed-column"},
	Short:   "The list of columns that hishtory displays",
	Args:    cobra.MinimumNArgs(1),
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

var setColorSchemeCmd = &cobra.Command{
	Use:   "color-scheme",
	Short: "Set a custom color scheme",
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(cmd.Help())
		os.Exit(1)
	},
}

var setColorSchemeSelectedText = &cobra.Command{
	Use:   "selected-text",
	Short: "Set the color of the selected text to the given hexadecimal color",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(validateColor(args[0]))
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.ColorScheme.SelectedText = args[0]
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setColorSchemeSelectedBackground = &cobra.Command{
	Use:   "selected-background",
	Short: "Set the background color of the selected row to the given hexadecimal color",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(validateColor(args[0]))
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.ColorScheme.SelectedBackground = args[0]
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setColorSchemeBorderColor = &cobra.Command{
	Use:   "border-color",
	Short: "Set the color of the table borders",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(validateColor(args[0]))
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.ColorScheme.BorderColor = args[0]
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var compactMode = &cobra.Command{
	Use:       "compact-mode",
	Short:     "Configure whether the TUI should run in compact mode to minimize wasted terminal space",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.ForceCompactMode = args[0] == "true"
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

func validateColor(color string) error {
	if !strings.HasPrefix(color, "#") || len(color) != 7 {
		return fmt.Errorf("color %q is invalid, it should be a hexadecimal color like #663399", color)
	}
	return nil
}

var setAiCompletionEndpoint = &cobra.Command{
	Use:   "ai-completion-endpoint",
	Short: "The AI endpoint to use for AI completions",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.AiCompletionEndpoint = args[0]
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setLogLevelCmd = &cobra.Command{
	Use:       "log-level",
	Short:     "Set the log level for hishtory logs",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"error", "warn", "info", "debug"},
	Run: func(cmd *cobra.Command, args []string) {
		level, err := logrus.ParseLevel(args[0])
		if err != nil {
			lib.CheckFatalError(fmt.Errorf("invalid log level: %v", err))
		}
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.LogLevel = level
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var setFullScreenCmd = &cobra.Command{
	Use:       "full-screen",
	Short:     "Configure whether or not hishtory is configured to run in full-screen mode",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"true", "false"},
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.FullScreenRendering = args[0] == "true"
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

func validateDefaultSearchColumns(ctx context.Context, columns []string) error {
	customColNames, err := lib.GetAllCustomColumnNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to get custom column names: %v", err)
	}
	for _, col := range columns {
		if !slices.Contains(lib.SUPPORTED_DEFAULT_COLUMNS, col) && !slices.Contains(customColNames, col) {
			return fmt.Errorf("column %q is not a valid column name", col)
		}
	}
	return nil
}

var setDefaultSearchColumns = &cobra.Command{
	Use:   "default-search-columns",
	Short: "Set the list of columns that are used for \"default\" search queries that don't use any search atoms",
	Long:  "By default hishtory queries are checked against `command`, `current_working_directory`, and `hostname`. This option can be used to exclude `current_working_directory` and/or `hostname` from default search queries. E.g. `hishtory config-set default-search-columns hostname command` would exclude `current_working_directory` from default searches. Alternatively, it can be used to include custom columns in default searches.",
	Args:  cobra.OnlyValidArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		if !slices.Contains(args, "command") {
			lib.CheckFatalError(fmt.Errorf("command is a required default search column"))
		}
		lib.CheckFatalError(validateDefaultSearchColumns(ctx, args))
		config.DefaultSearchColumns = args
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

func init() {
	rootCmd.AddCommand(configSetCmd)
	configSetCmd.AddCommand(setEnableControlRCmd)
	configSetCmd.AddCommand(setFilterDuplicateCommandsCmd)
	configSetCmd.AddCommand(setFilterWhitespacePrefixCmd)
	configSetCmd.AddCommand(setDisplayedColumnsCmd)
	configSetCmd.AddCommand(setTimestampFormatCmd)
	configSetCmd.AddCommand(setBetaModeCommand)
	configSetCmd.AddCommand(setHighlightMatchesCmd)
	configSetCmd.AddCommand(setEnableAiCompletionCmd)
	configSetCmd.AddCommand(setPresavingCmd)
	configSetCmd.AddCommand(setColorSchemeCmd)
	configSetCmd.AddCommand(setDefaultFilterCommand)
	configSetCmd.AddCommand(setAiCompletionEndpoint)
	configSetCmd.AddCommand(compactMode)
	configSetCmd.AddCommand(setLogLevelCmd)
	configSetCmd.AddCommand(setFullScreenCmd)
	configSetCmd.AddCommand(setDefaultSearchColumns)
	setColorSchemeCmd.AddCommand(setColorSchemeSelectedText)
	setColorSchemeCmd.AddCommand(setColorSchemeSelectedBackground)
	setColorSchemeCmd.AddCommand(setColorSchemeBorderColor)
}
