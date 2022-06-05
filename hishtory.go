package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/ddworken/hishtory/client/data"
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
	case "version":
		fallthrough
	case "status":
		config, err := lib.GetConfig()
		lib.CheckFatalError(err)
		fmt.Printf("Hishtory: v0.%s\nEnabled: %v\n", lib.Version, config.IsEnabled)
		fmt.Printf("Secret Key: %s\n", config.UserSecret)
		if len(os.Args) == 3 && os.Args[2] == "-v" {
			fmt.Printf("User ID: %s\n", data.UserId(config.UserSecret))
			fmt.Printf("Device ID: %s\n", config.DeviceId)
			printDumpStatus(config)
		}
		fmt.Printf("Commit Hash: %s\n", GitCommit)
	case "update":
		lib.CheckFatalError(lib.Update())
	default:
		lib.CheckFatalError(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func printDumpStatus(config lib.ClientConfig) {
	dumpRequests, err := getDumpRequests(config)
	lib.CheckFatalError(err)
	fmt.Printf("Dump Requests: ")
	for _, d := range dumpRequests {
		fmt.Printf("%#v, ", *d)
	}
	fmt.Print("\n")
}

func getDumpRequests(config lib.ClientConfig) ([]*shared.DumpRequest, error) {
	resp, err := lib.ApiGet("/api/v1/get-dump-requests?user_id=" + data.UserId(config.UserSecret) + "&device_id=" + config.DeviceId)
	if err != nil {
		return nil, err
	}
	var dumpRequests []*shared.DumpRequest
	err = json.Unmarshal(resp, &dumpRequests)
	return dumpRequests, err
}

func retrieveAdditionalEntriesFromRemote(db *gorm.DB) error {
	config, err := lib.GetConfig()
	if err != nil {
		return err
	}
	respBody, err := lib.ApiGet("/api/v1/query?device_id=" + config.DeviceId + "&user_id=" + data.UserId(config.UserSecret))
	if err != nil {
		return err
	}
	var retrievedEntries []*shared.EncHistoryEntry
	err = json.Unmarshal(respBody, &retrievedEntries)
	if err != nil {
		return fmt.Errorf("failed to load JSON response: %v", err)
	}
	for _, entry := range retrievedEntries {
		decEntry, err := data.DecryptHistoryEntry(config.UserSecret, *entry)
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
	err = retrieveAdditionalEntriesFromRemote(db)
	if err != nil {
		if lib.IsOfflineError(err) {
			fmt.Println("Warning: hishtory is offline so this may be missing recent results from your other machines!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	lib.CheckFatalError(displayBannerIfSet())
	data, err := data.Search(db, query, 25)
	lib.CheckFatalError(err)
	lib.DisplayResults(data)
}

func displayBannerIfSet() error {
	config, err := lib.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %v", err)
	}
	url := "/api/v1/banner?commit_hash=" + GitCommit + "&device_id=" + config.DeviceId + "&forced_banner=" + os.Getenv("FORCED_BANNER")
	respBody, err := lib.ApiGet(url)
	if err != nil {
		return err
	}
	if len(respBody) > 0 {
		fmt.Println(string(respBody))
	}
	return nil
}

func saveHistoryEntry() {
	config, err := lib.GetConfig()
	if err != nil {
		log.Fatalf("hishtory cannot save an entry because the hishtory config file does not exist, try running `hishtory init` (err=%v)", err)
	}
	if !config.IsEnabled {
		lib.GetLogger().Printf("Skipping saving a history entry because hishtory is disabled\n")
		return
	}
	entry, err := lib.BuildHistoryEntry(os.Args)
	lib.CheckFatalError(err)
	if entry == nil {
		lib.GetLogger().Printf("Skipping saving a history entry because we failed to build a history entry (was the command prefixed with a space?)\n")
		return
	}

	// Persist it locally
	db, err := lib.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	err = lib.ReliableDbCreate(db, entry)
	lib.CheckFatalError(err)

	// Persist it remotely
	encEntry, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
	lib.CheckFatalError(err)
	encEntry.DeviceId = config.DeviceId
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	lib.CheckFatalError(err)
	_, err = lib.ApiPost("/api/v1/submit", "application/json", jsonValue)
	if err != nil {
		if lib.IsOfflineError(err) {
			// TODO: Somehow handle this and don't completely lose it
			lib.GetLogger().Printf("Failed to remotely persist hishtory entry because the device is offline!")
		} else {
			lib.CheckFatalError(err)
		}
	}

	// Check if there is a pending dump request and reply to it if so
	dumpRequests, err := getDumpRequests(config)
	if err != nil {
		if lib.IsOfflineError(err) {
			// It is fine to just ignore this, the next command will retry the API and eventually we will respond to any pending dump requests
			dumpRequests = []*shared.DumpRequest{}
			lib.GetLogger().Printf("Failed to check for dump requests because the device is offline!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	if len(dumpRequests) > 0 {
		lib.CheckFatalError(retrieveAdditionalEntriesFromRemote(db))
		entries, err := data.Search(db, "", 0)
		lib.CheckFatalError(err)
		var encEntries []*shared.EncHistoryEntry
		for _, entry := range entries {
			enc, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
			lib.CheckFatalError(err)
			encEntries = append(encEntries, &enc)
		}
		reqBody, err := json.Marshal(encEntries)
		lib.CheckFatalError(err)
		for _, dumpRequest := range dumpRequests {
			_, err := lib.ApiPost("/api/v1/submit-dump?user_id="+dumpRequest.UserId+"&requesting_device_id="+dumpRequest.RequestingDeviceId+"&source_device_id="+config.DeviceId, "application/json", reqBody)
			lib.CheckFatalError(err)
		}
	}
}

func export() {
	db, err := lib.OpenLocalSqliteDb()
	lib.CheckFatalError(err)
	err = retrieveAdditionalEntriesFromRemote(db)
	if err != nil {
		if lib.IsOfflineError(err) {
			fmt.Println("Warning: hishtory is offline so this may be missing recent results from your other machines!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	data, err := data.Search(db, "", 0)
	lib.CheckFatalError(err)
	for i := len(data) - 1; i >= 0; i-- {
		fmt.Println(data[i].Command)
	}
}
