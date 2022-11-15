package main

import (
	"github.com/ddworken/hishtory/client/cmd"
)

func main() {
	cmd.Execute()
}

// TODO(feature): Add a session_id column that corresponds to the shell session the command was run in

/*
Remaining things:
* Support exclusions in searches
* Figure out how to hide certain things from the help doc
* Figure out how to reorder the docs
* Acutally migrate saveHistoryEntry to cobra
*/
