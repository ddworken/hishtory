package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
)

var GitCommit string = "Unknown"

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
		lib.CheckFatalError(lib.Setup(os.Args))
	case "install":
		lib.CheckFatalError(lib.Install())
	case "enable":
		lib.CheckFatalError(lib.Enable())
	case "disable":
		lib.CheckFatalError(lib.Disable())
	case "status":
		config, err := lib.GetConfig()
		lib.CheckFatalError(err)
		fmt.Print("Hishtory: e2e sync\nEnabled: ")
		fmt.Print(config.IsEnabled)
		fmt.Print("\nSecret Key: ")
		fmt.Print(config.UserSecret)
		fmt.Print("\nCommit Hash: ")
		fmt.Print(GitCommit)
		fmt.Print("\n")
	case "update":
		lib.CheckFatalError(lib.Update("https://hishtory.dev/binaries/hishtory-linux"))
	default:
		lib.CheckFatalError(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func retrieveAdditionalEntriesFromRemote(db *gorm.DB) error {
	config, err := lib.GetConfig()
	if err != nil {
		return err
	}
	resp, err := http.Get(lib.GetServerHostname() + "/api/v1/equery?device_id=" + config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to pull latest history entries from the backend: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to retrieve data from backend, status_code=%d", resp.StatusCode)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read latest history entries response body: %v", err)
	}
	var retrievedEntries []*shared.EncHistoryEntry
	err = json.Unmarshal(data, &retrievedEntries)
	if err != nil {
		return fmt.Errorf("failed to load JSON response: %v", err)
	}
	for _, entry := range retrievedEntries {
		decEntry, err := shared.DecryptHistoryEntry(config.UserSecret, *entry)
		if err != nil {
			return fmt.Errorf("failed to decrypt history entry from server: %v", err)
		}
		lib.AddToDbIfNew(db, decEntry)
	}
	return nil
}

func query(query string) {
	db, err := lib.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	lib.CheckFatalError(retrieveAdditionalEntriesFromRemote(db))
	lib.CheckFatalError(displayBannerIfSet())
	data, err := shared.Search(db, query, 25)
	lib.CheckFatalError(err)
	lib.DisplayResults(data, false)
}

func displayBannerIfSet() error {
	config, err := lib.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %v", err)
	}
	url := lib.GetServerHostname() + "/api/v1/banner?commit_hash=" + GitCommit + "&device_id=" + config.DeviceId + "&forced_banner=" + os.Getenv("FORCED_BANNER")
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to call /api/v1/banner: %v", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to call %s, status_code=%d", url, resp.StatusCode)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read /api/v1/banner response body: %v", err)
	}
	if len(data) > 0 {
		fmt.Printf(string(data))
	}
	return nil
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
	db, err := lib.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	result := db.Create(entry)
	lib.CheckFatalError(result.Error)

	// Persist it remotely
	encEntry, err := shared.EncryptHistoryEntry(config.UserSecret, *entry)
	lib.CheckFatalError(err)
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	lib.CheckFatalError(err)
	resp, err := http.Post(lib.GetServerHostname()+"/api/v1/esubmit", "application/json", bytes.NewBuffer(jsonValue))
	lib.CheckFatalError(err)
	if resp.StatusCode != 200 {
		lib.CheckFatalError(fmt.Errorf("failed to submit result to backend, status_code=%d", resp.StatusCode))
	}
}

func export() {
	db, err := lib.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	lib.CheckFatalError(retrieveAdditionalEntriesFromRemote(db))
	data, err := shared.Search(db, "", 0)
	lib.CheckFatalError(err)
	for i := len(data)-1; i >= 0; i-- {
		fmt.Println(data[i].Command)
	}
}
