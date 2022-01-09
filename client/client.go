package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/rodaine/table"

	"github.com/ddworken/hishtory/shared"
)

func main() {
	switch os.Args[1] {
	case "upload":
		upload()
	case "query":
		query()
	case "init":
		setup()
	}
}

func setup() {
	userSecret := uuid.Must(uuid.NewRandom()).String()
	if len(os.Args) > 2 && os.Args[2] != "" {
		userSecret = os.Args[2]
	}
	fmt.Println("Setting secret hishtory key to " + string(userSecret))

	homedir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(path.Join(homedir, ".hishtory.secret"), []byte(userSecret), 0400)
	if err != nil {
		panic(err)
	}
}

func getUserSecret() (string, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to read secret hishtory key: %v", err)
	}
	secret, err := os.ReadFile(path.Join(homedir, ".hishtory.secret"))
	if err != nil {
		return "", fmt.Errorf("failed to read secret hishtory key: %v", err)
	}
	return string(secret), nil
}

func getServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return "http://localhost:8080"
}

func query() {
	userSecret, err := getUserSecret()
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
	display(data)
}

func display(results []*shared.HistoryEntry) {
	headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()
	tbl := table.New("Hostname", "CWD", "Timestamp", "Exit Code", "Command")
	tbl.WithHeaderFormatter(headerFmt)

	for _, result := range results {
		tbl.AddRow(result.Hostname, result.CurrentWorkingDirectory, result.EndTime.Format("Jan 2 2006 15:04:05 MST"), result.ExitCode, result.Command)
	}

	tbl.Print()
}

func upload() {
	var entry shared.HistoryEntry

	// exitCode
	exitCode, err := strconv.Atoi(os.Args[2])
	if err != nil {
		panic(err)
	}
	entry.ExitCode = exitCode

	// user
	user, err := user.Current()
	if err != nil {
		panic(err)
	}
	entry.LocalUsername = user.Username

	// cwd
	cwd, err := getCwd()
	if err != nil {
		panic(err)
	}
	entry.CurrentWorkingDirectory = cwd

	// TODO(ddworken): start time

	// end time
	entry.EndTime = time.Now()

	// command
	cmd, err := getLastCommand(os.Args[3])
	if err != nil {
		panic(err)
	}
	entry.Command = cmd

	// hostname
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	entry.Hostname = hostname

	// user secret
	userSecret, err := getUserSecret()
	if err != nil {
		panic(err)
	}
	entry.UserSecret = userSecret

	err = send(entry)
	if err != nil {
		panic(err)
	}
}

func getCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get cwd for last command: %v", err)
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user's home directory: %v", err)
	}
	if strings.HasPrefix(cwd, homedir) {
		return strings.Replace(cwd, homedir, "~", 1), nil
	}
	return cwd, nil
}

func getLastCommand(history string) (string, error) {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(history), " ", 2)[1]), nil
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
