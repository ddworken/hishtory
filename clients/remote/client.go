package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
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
		shared.CheckFatalError(shared.Setup(os.Args))
	case "install":
		shared.CheckFatalError(shared.Install())
		// TODO: should be able to do hishtory install $secret
	case "enable":
		shared.CheckFatalError(shared.Enable())
	case "disable":
		shared.CheckFatalError(shared.Disable())
	case "status":
		config, err := shared.GetConfig()
		shared.CheckFatalError(err)
		fmt.Print("Hishtory: Online Mode\nEnabled: ")
		fmt.Print(config.IsEnabled)
		fmt.Print("\nSecret Key: " + config.UserSecret + "\n")
		fmt.Print("Remote Server: " + getServerHostname() + "\n")
	case "update":
		shared.CheckFatalError(shared.Update("https://hishtory.dev/hishtory-online"))
	default:
		shared.CheckFatalError(fmt.Errorf("unknown command: %s", os.Args[1]))
	}
}

func getServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return "https://api.hishtory.dev"
}

func query(query string) {
	data, err := doQuery(query)
	shared.CheckFatalError(err)
	shared.DisplayResults(data, true)
}

func doQuery(query string) ([]*shared.HistoryEntry, error) {
	userSecret, err := shared.GetUserSecret()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", getServerHostname()+"/api/v1/search", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("query", query)
	q.Add("user_secret", userSecret)
	q.Add("limit", "25")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	resp_body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.Status != "200 OK" {
		return nil, fmt.Errorf("search API returned invalid result. status=" + resp.Status)
	}

	var data []*shared.HistoryEntry
	err = json.Unmarshal(resp_body, &data)
	return data, err
}

func saveHistoryEntry() {
	isEnabled, err := shared.IsEnabled()
	shared.CheckFatalError(err)
	if !isEnabled {
		return
	}
	entry, err := shared.BuildHistoryEntry(os.Args)
	shared.CheckFatalError(err)
	err = send(*entry)
	shared.CheckFatalError(err)
}

func send(entry shared.HistoryEntry) error {
	jsonValue, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal HistoryEntry as json: %v", err)
	}

	_, err = http.Post(getServerHostname()+"/api/v1/submit", "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		return fmt.Errorf("failed to send HistoryEntry to api: %v", err)
	}
	return nil
}

func export() {
	// TODO(ddworken)
}
