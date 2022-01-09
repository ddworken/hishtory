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
	case "init":
		shared.CheckFatalError(shared.Setup(os.Args))
	case "install":
		shared.CheckFatalError(shared.Setup(os.Args))
		shared.CheckFatalError(shared.Install())
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

	req, err := http.NewRequest("GET", getServerHostname()+"/api/v1/search", nil)
	shared.CheckFatalError(err)

	q := req.URL.Query()
	q.Add("query", query)
	q.Add("user_secret", userSecret)
	q.Add("limit", "25")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{}
	resp, err := client.Do(req)
	shared.CheckFatalError(err)
	defer resp.Body.Close()
	resp_body, err := ioutil.ReadAll(resp.Body)
	shared.CheckFatalError(err)
	if resp.Status != "200 OK" {
		shared.CheckFatalError(fmt.Errorf("search API returned invalid result. status=" + resp.Status))
	}

	var data []*shared.HistoryEntry
	err = json.Unmarshal(resp_body, &data)
	shared.CheckFatalError(err)
	shared.DisplayResults(data, true)
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
