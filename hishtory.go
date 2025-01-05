package main

import (
	"github.com/ddworken/hishtory/client/cmd"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/lib"
)

func main() {
	lib.CheckFatalError(data.ValidateHishtoryPath())
	cmd.Execute()
}

// TODO(feature): Add a session_id column that corresponds to the shell session the command was run in
// TODO(feature): Add a shell column that contains the shell name
