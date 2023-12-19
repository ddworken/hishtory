package cmd

import (
	"log"
	"os"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var configDeleteCmd = &cobra.Command{
	Use:     "config-delete",
	Short:   "Delete a config option",
	GroupID: GROUP_ID_CONFIG,
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(cmd.Help())
		os.Exit(1)
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
		// Delete it from the list of custom columns
		newColumns := make([]hctx.CustomColumnDefinition, 0)
		foundColumnToDelete := false
		for _, c := range config.CustomColumns {
			if c.ColumnName == columnName {
				foundColumnToDelete = true
			} else {
				newColumns = append(newColumns, c)
			}
		}
		if !foundColumnToDelete {
			log.Fatalf("Did not find a column with name %#v to delete (current columns = %#v)", columnName, config.CustomColumns)
		}
		config.CustomColumns = newColumns
		// And also delete it from the list of displayed columns
		newDisplayedColumns := make([]string, 0)
		for _, c := range config.DisplayedColumns {
			if c != columnName {
				newDisplayedColumns = append(newDisplayedColumns, c)
			}
		}
		config.DisplayedColumns = newDisplayedColumns
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
