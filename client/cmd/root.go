/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/ddworken/hishtory/client/lib"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "hishtory",
	Short: "hiSHtory: Better shell history",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddGroup(&cobra.Group{ID: GROUP_ID_QUERYING, Title: "History Searching"})
	rootCmd.AddGroup(&cobra.Group{ID: GROUP_ID_MANAGEMENT, Title: "History Management"})
	rootCmd.AddGroup(&cobra.Group{ID: GROUP_ID_CONFIG, Title: "Configuration"})
	rootCmd.AddGroup(&cobra.Group{ID: GROUP_ID_INSTALL, Title: "Installation"})
	rootCmd.Version = "v0." + lib.Version
}
