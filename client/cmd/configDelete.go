package cmd

import (
	"log"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var configDeleteCmd = &cobra.Command{
	Use:     "config-delete",
	Short:   "Delete a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var deleteCustomColumnsCmd = &cobra.Command{
	Use:   "custom-columns",
	Short: "Delete a custom column",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		columnName := args[0]
		if config.CustomColumns == nil {
			log.Fatalf("Did not find a column with name %#v to delete (current columns = %#v)", columnName, config.CustomColumns)
		}
		newColumns := make([]hctx.CustomColumnDefinition, 0)
		deletedColumns := false
		for _, c := range config.CustomColumns {
			if c.ColumnName != columnName {
				newColumns = append(newColumns, c)
				deletedColumns = true
			}
		}
		if !deletedColumns {
			log.Fatalf("Did not find a column with name %#v to delete (current columns = %#v)", columnName, config.CustomColumns)
		}
		config.CustomColumns = newColumns
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}
var deleteDisplayedColumnCommand = &cobra.Command{
	Use:   "displayed-columns",
	Short: "Delete a displayed column",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		deletedColumns := args
		newColumns := make([]string, 0)
		for _, c := range config.DisplayedColumns {
			isDeleted := false
			for _, d := range deletedColumns {
				if c == d {
					isDeleted = true
				}
			}
			if !isDeleted {
				newColumns = append(newColumns, c)
			}
		}
		config.DisplayedColumns = newColumns
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

func init() {
	rootCmd.AddCommand(configDeleteCmd)
	configDeleteCmd.AddCommand(deleteCustomColumnsCmd)
	configDeleteCmd.AddCommand(deleteDisplayedColumnCommand)
}
