package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/client/webui"

	"github.com/spf13/cobra"
)

var (
	disableAuth *bool
	forceCreds  *string
	port        *int
)

var webUiCmd = &cobra.Command{
	Use:   "start-web-ui",
	Short: "Serve a basic web UI for interacting with your shell history",
	Run: func(cmd *cobra.Command, args []string) {
		overridenUsername := ""
		overridenPassword := ""
		if *forceCreds != "" {
			if strings.Contains(*forceCreds, ":") {
				splitCreds := strings.SplitN(*forceCreds, ":", 2)
				overridenUsername = splitCreds[0]
				overridenPassword = splitCreds[1]
			} else {
				lib.CheckFatalError(fmt.Errorf("--force-creds=%#v doesn't contain a colon to delimit username and password", *forceCreds))
			}
		}
		if *disableAuth && *forceCreds != "" {
			lib.CheckFatalError(fmt.Errorf("cannot specify both --disable-auth and --force-creds"))
		}
		lib.CheckFatalError(webui.StartWebUiServer(hctx.MakeContext(), *port, *disableAuth, overridenUsername, overridenPassword))
		os.Exit(1)
	},
}

func init() {
	rootCmd.AddCommand(webUiCmd)
	disableAuth = webUiCmd.Flags().Bool("disable-auth", false, "Disable authentication for the Web UI (Warning: This means your entire shell history will be accessible from the local web server)")
	forceCreds = webUiCmd.Flags().String("force-creds", "", "Specify the credentials to use for basic auth in the form `user:password`")
	port = webUiCmd.Flags().Int("port", 8000, "The port for the web server to listen on")
}
