package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/client/lib"
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
		lib.CheckFatalError(lib.Setup( os.Args))
	case "install":
		lib.CheckFatalError(lib.Install())
	case "enable":
		lib.CheckFatalError(lib.Enable())
	case "disable":
		lib.CheckFatalError(lib.Disable())
	case "status":
		config, err := lib.GetConfig()
		lib.CheckFatalError(err)
		fmt.Print("Hishtory: Offline Mode\nEnabled: ")
		fmt.Print(config.IsEnabled)
		fmt.Print("\n")
	case "update":
		lib.CheckFatalError(lib.Update("https://hishtory.dev/binaries/hishtory-linux"))
	default:
		lib.CheckFatalError(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func query(query string) {
	db, err := shared.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	data, err := shared.Search(db, query, 25)
	lib.CheckFatalError(err)
	lib.DisplayResults(data, false)
}

func saveHistoryEntry() {
	config, err := lib.GetConfig()
	lib.CheckFatalError(err)
	if !config.IsEnabled {
		return
	}
	entry, err := lib.BuildHistoryEntry(os.Args)
	lib.CheckFatalError(err)

	// Persist it locally
	db, err := shared.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	result := db.Create(entry)
	lib.CheckFatalError(result.Error)

	// Persist it remotely
	encEntry, err := shared.EncryptHistoryEntry(config.UserSecret, *entry)
	lib.CheckFatalError(err)
	jsonValue, err := json.Marshal(encEntry)
	lib.CheckFatalError(err)
	_, err = http.Post(lib.GetServerHostname()+"/api/v1/esubmit", "application/json", bytes.NewBuffer(jsonValue))
	lib.CheckFatalError(err)
}

func export() {
	db, err := shared.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	data, err := shared.Search(db, "", 0)
	lib.CheckFatalError(err)
	for _, entry := range data {
		fmt.Println(entry)
	}
}
