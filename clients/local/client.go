package main

import (
	"os"

	"github.com/ddworken/hishtory/shared"
)

func main() {
	switch os.Args[1] {
	case "saveHistoryEntry":
		saveHistoryEntry()
	case "query":
		query()
	case "init":
		err := shared.Setup(os.Args)
		if err != nil {
			panic(err)
		}
	}
}

func getServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return "http://localhost:8080"
}

func query() {
	// TODO(ddworken)
	var data []*shared.HistoryEntry
	shared.DisplayResults(data)
}

func saveHistoryEntry() {
	entry, err := shared.BuildHistoryEntry(os.Args)
	if err != nil {
		panic(err)
	}

	err = shared.Persist(*entry)
	if err != nil {
		panic(err)
	}
}
