package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
	case "export":
		export()
	case "init":
		shared.CheckFatalError(shared.Setup( os.Args))
		// TODO: Call ebootstrap here
	case "install":
		shared.CheckFatalError(shared.Install())
		// TODO: Call ebootstrap here
	case "enable":
		shared.CheckFatalError(shared.Enable())
	case "disable":
		shared.CheckFatalError(shared.Disable())
	case "status":
		config, err := shared.GetConfig()
		shared.CheckFatalError(err)
		fmt.Print("Hishtory: Offline Mode\nEnabled: ")
		fmt.Print(config.IsEnabled)
		fmt.Print("\n")
	case "update":
		shared.CheckFatalError(shared.Update("https://hishtory.dev/hishtory-offline"))
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
	db, err := shared.OpenLocalSqliteDb()
	shared.CheckFatalError(err)
	data, err := shared.Search(db, userSecret, query, 25)
	shared.CheckFatalError(err)
	shared.DisplayResults(data, false)
}

func saveHistoryEntry() {
	config, err := shared.GetConfig()
	shared.CheckFatalError(err)
	if !config.IsEnabled {
		return
	}
	entry, err := shared.BuildHistoryEntry(os.Args)
	shared.CheckFatalError(err)

	// Persist it locally
	db, err := shared.OpenLocalSqliteDb()
	shared.CheckFatalError(err)
	result := db.Create(entry)
	shared.CheckFatalError(result.Error)

	// Persist it remotely
	encEntry, err := shared.EncryptHistoryEntry(config.UserSecret, *entry)
	shared.CheckFatalError(err)
	jsonValue, err := json.Marshal(encEntry)
	shared.CheckFatalError(err)
	_, err = http.Post(getServerHostname()+"/api/v1/esubmit", "application/json", bytes.NewBuffer(jsonValue))
	shared.CheckFatalError(err)
}

func export() {
	userSecret, err := shared.GetUserSecret()
	shared.CheckFatalError(err)
	db, err := shared.OpenLocalSqliteDb()
	shared.CheckFatalError(err)
	data, err := shared.Search(db, userSecret, "", 0)
	shared.CheckFatalError(err)
	for _, entry := range data {
		fmt.Println(entry)
	}
}
