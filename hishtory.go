package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
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
		ctx := hctx.MakeContext()
		lib.CheckFatalError(maybeUploadSkippedHistoryEntries(ctx))
		saveHistoryEntry(ctx)
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
	case "query":
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		query(ctx, strings.Join(os.Args[2:], " "))
	case "tquery":
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		lib.CheckFatalError(lib.TuiQuery(ctx))
	case "export":
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		export(ctx, strings.Join(os.Args[2:], " "))
	case "redact":
		fallthrough
	case "delete":
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.RetrieveAdditionalEntriesFromRemote(ctx))
		lib.CheckFatalError(lib.ProcessDeletionRequests(ctx))
		query := strings.Join(os.Args[2:], " ")
		force := false
		if os.Args[2] == "--force" {
			query = strings.Join(os.Args[3:], " ")
			force = true
		}
		lib.CheckFatalError(lib.Redact(ctx, query, force))
	case "init":
		db, err := hctx.OpenLocalSqliteDb()
		lib.CheckFatalError(err)
		data, err := data.Search(db, "", 10)
		lib.CheckFatalError(err)
		if len(data) > 0 {
			fmt.Printf("Your current hishtory profile has saved history entries, are you sure you want to run `init` and reset? [y/N]")
			reader := bufio.NewReader(os.Stdin)
			resp, err := reader.ReadString('\n')
			lib.CheckFatalError(err)
			if strings.TrimSpace(resp) != "y" {
				fmt.Printf("Aborting init per user response of %#v\n", strings.TrimSpace(resp))
				return
			}
		}
		lib.CheckFatalError(lib.Setup(os.Args))
	case "install":
		lib.CheckFatalError(lib.Install())
		if os.Getenv("HISHTORY_TEST") == "" {
			ctx := hctx.MakeContext()
			numImported, err := lib.ImportHistory(ctx)
			lib.CheckFatalError(err)
			if numImported > 0 {
				fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
			}
		}
	case "import":
		ctx := hctx.MakeContext()
		numImported, err := lib.ImportHistory(ctx)
		lib.CheckFatalError(err)
		if numImported > 0 {
			fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
		}
	case "enable":
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.Enable(ctx))
	case "disable":
		ctx := hctx.MakeContext()
		lib.CheckFatalError(lib.Disable(ctx))
	case "version":
		fallthrough
	case "status":
		ctx := hctx.MakeContext()
		config := hctx.GetConf(ctx)
		fmt.Printf("hiSHtory: v0.%s\nEnabled: %v\n", lib.Version, config.IsEnabled)
		fmt.Printf("Secret Key: %s\n", config.UserSecret)
		if len(os.Args) == 3 && os.Args[2] == "-v" {
			fmt.Printf("User ID: %s\n", data.UserId(config.UserSecret))
			fmt.Printf("Device ID: %s\n", config.DeviceId)
			printDumpStatus(config)
		}
		fmt.Printf("Commit Hash: %s\n", GitCommit)
	case "update":
		err := lib.Update(hctx.MakeContext())
		if err != nil {
			log.Fatalf("Failed to update hishtory: %v", err)
		}
	case "reupload":
		// Purposefully undocumented since this command is generally not necessary to run
		lib.CheckFatalError(lib.Reupload(hctx.MakeContext()))
	case "-h":
		fallthrough
	case "help":
		fmt.Print(`hiSHtory: Better shell history

Supported commands:
    'hishtory query': Query for matching commands and display them in a table. Examples:
		'hishtory query apt-get'  			# Find shell commands containing 'apt-get'
		'hishtory query apt-get install'  	# Find shell commands containing 'apt-get' and 'install'
		'hishtory query curl cwd:/tmp/'  	# Find shell commands containing 'curl' run in '/tmp/'
		'hishtory query curl user:david'	# Find shell commands containing 'curl' run by 'david'
		'hishtory query curl host:x1'		# Find shell commands containing 'curl' run on 'x1'
		'hishtory query exit_code:1'		# Find shell commands that exited with status code 1
		'hishtory query before:2022-02-01'	# Find shell commands run before 2022-02-01
	'hishtory export': Query for matching commands and display them in list without any other 
		metadata. Supports the same query format as 'hishtory query'. 
	'hishtory redact': Query for matching commands and remove them from your shell history (on the
		current machine and on all remote machines). Supports the same query format as 'hishtory query'.
	'hishtory update': Securely update hishtory to the latest version. 
	'hishtory disable': Stop recording shell commands 
	'hishtory enable': Start recording shell commands 
	'hishtory status': View status info including the secret key which is needed to sync shell
		history from another machine. 
	'hishtory init': Set the secret key to enable syncing shell commands from another 
		machine with a matching secret key. 
	'hishtory help': View this help page
`)
	default:
		lib.CheckFatalError(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func printDumpStatus(config hctx.ClientConfig) {
	dumpRequests, err := getDumpRequests(config)
	lib.CheckFatalError(err)
	fmt.Printf("Dump Requests: ")
	for _, d := range dumpRequests {
		fmt.Printf("%#v, ", *d)
	}
	fmt.Print("\n")
}

func getDumpRequests(config hctx.ClientConfig) ([]*shared.DumpRequest, error) {
	resp, err := lib.ApiGet("/api/v1/get-dump-requests?user_id=" + data.UserId(config.UserSecret) + "&device_id=" + config.DeviceId)
	if lib.IsOfflineError(err) {
		return []*shared.DumpRequest{}, nil
	}
	if err != nil {
		return nil, err
	}
	var dumpRequests []*shared.DumpRequest
	err = json.Unmarshal(resp, &dumpRequests)
	return dumpRequests, err
}

func query(ctx *context.Context, query string) {
	db := hctx.GetDb(ctx)
	err := lib.RetrieveAdditionalEntriesFromRemote(ctx)
	if err != nil {
		if lib.IsOfflineError(err) {
			fmt.Println("Warning: hishtory is offline so this may be missing recent results from your other machines!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	lib.CheckFatalError(displayBannerIfSet(ctx))
	data, err := data.Search(db, query, 25)
	lib.CheckFatalError(err)
	lib.DisplayResults(data)
}

func displayBannerIfSet(ctx *context.Context) error {
	respBody, err := lib.GetBanner(ctx, GitCommit)
	if lib.IsOfflineError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(respBody) > 0 {
		fmt.Println(string(respBody))
	}
	return nil
}

func maybeUploadSkippedHistoryEntries(ctx *context.Context) error {
	config := hctx.GetConf(ctx)
	if !config.HaveMissedUploads {
		return nil
	}

	// Upload the missing entries
	db := hctx.GetDb(ctx)
	query := fmt.Sprintf("after:%s", time.Unix(config.MissedUploadTimestamp, 0).Format("2006-01-02"))
	entries, err := data.Search(db, query, 0)
	if err != nil {
		return fmt.Errorf("failed to retrieve history entries that haven't been uploaded yet: %v", err)
	}
	hctx.GetLogger().Printf("Uploading %d history entries that previously failed to upload (query=%#v)\n", len(entries), query)
	jsonValue, err := lib.EncryptAndMarshal(config, entries)
	if err != nil {
		return err
	}
	_, err = lib.ApiPost("/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
	if err != nil {
		// Failed to upload the history entry, so we must still be offline. So just return nil and we'll try again later.
		return nil
	}

	// Mark down that we persisted it
	config.HaveMissedUploads = false
	config.MissedUploadTimestamp = 0
	err = hctx.SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to mark a history entry as uploaded: %v", err)
	}
	return nil
}

func saveHistoryEntry(ctx *context.Context) {
	config := hctx.GetConf(ctx)
	if !config.IsEnabled {
		hctx.GetLogger().Printf("Skipping saving a history entry because hishtory is disabled\n")
		return
	}
	entry, err := lib.BuildHistoryEntry(ctx, os.Args)
	lib.CheckFatalError(err)
	if entry == nil {
		hctx.GetLogger().Printf("Skipping saving a history entry because we failed to build a history entry (was the command prefixed with a space?)\n")
		return
	}

	// Persist it locally
	db := hctx.GetDb(ctx)
	err = lib.ReliableDbCreate(db, *entry)
	lib.CheckFatalError(err)

	// Persist it remotely
	jsonValue, err := lib.EncryptAndMarshal(config, []*data.HistoryEntry{entry})
	lib.CheckFatalError(err)
	_, err = lib.ApiPost("/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
	if err != nil {
		if lib.IsOfflineError(err) {
			hctx.GetLogger().Printf("Failed to remotely persist hishtory entry because the device is offline!")
			if !config.HaveMissedUploads {
				config.HaveMissedUploads = true
				config.MissedUploadTimestamp = time.Now().Unix()
				lib.CheckFatalError(hctx.SetConfig(config))
			}
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
			hctx.GetLogger().Printf("Failed to check for dump requests because the device is offline!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	if len(dumpRequests) > 0 {
		lib.CheckFatalError(lib.RetrieveAdditionalEntriesFromRemote(ctx))
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

func export(ctx *context.Context, query string) {
	db := hctx.GetDb(ctx)
	err := lib.RetrieveAdditionalEntriesFromRemote(ctx)
	if err != nil {
		if lib.IsOfflineError(err) {
			fmt.Println("Warning: hishtory is offline so this may be missing recent results from your other machines!")
		} else {
			lib.CheckFatalError(err)
		}
	}
	data, err := data.Search(db, query, 0)
	lib.CheckFatalError(err)
	for i := len(data) - 1; i >= 0; i-- {
		fmt.Println(data[i].Command)
	}
}

// TODO(feature): Add a session_id column that corresponds to the shell session the command was run in
