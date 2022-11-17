package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Completely uninstall hiSHtory and remove your shell history",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		fmt.Printf("Are you sure you want to uninstall hiSHtory and delete all locally saved history data [y/N]")
		reader := bufio.NewReader(os.Stdin)
		resp, err := reader.ReadString('\n')
		lib.CheckFatalError(err)
		if strings.TrimSpace(resp) != "y" {
			fmt.Printf("Aborting uninstall per user response of %#v\n", strings.TrimSpace(resp))
			return
		}
		fmt.Printf("Do you have any feedback on why you're uninstallying hiSHtory? Type any feedback and then hit enter.\nFeedback: ")
		feedbackTxt, err := reader.ReadString('\n')
		lib.CheckFatalError(err)
		feedback := shared.Feedback{
			Date:     time.Now(),
			Feedback: feedbackTxt,
			UserId:   data.UserId(hctx.GetConf(ctx).UserSecret),
		}
		reqBody, err := json.Marshal(feedback)
		lib.CheckFatalError(err)
		_, _ = lib.ApiPost("/api/v1/feedback", "application/json", reqBody)
		lib.CheckFatalError(lib.Uninstall(ctx))
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

// TODO: maybe prompt users for feedback on why they're uninstalling?
