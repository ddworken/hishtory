package lib

import (
	_ "embed"

	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/rodaine/table"

	"github.com/ddworken/hishtory/shared"
)

var (
	//go:embed config.sh
	CONFIG_SH_CONTENTS string
)

func getCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get cwd for last command: %v", err)
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user's home directory: %v", err)
	}
	if cwd == homedir {
		return "~/", nil
	}
	if strings.HasPrefix(cwd, homedir) {
		return strings.Replace(cwd, homedir, "~", 1), nil
	}
	return cwd, nil
}

func BuildHistoryEntry(args []string) (*shared.HistoryEntry, error) {
	var entry shared.HistoryEntry

	// exitCode
	exitCode, err := strconv.Atoi(args[2])
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.ExitCode = exitCode

	// user
	user, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.LocalUsername = user.Username

	// cwd
	cwd, err := getCwd()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.CurrentWorkingDirectory = cwd

	// start time
	nanos, err := strconv.ParseInt(args[4], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse start time %s as int: %v", args[4], err)
	}
	entry.StartTime = time.Unix(0, nanos)

	// end time
	entry.EndTime = time.Now()

	// command
	cmd, err := getLastCommand(args[3])
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.Command = cmd

	// hostname
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.Hostname = hostname

	return &entry, nil
}

func getLastCommand(history string) (string, error) {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(history), " ", 2)[1]), nil
}

func GetUserSecret() (string, error) {
	config, err := GetConfig()
	if err != nil {
		return "", err
	}
	return config.UserSecret, nil
}

func Setup(args []string) error {
	userSecret := uuid.Must(uuid.NewRandom()).String()
	if len(args) > 2 && args[2] != "" {
		userSecret = args[2]
	}
	fmt.Println("Setting secret hishtory key to " + string(userSecret))

	var config ClientConfig
	config.UserSecret = userSecret
	config.IsEnabled = true
	config.DeviceId = uuid.Must(uuid.NewRandom()).String()

	err := SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to persist config to disk: %v", err)
	}

	_, err = http.Get(GetServerHostname() + "/api/v1/eregister?user_id=" + shared.UserId(userSecret) + "&device_id=" + config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to register device with backend: %v", err)
	}

	resp, err := http.Get(GetServerHostname() + "/api/v1/ebootstrap?user_id=" + shared.UserId(userSecret) + "&device_id=" + config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to bootstrap device from the backend: %v", err)
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read bootstrap response body: %v", err)
	}
	var retrievedEntries []*shared.EncHistoryEntry
	err = json.Unmarshal(data, &retrievedEntries)
	if err != nil {
		return fmt.Errorf("failed to load JSON response: %v", err)
	}
	db, err := shared.OpenLocalSqliteDb()
	if err != nil {
		return fmt.Errorf("failed to open DB: %v", err)
	}
	for _, entry := range retrievedEntries {
		decEntry, err := shared.DecryptHistoryEntry(userSecret, *entry)
		if err != nil {
			return fmt.Errorf("failed to decrypt history entry from server: %v", err)
		}
		AddToDbIfNew(db, decEntry)
	}

	return nil
}

func AddToDbIfNew(db *gorm.DB, entry shared.HistoryEntry) {
	tx := db.Where("local_username = ?", entry.LocalUsername)
	tx = tx.Where("hostname = ?", entry.Hostname)
	tx = tx.Where("command = ?", entry.Command)
	tx = tx.Where("current_working_directory = ?", entry.CurrentWorkingDirectory)
	tx = tx.Where("exit_code = ?", entry.ExitCode)
	tx = tx.Where("start_time = ?", entry.StartTime)
	tx = tx.Where("end_time = ?", entry.EndTime)
	var results []shared.HistoryEntry
	tx.Limit(1).Find(&results)
	if len(results) == 0 {
		db.Create(entry)
	}
}

func DisplayResults(results []*shared.HistoryEntry, displayHostname bool) {
	headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()
	tbl := table.New("CWD", "Timestamp", "Runtime", "Exit Code", "Command")
	if displayHostname {
		tbl = table.New("Hostname", "CWD", "Timestamp", "Runtime", "Exit Code", "Command")
	}
	tbl.WithHeaderFormatter(headerFmt)

	for _, result := range results {
		timestamp := result.StartTime.Format("Jan 2 2006 15:04:05 MST")
		duration := result.EndTime.Sub(result.StartTime).Round(time.Millisecond).String()
		if displayHostname {
			tbl.AddRow(result.Hostname, result.CurrentWorkingDirectory, timestamp, duration, result.ExitCode, result.Command)
		} else {
			tbl.AddRow(result.CurrentWorkingDirectory, timestamp, duration, result.ExitCode, result.Command)
		}
	}

	tbl.Print()
}

