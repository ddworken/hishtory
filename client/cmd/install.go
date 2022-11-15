package cmd

import (
	"fmt"
	"os"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:    "install",
	Hidden: true,
	Short:  "Copy this binary to ~/.hishtory/ and configure your shell to use it for recording your shell history",
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(lib.Install())
		if os.Getenv("HISHTORY_SKIP_INIT_IMPORT") == "" {
			db, err := hctx.OpenLocalSqliteDb()
			lib.CheckFatalError(err)
			data, err := lib.Search(nil, db, "", 10)
			lib.CheckFatalError(err)
			if len(data) < 10 {
				fmt.Println("Importing existing shell history...")
				ctx := hctx.MakeContext()
				numImported, err := lib.ImportHistory(ctx, false, false)
				lib.CheckFatalError(err)
				if numImported > 0 {
					fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
