package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ddworken/hishtory/shared"
)

func main() {
	if len(os.Args) == 1 {
		fmt.Println("Must specify a command! Do you mean `hishtory query`?")
		return
	}
	switch os.Args[1] {
	case "saveHistoryEntry":
		saveHistoryEntry()
	case "query":
		query(strings.Join(os.Args[2:], " "))
	case "init":
		shared.CheckFatalError(shared.Setup(os.Args))
	case "install":
		shared.CheckFatalError(shared.Install())
		shared.CheckFatalError(shared.Setup(os.Args))
	case "enable":
		shared.CheckFatalError(shared.Enable())
	case "disable":
		shared.CheckFatalError(shared.Disable())
	default:
		shared.CheckFatalError(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func getServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return "http://localhost:8080"
}

func query(query string) {
	userSecret, err := shared.GetUserSecret()
	shared.CheckFatalError(err)
	db, err := shared.OpenDB()
	shared.CheckFatalError(err)
	data, err := shared.Search(db, userSecret, query, 25)
	shared.CheckFatalError(err)
	shared.DisplayResults(data, false)
}

func saveHistoryEntry() {
	isEnabled, err := shared.IsEnabled()
	shared.CheckFatalError(err)
	if !isEnabled {
		return
	}
	entry, err := shared.BuildHistoryEntry(os.Args)
	shared.CheckFatalError(err)
	err = shared.Persist(*entry)
	shared.CheckFatalError(err)
}