type ClientConfig struct {
	UserSecret string `json:"user_secret"`
	IsEnabled  bool   `json:"is_enabled"`
	DeviceId   string `json:"device_id"`
}

func GetConfig() (ClientConfig, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to retrieve homedir: %v", err)
	}
	data, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to read config file: %v", err)
	}
	var config ClientConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func SetConfig(config ClientConfig) error {
	serializedConfig, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %v", err)
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to retrieve homedir: %v", err)
	}
	clientDir := path.Join(homedir, shared.HISHTORY_PATH)
	err = os.MkdirAll(clientDir, 0744)
	if err != nil {
		return fmt.Errorf("failed to create ~/.hishtory/ folder: %v", err)
	}
	err = os.WriteFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH), serializedConfig, 0600)
	if err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}
	return nil
}

func IsEnabled() (bool, error) {
	config, err := GetConfig()
	if err != nil {
		return false, err
	}
	return config.IsEnabled, nil
}

func Enable() error {
	config, err := GetConfig()
	if err != nil {
		return err
	}
	config.IsEnabled = true
	return SetConfig(config)
}

func Disable() error {
	config, err := GetConfig()
	if err != nil {
		return err
	}
	config.IsEnabled = false
	return SetConfig(config)
}

func CheckFatalError(err error) {
	if err != nil {
		log.Fatalf("hishtory fatal error: %v", err)
	}
}

func Install() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %v", err)
	}
	clientDir := path.Join(homedir, shared.HISHTORY_PATH)
	err = os.MkdirAll(clientDir, 0744)
	if err != nil {
		return fmt.Errorf("failed to create folder for hishtory binary: %v", err)
	}
	path, err := installBinary(homedir)
	if err != nil {
		return err
	}
	err = configureBashrc(homedir, path)
	if err != nil {
		return err
	}
	_, err = GetConfig()
	if err != nil {
		// No config, so set up a new installation
		return Setup(os.Args)
	}
	return nil
}

func configureBashrc(homedir, binaryPath string) error {
	// Create the file we're going to source in our bashrc. Do this no matter what in case there are updates to it.
	bashConfigPath := path.Join(filepath.Dir(binaryPath), "config.sh")
	err := ioutil.WriteFile(bashConfigPath, []byte(CONFIG_SH_CONTENTS), 0644)
	if err != nil {
		return fmt.Errorf("failed to write config.sh file: %v", err)
	}
	// Check if we need to configure the bashrc
	bashrc, err := ioutil.ReadFile(path.Join(homedir, ".bashrc"))
	if err != nil {
		return fmt.Errorf("failed to read bashrc: %v", err)
	}
	if strings.Contains(string(bashrc), "# Hishtory Config:") {
		return nil
	}
	// Add to bashrc
	f, err := os.OpenFile(path.Join(homedir, ".bashrc"), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to append to bashrc: %v", err)
	}
	defer f.Close()
	_, err = f.WriteString("\n# Hishtory Config:\nexport PATH=\"$PATH:" + filepath.Dir(binaryPath) + "\"\nsource " + bashConfigPath + "\n")
	if err != nil {
		return fmt.Errorf("failed to append to bashrc: %v", err)
	}
	return nil
}

func installBinary(homedir string) (string, error) {
	clientPath, err := exec.LookPath("hishtory")
	if err != nil {
		clientPath = path.Join(homedir, shared.HISHTORY_PATH, "hishtory")
	}
	err = copyFile(os.Args[0], clientPath)
	if err != nil {
		return "", fmt.Errorf("failed to copy hishtory binary to $PATH: %v", err)
	}
	err = os.Chmod(clientPath, 0700)
	if err != nil {
		return "", fmt.Errorf("failed to set permissions on hishtory binary: %v", err)
	}
	return clientPath, nil
}

func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}

func Update(url string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command("bash", "-c", "curl -o /tmp/hishtory-client "+url+"; chmod +x /tmp/hishtory-client")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Failed to download update: %v, stdout=%#v, stderr=%#v", err, stdout.String(), stderr.String())
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %v", err)
	}
	err = syscall.Unlink(path.Join(homedir, shared.HISHTORY_PATH, "hishtory"))
	if err != nil {
		return fmt.Errorf("Failed to unlink: %v", err)
	}
	cmd = exec.Command("/tmp/hishtory-client", "install")
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Failed to update: %v", err)
	}
	return nil
}

func GetServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return "https://api.hishtory.dev"
}
