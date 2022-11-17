package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Completely uninstall hiSHtory and remove your shell history",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Are you sure you want to uninstall hiSHtory and delete all locally saved history data [y/N]")
		reader := bufio.NewReader(os.Stdin)
		resp, err := reader.ReadString('\n')
		lib.CheckFatalError(err)
		if strings.TrimSpace(resp) != "y" {
			fmt.Printf("Aborting uninstall per user response of %#v\n", strings.TrimSpace(resp))
			return
		}
		lib.CheckFatalError(lib.Uninstall(hctx.MakeContext()))
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}
