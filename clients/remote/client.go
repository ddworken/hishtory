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
	userSecret, err := shared.GetUserSecret()
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest("GET", getServerHostname()+"/api/v1/search", nil)
	if err != nil {
		panic(err)
	}

	q := req.URL.Query()
	q.Add("query", strings.Join(os.Args[2:], " "))
	q.Add("user_secret", userSecret)
	q.Add("limit", "25")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	resp_body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if resp.Status != "200 OK" {
		panic("search API returned invalid result. status=" + resp.Status)
	}

	var data []*shared.HistoryEntry
	err = json.Unmarshal(resp_body, &data)
	if err != nil {
		panic(err)
	}
	shared.DisplayResults(data)
}

func saveHistoryEntry() {
	entry, err := shared.BuildHistoryEntry(os.Args)
	if err != nil {
		panic(err)
	}

	err = send(*entry)
	if err != nil {
		panic(err)
	}
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
