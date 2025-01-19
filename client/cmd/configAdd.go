package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/spf13/cobra"
)

var configAddCmd = &cobra.Command{
	Use:     "config-add",
	Short:   "Add a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(cmd.Help())
		os.Exit(1)
	},
}

var addCustomColumnsCmd = &cobra.Command{
	Use:     "custom-columns",
	Aliases: []string{"custom-column"},
	Short:   "Add a custom column",
	Args:    cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		columnName := args[0]
		command := args[1]
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		if config.CustomColumns == nil {
			config.CustomColumns = make([]hctx.CustomColumnDefinition, 0)
		}
		for _, existingColumn := range config.CustomColumns {
			if strings.EqualFold(existingColumn.ColumnName, columnName) {
				lib.CheckFatalError(fmt.Errorf("cannot create a column named %#v since there is already one named %#v", existingColumn.ColumnName, columnName))
			}
		}
		config.CustomColumns = append(config.CustomColumns, hctx.CustomColumnDefinition{ColumnName: columnName, ColumnCommand: command})
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var addDisplayedColumnsCmd = &cobra.Command{
	Use:     "displayed-columns",
	Aliases: []string{"displayed-column"},
	Short:   "Add a column to be displayed",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		config.DisplayedColumns = append(config.DisplayedColumns, args...)
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

var addDefaultSearchColumnsCmd = &cobra.Command{
	Use:     "default-search-columns",
	Aliases: []string{"default-search-column"},
	Short:   "Add a column that is used for \"default\" search queries that don't use any search atoms",
	Long:    "By default hishtory queries are checked against `command`, `current_working_directory`, and `hostname`. This option can be used to add additional columns to the list of columns checked in default queries. E.g. to add a custom column named `git_remote` to the list of default search columns, you would run `hishtory config-add default-search-columns git_remote`",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		lib.CheckFatalError(validateDefaultSearchColumns(ctx, args))
		config.DefaultSearchColumns = append(config.DefaultSearchColumns, args...)
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

func init() {
	rootCmd.AddCommand(configAddCmd)
	configAddCmd.AddCommand(addCustomColumnsCmd)
	configAddCmd.AddCommand(addDisplayedColumnsCmd)
	configAddCmd.AddCommand(addDefaultSearchColumnsCmd)
}
