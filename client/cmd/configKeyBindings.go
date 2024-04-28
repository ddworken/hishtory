package cmd

import (
	"fmt"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/spf13/cobra"
)

var getKeyBindingsCmd = &cobra.Command{
	Use:   "key-bindings",
	Short: "Get the currently configured key bindings for the TUI",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Println("up: \t\t\t" + strings.Join(config.KeyBindings.Up, " "))
		fmt.Println("down: \t\t\t" + strings.Join(config.KeyBindings.Down, " "))
		fmt.Println("page-up: \t\t" + strings.Join(config.KeyBindings.PageUp, " "))
		fmt.Println("page-down: \t\t" + strings.Join(config.KeyBindings.PageDown, " "))
		fmt.Println("select-entry: \t\t" + strings.Join(config.KeyBindings.SelectEntry, " "))
		fmt.Println("select-entry-and-cd: \t" + strings.Join(config.KeyBindings.SelectEntryAndChangeDir, " "))
		fmt.Println("left: \t\t\t" + strings.Join(config.KeyBindings.Left, " "))
		fmt.Println("right: \t\t\t" + strings.Join(config.KeyBindings.Right, " "))
		fmt.Println("table-left: \t\t" + strings.Join(config.KeyBindings.TableLeft, " "))
		fmt.Println("table-right: \t\t" + strings.Join(config.KeyBindings.TableRight, " "))
		fmt.Println("delete-entry: \t\t" + strings.Join(config.KeyBindings.DeleteEntry, " "))
		fmt.Println("help: \t\t\t" + strings.Join(config.KeyBindings.Help, " "))
		fmt.Println("quit: \t\t\t" + strings.Join(config.KeyBindings.Quit, " "))
		fmt.Println("jump-start-of-input: \t" + strings.Join(config.KeyBindings.JumpStartOfInput, " "))
		fmt.Println("jump-end-of-input: \t" + strings.Join(config.KeyBindings.JumpEndOfInput, " "))
		fmt.Println("word-left: \t\t" + strings.Join(config.KeyBindings.WordLeft, " "))
		fmt.Println("word-right: \t\t" + strings.Join(config.KeyBindings.WordRight, " "))
	},
}

var setKeyBindingsCmd = &cobra.Command{
	Use:   "key-bindings",
	Short: "Set custom key bindings for the TUI",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		switch args[0] {
		case "up":
			config.KeyBindings.Up = args[1:]
		case "down":
			config.KeyBindings.Down = args[1:]
		case "page-up":
			config.KeyBindings.PageUp = args[1:]
		case "page-down":
			config.KeyBindings.PageDown = args[1:]
		case "select-entry":
			config.KeyBindings.SelectEntry = args[1:]
		case "select-entry-and-cd":
			config.KeyBindings.SelectEntryAndChangeDir = args[1:]
		case "left":
			config.KeyBindings.Left = args[1:]
		case "right":
			config.KeyBindings.Right = args[1:]
		case "table-left":
			config.KeyBindings.TableLeft = args[1:]
		case "table-right":
			config.KeyBindings.TableRight = args[1:]
		case "delete-entry":
			config.KeyBindings.DeleteEntry = args[1:]
		case "help":
			config.KeyBindings.Help = args[1:]
		case "quit":
			config.KeyBindings.Quit = args[1:]
		case "jump-start-of-input":
			config.KeyBindings.JumpStartOfInput = args[1:]
		case "jump-end-of-input":
			config.KeyBindings.JumpEndOfInput = args[1:]
		case "word-left":
			config.KeyBindings.WordLeft = args[1:]
		case "word-right":
			config.KeyBindings.WordRight = args[1:]
		}
		lib.CheckFatalError(hctx.SetConfig(config))
	},
}

func init() {
	configGetCmd.AddCommand(getKeyBindingsCmd)
	configSetCmd.AddCommand(setKeyBindingsCmd)
}
