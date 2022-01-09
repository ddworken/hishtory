package shared

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/rodaine/table"
)

const (
	CONFIG_PATH = ".hishtory.config"
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
	if strings.HasPrefix(cwd, homedir) {
		return strings.Replace(cwd, homedir, "~", 1), nil
	}
	return cwd, nil
}

func BuildHistoryEntry(args []string) (*HistoryEntry, error) {
	var entry HistoryEntry

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

	// TODO(ddworken): start time

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

	// user secret
	userSecret, err := GetUserSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.UserSecret = userSecret

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
	return SetConfig(config)
}

func DisplayResults(results []*HistoryEntry) {
	headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()
	tbl := table.New("Hostname", "CWD", "Timestamp", "Exit Code", "Command")
	tbl.WithHeaderFormatter(headerFmt)

	for _, result := range results {
		tbl.AddRow(result.Hostname, result.CurrentWorkingDirectory, result.EndTime.Format("Jan 2 2006 15:04:05 MST"), result.ExitCode, result.Command)
	}

	tbl.Print()
}

type ClientConfig struct {
	UserSecret string `json:"user_secret"`
	IsEnabled  bool   `json:"is_enabled"`
}

func GetConfig() (ClientConfig, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to retrieve homedir: %v", err)
	}
	data, err := os.ReadFile(path.Join(homedir, CONFIG_PATH))
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
	err = os.WriteFile(path.Join(homedir, CONFIG_PATH), serializedConfig, 0600)
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
