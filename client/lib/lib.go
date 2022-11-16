package lib

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "embed" // for embedding config.sh

	"gorm.io/gorm"

	"github.com/araddon/dateparse"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/rodaine/table"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
)

//go:embed config.sh
var ConfigShContents string

//go:embed config.zsh
var ConfigZshContents string

//go:embed config.fish
var ConfigFishContents string

var Version string = "Unknown"

// 256KB ought to be enough for any reasonable cmd
var maxSupportedLineLengthForImport = 256_000

func getCwd(ctx *context.Context) (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get cwd for last command: %v", err)
	}
	homedir := hctx.GetHome(ctx)
	if cwd == homedir {
		return "~/", homedir, nil
	}
	if strings.HasPrefix(cwd, homedir) {
		return strings.Replace(cwd, homedir, "~", 1), homedir, nil
	}
	return cwd, homedir, nil
}

func BuildHistoryEntry(ctx *context.Context, args []string) (*data.HistoryEntry, error) {
	if len(args) < 6 {
		hctx.GetLogger().Warnf("BuildHistoryEntry called with args=%#v, which has too few entries! This can happen in specific edge cases for newly opened terminals and is likely not a problem.", args)
		return nil, nil
	}
	shell := args[2]

	var entry data.HistoryEntry

	// exitCode
	exitCode, err := strconv.Atoi(args[3])
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

	// cwd and homedir
	cwd, homedir, err := getCwd(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.CurrentWorkingDirectory = cwd
	entry.HomeDirectory = homedir

	// start time
	seconds, err := parseCrossPlatformInt(args[5])
	if err != nil {
		return nil, fmt.Errorf("failed to parse start time %s as int: %v", args[5], err)
	}
	entry.StartTime = time.Unix(seconds, 0)

	// end time
	entry.EndTime = time.Now()

	// command
	if shell == "bash" {
		cmd, err := getLastCommand(args[4])
		if err != nil {
			return nil, fmt.Errorf("failed to build history entry: %v", err)
		}
		shouldBeSkipped, err := shouldSkipHiddenCommand(ctx, args[4])
		if err != nil {
			return nil, fmt.Errorf("failed to check if command was hidden: %v", err)
		}
		if shouldBeSkipped || strings.HasPrefix(cmd, " ") {
			// Don't save commands that start with a space
			return nil, nil
		}
		cmd, err = maybeSkipBashHistTimePrefix(cmd)
		if err != nil {
			return nil, err
		}
		entry.Command = cmd
	} else if shell == "zsh" || shell == "fish" {
		cmd := strings.TrimSuffix(strings.TrimSuffix(args[4], "\n"), " ")
		if strings.HasPrefix(cmd, " ") {
			// Don't save commands that start with a space
			return nil, nil
		}
		entry.Command = cmd
	} else {
		return nil, fmt.Errorf("tried to save a hishtory entry from an unsupported shell=%#v", shell)
	}
	if strings.TrimSpace(entry.Command) == "" {
		// Skip recording empty commands where the user just hits enter in their terminal
		return nil, nil
	}

	// hostname
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %v", err)
	}
	entry.Hostname = hostname

	// device ID
	config := hctx.GetConf(ctx)
	entry.DeviceId = config.DeviceId

	// custom columns
	cc, err := buildCustomColumns(ctx)
	if err != nil {
		return nil, err
	}
	entry.CustomColumns = cc

	return &entry, nil
}

func buildCustomColumns(ctx *context.Context) (data.CustomColumns, error) {
	ccs := data.CustomColumns{}
	config := hctx.GetConf(ctx)
	for _, cc := range config.CustomColumns {
		cmd := exec.Command("bash", "-c", cc.ColumnCommand)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Start()
		if err != nil {
			return nil, fmt.Errorf("failed to execute custom command named %v (stdout=%#v, stderr=%#v)", cc.ColumnName, stdout.String(), stderr.String())
		}
		err = cmd.Wait()
		if err != nil {
			// Log a warning, but don't crash. This way commands can exit with a different status and still work.
			hctx.GetLogger().Warnf("failed to execute custom command named %v (stdout=%#v, stderr=%#v)", cc.ColumnName, stdout.String(), stderr.String())
		}
		ccv := data.CustomColumn{
			Name: cc.ColumnName,
			Val:  strings.TrimSpace(stdout.String()),
		}
		ccs = append(ccs, ccv)
	}
	return ccs, nil
}

func stripZshWeirdness(cmd string) string {
	// Zsh has this weird behavior where sometimes commands are saved in the hishtory file
	// with a weird prefix. I've never been able to figure out why this happens, but we
	// can at least strip it.
	firstCommandBugRegex := regexp.MustCompile(`: \d+:\d;(.*)`)
	matches := firstCommandBugRegex.FindStringSubmatch(cmd)
	if len(matches) == 2 {
		return matches[1]
	}
	return cmd
}

func isBashWeirdness(cmd string) bool {
	// Bash has this weird behavior where the it has entries like `#1664342754` in the
	// history file. We want to skip these.
	firstCommandBugRegex := regexp.MustCompile(`^#\d+\s+$`)
	return firstCommandBugRegex.MatchString(cmd)
}

func buildRegexFromTimeFormat(timeFormat string) string {
	expectedRegex := ""
	lastCharWasPercent := false
	for _, char := range timeFormat {
		if lastCharWasPercent {
			if char == '%' {
				expectedRegex += regexp.QuoteMeta(string(char))
				lastCharWasPercent = false
				continue
			} else if char == 't' {
				expectedRegex += "\t"
			} else if char == 'F' {
				expectedRegex += buildRegexFromTimeFormat("%Y-%m-%d")
			} else if char == 'Y' {
				expectedRegex += "[0-9]{4}"
			} else if char == 'G' {
				expectedRegex += "[0-9]{4}"
			} else if char == 'g' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'C' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'u' || char == 'w' {
				expectedRegex += "[0-9]"
			} else if char == 'm' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'd' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'D' {
				expectedRegex += buildRegexFromTimeFormat("%m/%d/%y")
			} else if char == 'T' {
				expectedRegex += buildRegexFromTimeFormat("%H:%M:%S")
			} else if char == 'H' || char == 'I' || char == 'U' || char == 'V' || char == 'W' || char == 'y' || char == 'Y' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'M' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'j' {
				expectedRegex += "[0-9]{3}"
			} else if char == 'S' || char == 'm' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'c' {
				// Note: Specific to the POSIX locale
				expectedRegex += buildRegexFromTimeFormat("%a %b %e %H:%M:%S %Y")
			} else if char == 'a' {
				// Note: Specific to the POSIX locale
				expectedRegex += "(Sun|Mon|Tue|Wed|Thu|Fri|Sat)"
			} else if char == 'b' || char == 'h' {
				// Note: Specific to the POSIX locale
				expectedRegex += "(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)"
			} else if char == 'e' || char == 'k' || char == 'l' {
				expectedRegex += "[0-9 ]{2}"
			} else if char == 'n' {
				expectedRegex += "\n"
			} else if char == 'p' {
				expectedRegex += "(AM|PM)"
			} else if char == 'P' {
				expectedRegex += "(am|pm)"
			} else if char == 's' {
				expectedRegex += "\\d+"
			} else if char == 'z' {
				expectedRegex += "[+-][0-9]{4}"
			} else if char == 'r' {
				expectedRegex += buildRegexFromTimeFormat("%I:%M:%S %p")
			} else if char == 'R' {
				expectedRegex += buildRegexFromTimeFormat("%H:%M")
			} else if char == 'x' {
				expectedRegex += buildRegexFromTimeFormat("%m/%d/%y")
			} else if char == 'X' {
				expectedRegex += buildRegexFromTimeFormat("%H:%M:%S")
			} else {
				panic(fmt.Sprintf("buildRegexFromTimeFormat doesn't support %%%v, please open a bug against github.com/ddworken/hishtory", string(char)))
			}
		} else if char != '%' {
			expectedRegex += regexp.QuoteMeta(string(char))
		}
		lastCharWasPercent = false
		if char == '%' {
			lastCharWasPercent = true
		}
	}
	return expectedRegex
}

func maybeSkipBashHistTimePrefix(cmdLine string) (string, error) {
	format := os.Getenv("HISTTIMEFORMAT")
	if format == "" {
		return cmdLine, nil
	}
	re, err := regexp.Compile("^" + buildRegexFromTimeFormat(format))
	if err != nil {
		return "", fmt.Errorf("failed to parse regex for HISTTIMEFORMAT variable: %v", err)
	}
	return re.ReplaceAllLiteralString(cmdLine, ""), nil
}

func parseCrossPlatformInt(data string) (int64, error) {
	data = strings.TrimSuffix(data, "N")
	return strconv.ParseInt(data, 10, 64)
}

func getLastCommand(history string) (string, error) {
	split := strings.SplitN(strings.TrimSpace(history), " ", 2)
	if len(split) <= 1 {
		return "", fmt.Errorf("got unexpected bash history line: %#v, please open a bug at github.com/ddworken/hishtory", history)
	}
	split = strings.SplitN(split[1], " ", 2)
	if len(split) <= 1 {
		return "", fmt.Errorf("got unexpected bash history line: %#v, please open a bug at github.com/ddworken/hishtory", history)
	}
	return split[1], nil
}

func shouldSkipHiddenCommand(ctx *context.Context, historyLine string) (bool, error) {
	config := hctx.GetConf(ctx)
	if config.LastSavedHistoryLine == historyLine {
		return true, nil
	}
	config.LastSavedHistoryLine = historyLine
	err := hctx.SetConfig(config)
	if err != nil {
		return false, err
	}
	return false, nil
}

func Setup(args []string) error {
	userSecret := uuid.Must(uuid.NewRandom()).String()
	isOffline := false
	if len(args) > 2 && args[2] != "" {
		if args[2] == "--offline" {
			isOffline = true
		} else {
			if args[2][0] == '-' {
				return fmt.Errorf("refusing to set user secret to %#v since it looks like a flag", args[2])
			}
			userSecret = args[2]
		}
	}
	fmt.Println("Setting secret hishtory key to " + string(userSecret))

	// Create and set the config
	var config hctx.ClientConfig
	config.UserSecret = userSecret
	config.IsEnabled = true
	config.DeviceId = uuid.Must(uuid.NewRandom()).String()
	config.ControlRSearchEnabled = true
	config.IsOffline = isOffline
	err := hctx.SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to persist config to disk: %v", err)
	}

	// Drop all existing data
	db, err := hctx.OpenLocalSqliteDb()
	if err != nil {
		return err
	}
	db.Exec("DELETE FROM history_entries")

	// Bootstrap from remote date
	if config.IsOffline {
		return nil
	}
	_, err = ApiGet("/api/v1/register?user_id=" + data.UserId(userSecret) + "&device_id=" + config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to register device with backend: %v", err)
	}

	respBody, err := ApiGet("/api/v1/bootstrap?user_id=" + data.UserId(userSecret) + "&device_id=" + config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to bootstrap device from the backend: %v", err)
	}
	var retrievedEntries []*shared.EncHistoryEntry
	err = json.Unmarshal(respBody, &retrievedEntries)
	if err != nil {
		return fmt.Errorf("failed to load JSON response: %v", err)
	}
	for _, entry := range retrievedEntries {
		decEntry, err := data.DecryptHistoryEntry(userSecret, *entry)
		if err != nil {
			return fmt.Errorf("failed to decrypt history entry from server: %v", err)
		}
		AddToDbIfNew(db, decEntry)
	}

	return nil
}

func AddToDbIfNew(db *gorm.DB, entry data.HistoryEntry) {
	tx := db.Where("local_username = ?", entry.LocalUsername)
	tx = tx.Where("hostname = ?", entry.Hostname)
	tx = tx.Where("command = ?", entry.Command)
	tx = tx.Where("current_working_directory = ?", entry.CurrentWorkingDirectory)
	tx = tx.Where("home_directory = ?", entry.HomeDirectory)
	tx = tx.Where("exit_code = ?", entry.ExitCode)
	tx = tx.Where("start_time = ?", entry.StartTime)
	tx = tx.Where("end_time = ?", entry.EndTime)
	var results []data.HistoryEntry
	tx.Limit(1).Find(&results)
	if len(results) == 0 {
		db.Create(entry)
		// TODO: check the error here and bubble it up
	}
}

func getCustomColumnValue(ctx *context.Context, header string, entry data.HistoryEntry) (string, error) {
	for _, c := range entry.CustomColumns {
		if strings.EqualFold(c.Name, header) {
			return c.Val, nil
		}
	}
	config := hctx.GetConf(ctx)
	for _, c := range config.CustomColumns {
		if strings.EqualFold(c.ColumnName, header) {
			return "", nil
		}
	}
	return "", fmt.Errorf("failed to find a column matching the column name %#v (is there a typo?)", header)
}

func buildTableRow(ctx *context.Context, columnNames []string, entry data.HistoryEntry) ([]string, error) {
	row := make([]string, 0)
	for _, header := range columnNames {
		switch header {
		case "Hostname":
			row = append(row, entry.Hostname)
		case "CWD":
			row = append(row, entry.CurrentWorkingDirectory)
		case "Timestamp":
			row = append(row, entry.StartTime.Format(hctx.GetConf(ctx).TimestampFormat))
		case "Runtime":
			row = append(row, entry.EndTime.Sub(entry.StartTime).Round(time.Millisecond).String())
		case "Exit Code":
			row = append(row, fmt.Sprintf("%d", entry.ExitCode))
		case "Command":
			row = append(row, entry.Command)
		default:
			customColumnValue, err := getCustomColumnValue(ctx, header, entry)
			if err != nil {
				return nil, err
			}
			row = append(row, customColumnValue)
		}
	}
	return row, nil
}

func stringArrayToAnyArray(arr []string) []any {
	ret := make([]any, 0)
	for _, item := range arr {
		ret = append(ret, item)
	}
	return ret
}

func DisplayResults(ctx *context.Context, results []*data.HistoryEntry, numResults int) error {
	config := hctx.GetConf(ctx)
	headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()

	columns := make([]any, 0)
	for _, c := range config.DisplayedColumns {
		columns = append(columns, c)
	}
	tbl := table.New(columns...)
	tbl.WithHeaderFormatter(headerFmt)

	lastCommand := ""
	numRows := 0
	for _, entry := range results {
		if entry != nil && strings.TrimSpace(entry.Command) == strings.TrimSpace(lastCommand) && config.FilterDuplicateCommands {
			continue
		}
		row, err := buildTableRow(ctx, config.DisplayedColumns, *entry)
		if err != nil {
			return err
		}
		tbl.AddRow(stringArrayToAnyArray(row)...)
		numRows += 1
		lastCommand = entry.Command
		if numRows >= numResults {
			break
		}
	}

	tbl.Print()
	return nil
}

func IsEnabled(ctx *context.Context) (bool, error) {
	return hctx.GetConf(ctx).IsEnabled, nil
}

func Enable(ctx *context.Context) error {
	config := hctx.GetConf(ctx)
	config.IsEnabled = true
	return hctx.SetConfig(config)
}

func Disable(ctx *context.Context) error {
	config := hctx.GetConf(ctx)
	config.IsEnabled = false
	return hctx.SetConfig(config)
}

func CheckFatalError(err error) {
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		log.Fatalf("hishtory fatal error at %s:%d: %v", filename, line, err)
	}
}

func ImportHistory(ctx *context.Context, shouldReadStdin, force bool) (int, error) {
	config := hctx.GetConf(ctx)
	if config.HaveCompletedInitialImport && !force {
		// Don't run an import if we already have run one. This avoids importing the same entry multiple times.
		return 0, nil
	}
	homedir := hctx.GetHome(ctx)
	bashHistPath := filepath.Join(homedir, ".bash_history")
	historyEntries, err := readFileToArray(bashHistPath)
	if err != nil {
		return 0, fmt.Errorf("failed to parse bash history: %v", err)
	}
	zshHistPath := filepath.Join(homedir, ".zsh_history")
	extraEntries, err := readFileToArray(zshHistPath)
	if err != nil {
		return 0, fmt.Errorf("failed to parse zsh history: %v", err)
	}
	historyEntries = append(historyEntries, extraEntries...)
	extraEntries, err = parseFishHistory(homedir)
	if err != nil {
		return 0, fmt.Errorf("failed to parse fish history: %v", err)
	}
	historyEntries = append(historyEntries, extraEntries...)
	if histfile := os.Getenv("HISTFILE"); histfile != "" && histfile != zshHistPath && histfile != bashHistPath {
		extraEntries, err := readFileToArray(histfile)
		if err != nil {
			return 0, fmt.Errorf("failed to parse histfile: %v", err)
		}
		historyEntries = append(historyEntries, extraEntries...)
	}
	if shouldReadStdin {
		extraEntries, err = readStdin()
		if err != nil {
			return 0, fmt.Errorf("failed to read stdin: %v", err)
		}
		historyEntries = append(historyEntries, extraEntries...)
	}
	db := hctx.GetDb(ctx)
	currentUser, err := user.Current()
	if err != nil {
		return 0, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return 0, err
	}
	for _, cmd := range historyEntries {
		cmd := stripZshWeirdness(cmd)
		if isBashWeirdness(cmd) || strings.HasPrefix(cmd, " ") {
			// Skip it
			continue
		}
		entry := data.HistoryEntry{
			LocalUsername:           currentUser.Name,
			Hostname:                hostname,
			Command:                 cmd,
			CurrentWorkingDirectory: "Unknown",
			HomeDirectory:           homedir,
			ExitCode:                0,
			StartTime:               time.Now(),
			EndTime:                 time.Now(),
			DeviceId:                config.DeviceId,
		}
		err = ReliableDbCreate(db, entry)
		if err != nil {
			return 0, fmt.Errorf("failed to insert imported history entry: %v", err)
		}
	}
	err = Reupload(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to upload hishtory import: %v", err)
	}
	config.HaveCompletedInitialImport = true
	err = hctx.SetConfig(config)
	if err != nil {
		return 0, fmt.Errorf("failed to mark initial import as completed, this may lead to duplicate history entries: %v", err)
	}
	// Trigger a checkpoint so that these bulk entries are added from the WAL to the main DB
	db.Exec("PRAGMA wal_checkpoint")
	return len(historyEntries), nil
}

func readStdin() ([]string, error) {
	ret := make([]string, 0)
	in := bufio.NewReader(os.Stdin)
	for {
		s, err := in.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break
		}
		s = strings.TrimSpace(s)
		if s != "" {
			ret = append(ret, s)
		}
	}
	return ret, nil
}

func parseFishHistory(homedir string) ([]string, error) {
	lines, err := readFileToArray(filepath.Join(homedir, ".local/share/fish/fish_history"))
	if err != nil {
		return nil, err
	}
	ret := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- cmd: ") {
			ret = append(ret, strings.SplitN(line, ": ", 2)[1])
		}
	}
	return ret, nil
}

func readFileToArray(path string) ([]string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxSupportedLineLengthForImport)
	scanner.Buffer(buf, maxSupportedLineLengthForImport)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func Install() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %v", err)
	}
	err = hctx.MakeHishtoryDir()
	if err != nil {
		return err
	}
	path, err := installBinary(homedir)
	if err != nil {
		return err
	}
	err = configureBashrc(homedir, path)
	if err != nil {
		return err
	}
	err = configureZshrc(homedir, path)
	if err != nil {
		return err
	}
	err = configureFish(homedir, path)
	if err != nil {
		return err
	}
	err = handleUpgradedFeatures()
	if err != nil {
		return err
	}
	_, err = hctx.GetConfig()
	if err != nil {
		// No config, so set up a new installation
		return Setup(os.Args)
	}
	return nil
}

func handleUpgradedFeatures() error {
	configConents, err := hctx.GetConfigContents()
	if err != nil {
		// No config, so this is a new install and thus there is nothing to do
		return nil
	}
	if strings.Contains(string(configConents), "enable_control_r_search") {
		// control-r search is already configured, so there is nothing to do
		return nil
	}
	// Enable control-r search
	config, err := hctx.GetConfig()
	if err != nil {
		return err
	}
	config.ControlRSearchEnabled = true
	return hctx.SetConfig(config)
}

func getFishConfigPath(homedir string) string {
	return path.Join(homedir, data.HISHTORY_PATH, "config.fish")
}

func configureFish(homedir, binaryPath string) error {
	// Check if fish is installed
	_, err := exec.LookPath("fish")
	if err != nil {
		return nil
	}
	// Create the file we're going to source. Do this no matter what in case there are updates to it.
	configContents := ConfigFishContents
	if os.Getenv("HISHTORY_TEST") != "" {
		testConfig, err := tweakConfigForTests(ConfigFishContents)
		if err != nil {
			return err
		}
		configContents = testConfig
	}
	err = ioutil.WriteFile(getFishConfigPath(homedir), []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.zsh file: %v", err)
	}
	// Check if we need to configure the fishrc
	fishIsConfigured, err := isFishConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.config/fish/config.fish: %v", err)
	}
	if fishIsConfigured {
		return nil
	}
	// Add to fishrc
	err = os.MkdirAll(path.Join(homedir, ".config/fish"), 0o744)
	if err != nil {
		return fmt.Errorf("failed to create fish config directory: %v", err)
	}
	return addToShellConfig(path.Join(homedir, ".config/fish/config.fish"), getFishConfigFragment(homedir))
}

func getFishConfigFragment(homedir string) string {
	return "\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, data.HISHTORY_PATH) + "\"\nsource " + getFishConfigPath(homedir) + "\n"
}

func isFishConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".config/fish/config.fish"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	fishConfig, err := ioutil.ReadFile(path.Join(homedir, ".config/fish/config.fish"))
	if err != nil {
		return false, fmt.Errorf("failed to read ~/.config/fish/config.fish: %v", err)
	}
	return strings.Contains(string(fishConfig), "# Hishtory Config:"), nil
}

func getZshConfigPath(homedir string) string {
	return path.Join(homedir, data.HISHTORY_PATH, "config.zsh")
}

func configureZshrc(homedir, binaryPath string) error {
	// Create the file we're going to source in our zshrc. Do this no matter what in case there are updates to it.
	configContents := ConfigZshContents
	if os.Getenv("HISHTORY_TEST") != "" {
		testConfig, err := tweakConfigForTests(configContents)
		if err != nil {
			return err
		}
		configContents = testConfig
	}
	err := ioutil.WriteFile(getZshConfigPath(homedir), []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.zsh file: %v", err)
	}
	// Check if we need to configure the zshrc
	zshIsConfigured, err := isZshConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check .zshrc: %v", err)
	}
	if zshIsConfigured {
		return nil
	}
	// Add to zshrc
	return addToShellConfig(getZshRcPath(homedir), getZshConfigFragment(homedir))
}

func getZshRcPath(homedir string) string {
	if zdotdir := os.Getenv("ZDOTDIR"); zdotdir != "" {
		return path.Join(zdotdir, ".zshrc")
	}
	return path.Join(homedir, ".zshrc")
}

func getZshConfigFragment(homedir string) string {
	return "\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, data.HISHTORY_PATH) + "\"\nsource " + getZshConfigPath(homedir) + "\n"
}

func isZshConfigured(homedir string) (bool, error) {
	_, err := os.Stat(getZshRcPath(homedir))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := ioutil.ReadFile(getZshRcPath(homedir))
	if err != nil {
		return false, fmt.Errorf("failed to read zshrc: %v", err)
	}
	return strings.Contains(string(bashrc), "# Hishtory Config:"), nil
}

func getBashConfigPath(homedir string) string {
	return path.Join(homedir, data.HISHTORY_PATH, "config.sh")
}

func configureBashrc(homedir, binaryPath string) error {
	// Create the file we're going to source in our bashrc. Do this no matter what in case there are updates to it.
	configContents := ConfigShContents
	if os.Getenv("HISHTORY_TEST") != "" {
		testConfig, err := tweakConfigForTests(ConfigShContents)
		if err != nil {
			return err
		}
		configContents = testConfig
	}
	err := ioutil.WriteFile(getBashConfigPath(homedir), []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.sh file: %v", err)
	}
	// Check if we need to configure the bashrc and configure it if so
	bashRcIsConfigured, err := isBashRcConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.bashrc: %v", err)
	}
	if !bashRcIsConfigured {
		err = addToShellConfig(path.Join(homedir, ".bashrc"), getBashConfigFragment(homedir))
		if err != nil {
			return err
		}
	}
	// Check if we need to configure the bash_profile and configure it if so
	bashProfileIsConfigured, err := isBashProfileConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.bash_profile: %v", err)
	}
	if !bashProfileIsConfigured {
		err = addToShellConfig(path.Join(homedir, ".bash_profile"), getBashConfigFragment(homedir))
		if err != nil {
			return err
		}
	}
	return nil
}

func addToShellConfig(shellConfigPath, configFragment string) error {
	f, err := os.OpenFile(shellConfigPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to append to %s: %v", shellConfigPath, err)
	}
	defer f.Close()
	_, err = f.WriteString(configFragment)
	if err != nil {
		return fmt.Errorf("failed to append to %s: %v", shellConfigPath, err)
	}
	return nil
}

func getBashConfigFragment(homedir string) string {
	return "\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, data.HISHTORY_PATH) + "\"\nsource " + getBashConfigPath(homedir) + "\n"
}

func isBashRcConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".bashrc"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := ioutil.ReadFile(path.Join(homedir, ".bashrc"))
	if err != nil {
		return false, fmt.Errorf("failed to read bashrc: %v", err)
	}
	return strings.Contains(string(bashrc), "# Hishtory Config:"), nil
}

func isBashProfileConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".bash_profile"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := ioutil.ReadFile(path.Join(homedir, ".bash_profile"))
	if err != nil {
		return false, fmt.Errorf("failed to read bash_profile: %v", err)
	}
	return strings.Contains(string(bashrc), "# Hishtory Config:"), nil
}

func installBinary(homedir string) (string, error) {
	clientPath, err := exec.LookPath("hishtory")
	if err != nil {
		clientPath = path.Join(homedir, data.HISHTORY_PATH, "hishtory")
	}
	if _, err := os.Stat(clientPath); err == nil {
		err = syscall.Unlink(clientPath)
		if err != nil {
			return "", fmt.Errorf("failed to unlink %s for install: %v", clientPath, err)
		}
	}
	err = copyFile(os.Args[0], clientPath)
	if err != nil {
		return "", fmt.Errorf("failed to copy hishtory binary to $PATH: %v", err)
	}
	err = os.Chmod(clientPath, 0o700)
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

	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}

	return destination.Close()
}

func GetDownloadData() (shared.UpdateInfo, error) {
	respBody, err := ApiGet("/api/v1/download")
	if err != nil {
		return shared.UpdateInfo{}, fmt.Errorf("failed to download update info: %v", err)
	}
	var downloadData shared.UpdateInfo
	err = json.Unmarshal(respBody, &downloadData)
	if err != nil {
		return shared.UpdateInfo{}, fmt.Errorf("failed to parse update info: %v", err)
	}
	return downloadData, nil
}

func getTmpClientPath() string {
	tmpDir := "/tmp/"
	if os.Getenv("TMPDIR") != "" {
		tmpDir = os.Getenv("TMPDIR")
	}
	return path.Join(tmpDir, "hishtory-client")
}

func Update(ctx *context.Context) error {
	// Download the binary
	downloadData, err := GetDownloadData()
	if err != nil {
		return err
	}
	if downloadData.Version == "v0."+Version {
		fmt.Printf("Latest version (v0.%s) is already installed\n", Version)
		return nil
	}
	err = downloadFiles(downloadData)
	if err != nil {
		return err
	}

	// Verify the SLSA attestation
	var slsaError error
	if runtime.GOOS == "darwin" {
		slsaError = verifyBinaryMac(ctx, getTmpClientPath(), downloadData)
	} else {
		slsaError = verifyBinary(ctx, getTmpClientPath(), getTmpClientPath()+".intoto.jsonl", downloadData.Version)
	}
	if slsaError != nil {
		err = handleSlsaFailure(slsaError)
		if err != nil {
			return err
		}
	}

	// Unlink the existing binary so we can overwrite it even though it is still running
	if runtime.GOOS == "linux" {
		homedir := hctx.GetHome(ctx)
		err = syscall.Unlink(path.Join(homedir, data.HISHTORY_PATH, "hishtory"))
		if err != nil {
			return fmt.Errorf("failed to unlink %s for update: %v", path.Join(homedir, data.HISHTORY_PATH, "hishtory"), err)
		}
	}

	// Install the new one
	cmd := exec.Command("chmod", "+x", getTmpClientPath())
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to chmod +x the update (stdout=%#v, stderr=%#v): %v", stdout.String(), stderr.String(), err)
	}
	cmd = exec.Command(getTmpClientPath(), "install")
	cmd.Stdout = os.Stdout
	stderr = bytes.Buffer{}
	cmd.Stdin = os.Stdin
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to install update (stderr=%#v), is %s in a noexec directory? (if so, set the TMPDIR environment variable): %v", stderr.String(), getTmpClientPath(), err)
	}
	fmt.Printf("Successfully updated hishtory from v0.%s to %s\n", Version, downloadData.Version)
	return nil
}

func verifyBinaryMac(ctx *context.Context, binaryPath string, downloadData shared.UpdateInfo) error {
	// On Mac, binary verification is a bit more complicated since mac binaries are code
	// signed. To verify a signed binary, we:
	// 1. Download the unsigned binary
	// 2. Strip the real signature from the signed binary and the ad-hoc signature from the unsigned binary
	// 3. Assert that those binaries match
	// 4. Use SLSA to verify the unsigned binary (pre-strip)
	// Yes, this is complicated. But AFAICT, it is the only solution here.

	// Step 1: Download the "unsigned" binary that actually has an ad-hoc signature from the
	// go compiler.
	unsignedBinaryPath := binaryPath + "-unsigned"
	var err error = nil
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		err = downloadFile(unsignedBinaryPath, downloadData.DarwinAmd64UnsignedUrl)
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		err = downloadFile(unsignedBinaryPath, downloadData.DarwinArm64UnsignedUrl)
	} else {
		err = fmt.Errorf("verifyBinaryMac() called for the unhandled branch GOOS=%s, GOARCH=%s", runtime.GOOS, runtime.GOARCH)
	}
	if err != nil {
		return err
	}

	// Step 2: Create the .nosig files that have no signatures whatsoever
	noSigSuffix := ".nosig"
	err = stripCodeSignature(binaryPath, binaryPath+noSigSuffix)
	if err != nil {
		return err
	}
	err = stripCodeSignature(unsignedBinaryPath, unsignedBinaryPath+noSigSuffix)
	if err != nil {
		return err
	}

	// Step 3: Compare the binaries
	err = assertIdenticalBinaries(binaryPath+noSigSuffix, unsignedBinaryPath+noSigSuffix)
	if err != nil {
		return err
	}

	// Step 4: Use SLSA to verify the unsigned binary
	return verifyBinary(ctx, unsignedBinaryPath, getTmpClientPath()+".intoto.jsonl", downloadData.Version)
}

func assertIdenticalBinaries(bin1Path, bin2Path string) error {
	bin1, err := os.ReadFile(bin1Path)
	if err != nil {
		return err
	}
	bin2, err := os.ReadFile(bin2Path)
	if err != nil {
		return err
	}
	if len(bin1) != len(bin2) {
		return fmt.Errorf("unsigned binaries have different lengths (len(%s)=%d, len(%s)=%d)", bin1Path, len(bin1), bin2Path, len(bin2))
	}
	differences := make([]string, 0)
	for i := range bin1 {
		b1 := bin1[i]
		b2 := bin2[i]
		if b1 != b2 {
			differences = append(differences, fmt.Sprintf("diff at index %d: %s[%d]=%x, %s[%d]=%x", i, bin1Path, i, b1, bin2Path, i, b2))
		}
	}
	for _, d := range differences {
		hctx.GetLogger().Infof("comparing binaries: %#v\n", d)
	}
	if len(differences) > 5 {
		return fmt.Errorf("found %d differences in the binary", len(differences))
	}
	return nil
}

func stripCodeSignature(inPath, outPath string) error {
	_, err := exec.LookPath("codesign_allocate")
	if err != nil {
		return fmt.Errorf("your system is missing the codesign_allocate tool, so we can't verify the SLSA attestation (you can bypass this by setting `export HISHTORY_DISABLE_SLSA_ATTESTATION=true` in your shell)")
	}
	cmd := exec.Command("codesign_allocate", "-i", inPath, "-o", outPath, "-r")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to use codesign_allocate to strip signatures on binary=%v (stdout=%#v, stderr%#v): %v", inPath, stdout.String(), stderr.String(), err)
	}
	return nil
}

func downloadFiles(updateInfo shared.UpdateInfo) error {
	clientUrl := ""
	clientProvenanceUrl := ""
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		clientUrl = updateInfo.LinuxAmd64Url
		clientProvenanceUrl = updateInfo.LinuxAmd64AttestationUrl
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		clientUrl = updateInfo.DarwinAmd64Url
		clientProvenanceUrl = updateInfo.DarwinAmd64AttestationUrl
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		clientUrl = updateInfo.DarwinArm64Url
		clientProvenanceUrl = updateInfo.DarwinArm64AttestationUrl
	} else {
		return fmt.Errorf("no update info found for GOOS=%s, GOARCH=%s", runtime.GOOS, runtime.GOARCH)
	}
	err := downloadFile(getTmpClientPath(), clientUrl)
	if err != nil {
		return err
	}
	err = downloadFile(getTmpClientPath()+".intoto.jsonl", clientProvenanceUrl)
	if err != nil {
		return err
	}
	return nil
}

func downloadFile(filename, url string) error {
	// Download the data
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file at %s to %s: %v", url, filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download file at %s due to resp_code=%d", url, resp.StatusCode)
	}

	// Delete the file if it already exists. This is necessary due to https://openradar.appspot.com/FB8735191
	if _, err := os.Stat(filename); err == nil {
		err = os.Remove(filename)
		if err != nil {
			return fmt.Errorf("failed to delete file %v when trying to download a new version", filename)
		}
	}

	// Create the file
	out, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to save file to %s: %v", filename, err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)

	return err
}

func getServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return "https://api.hishtory.dev"
}

func httpClient() *http.Client {
	return &http.Client{}
}

func ApiGet(path string) ([]byte, error) {
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return nil, fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}
	start := time.Now()
	req, err := http.NewRequest("GET", getServerHostname()+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET: %v", err)
	}
	req.Header.Set("X-Hishtory-Version", "v0."+Version)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to GET %s%s: %v", getServerHostname(), path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to GET %s%s: status_code=%d", getServerHostname(), path, resp.StatusCode)
	}
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from GET %s%s: %v", getServerHostname(), path, err)
	}
	duration := time.Since(start)
	hctx.GetLogger().Infof("ApiGet(%#v): %s\n", path, duration.String())
	return respBody, nil
}

func ApiPost(path, contentType string, data []byte) ([]byte, error) {
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return nil, fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}
	start := time.Now()
	req, err := http.NewRequest("POST", getServerHostname()+path, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create POST: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Hishtory-Version", "v0."+Version)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to POST %s: status_code=%d", path, resp.StatusCode)
	}
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from POST %s: %v", path, err)
	}
	duration := time.Since(start)
	hctx.GetLogger().Infof("ApiPost(%#v): %s\n", path, duration.String())
	return respBody, nil
}

func IsOfflineError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "dial tcp: lookup api.hishtory.dev") ||
		strings.Contains(err.Error(), "connect: network is unreachable") ||
		strings.Contains(err.Error(), "read: connection reset by peer") ||
		strings.Contains(err.Error(), ": EOF") ||
		strings.Contains(err.Error(), ": status_code=502") ||
		strings.Contains(err.Error(), ": status_code=503") ||
		strings.Contains(err.Error(), ": i/o timeout") ||
		strings.Contains(err.Error(), "connect: operation timed out")
}

func ReliableDbCreate(db *gorm.DB, entry interface{}) error {
	var err error = nil
	i := 0
	for i = 0; i < 10; i++ {
		result := db.Create(entry)
		err = result.Error
		if err != nil {
			errMsg := err.Error()
			if errMsg == "database is locked (5) (SQLITE_BUSY)" || errMsg == "database is locked (261)" {
				time.Sleep(time.Duration(i*rand.Intn(100)) * time.Millisecond)
				continue
			}
			if strings.Contains(errMsg, "UNIQUE constraint failed") {
				if i == 0 {
					return err
				} else {
					return nil
				}
			}
			return fmt.Errorf("unrecoverable sqlite error: %v", err)
		}
		if err != nil && err.Error() != "database is locked (5) (SQLITE_BUSY)" {
			return fmt.Errorf("unrecoverable sqlite error: %v", err)
		}
	}
	return fmt.Errorf("failed to create DB entry even with %d retries: %v", i, err)
}

func EncryptAndMarshal(config hctx.ClientConfig, entries []*data.HistoryEntry) ([]byte, error) {
	var encEntries []shared.EncHistoryEntry
	for _, entry := range entries {
		encEntry, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt history entry")
		}
		encEntry.DeviceId = config.DeviceId
		encEntries = append(encEntries, encEntry)
	}
	jsonValue, err := json.Marshal(encEntries)
	if err != nil {
		return jsonValue, fmt.Errorf("failed to marshal encrypted history entry: %v", err)
	}
	return jsonValue, nil
}

func Redact(ctx *context.Context, query string, force bool) error {
	tx, err := MakeWhereQueryFromSearch(ctx, hctx.GetDb(ctx), query)
	if err != nil {
		return err
	}
	var historyEntries []*data.HistoryEntry
	res := tx.Find(&historyEntries)
	if res.Error != nil {
		return res.Error
	}
	if force {
		fmt.Printf("Permanently deleting %d entries\n", len(historyEntries))
	} else {
		fmt.Printf("This will permanently delete %d entries, are you sure? [y/N]", len(historyEntries))
		reader := bufio.NewReader(os.Stdin)
		resp, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read response: %v", err)
		}
		if strings.TrimSpace(resp) != "y" {
			fmt.Printf("Aborting delete per user response of %#v\n", strings.TrimSpace(resp))
			return nil
		}
	}
	tx, err = MakeWhereQueryFromSearch(ctx, hctx.GetDb(ctx), query)
	if err != nil {
		return err
	}
	res = tx.Delete(&data.HistoryEntry{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != int64(len(historyEntries)) {
		return fmt.Errorf("DB deleted %d rows, when we only expected to delete %d rows, something may have gone wrong", res.RowsAffected, len(historyEntries))
	}
	err = deleteOnRemoteInstances(ctx, historyEntries)
	if err != nil {
		return err
	}
	return nil
}

func deleteOnRemoteInstances(ctx *context.Context, historyEntries []*data.HistoryEntry) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}

	var deletionRequest shared.DeletionRequest
	deletionRequest.SendTime = time.Now()
	deletionRequest.UserId = data.UserId(config.UserSecret)

	for _, entry := range historyEntries {
		deletionRequest.Messages.Ids = append(deletionRequest.Messages.Ids, shared.MessageIdentifier{Date: entry.EndTime, DeviceId: entry.DeviceId})
	}
	data, err := json.Marshal(deletionRequest)
	if err != nil {
		return err
	}
	_, err = ApiPost("/api/v1/add-deletion-request", "application/json", data)
	if err != nil {
		return fmt.Errorf("failed to send deletion request to backend service, this may cause commands to not get deleted on other instances of hishtory: %v", err)
	}
	return nil
}

func Reupload(ctx *context.Context) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	entries, err := Search(ctx, hctx.GetDb(ctx), "", 0)
	if err != nil {
		return fmt.Errorf("failed to reupload due to failed search: %v", err)
	}
	for _, chunk := range chunks(entries, 100) {
		jsonValue, err := EncryptAndMarshal(config, chunk)
		if err != nil {
			return fmt.Errorf("failed to reupload due to failed encryption: %v", err)
		}
		_, err = ApiPost("/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
		if err != nil {
			return fmt.Errorf("failed to reupload due to failed POST: %v", err)
		}
	}
	return nil
}

func chunks[k any](slice []k, chunkSize int) [][]k {
	var chunks [][]k
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}

func RetrieveAdditionalEntriesFromRemote(ctx *context.Context) error {
	db := hctx.GetDb(ctx)
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	respBody, err := ApiGet("/api/v1/query?device_id=" + config.DeviceId + "&user_id=" + data.UserId(config.UserSecret))
	if IsOfflineError(err) {
		return nil
	}
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
		AddToDbIfNew(db, decEntry)
	}
	return ProcessDeletionRequests(ctx)
}

func ProcessDeletionRequests(ctx *context.Context) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	resp, err := ApiGet("/api/v1/get-deletion-requests?user_id=" + data.UserId(config.UserSecret) + "&device_id=" + config.DeviceId)
	if IsOfflineError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var deletionRequests []*shared.DeletionRequest
	err = json.Unmarshal(resp, &deletionRequests)
	if err != nil {
		return err
	}
	db := hctx.GetDb(ctx)
	for _, request := range deletionRequests {
		for _, entry := range request.Messages.Ids {
			res := db.Where("device_id = ? AND end_time = ?", entry.DeviceId, entry.Date).Delete(&data.HistoryEntry{})
			if res.Error != nil {
				return fmt.Errorf("DB error: %v", res.Error)
			}
		}
	}
	return nil
}

func GetBanner(ctx *context.Context, gitCommit string) ([]byte, error) {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return []byte{}, nil
	}
	url := "/api/v1/banner?commit_hash=" + gitCommit + "&user_id=" + data.UserId(config.UserSecret) + "&device_id=" + config.DeviceId + "&version=" + Version + "&forced_banner=" + os.Getenv("FORCED_BANNER")
	return ApiGet(url)
}

func tweakConfigForTests(configContents string) (string, error) {
	madeSubstitution := false
	skipLineIndex := -1
	ret := ""
	split := strings.Split(configContents, "\n")
	for i, line := range split {
		if strings.Contains(line, "# Background Run") {
			ret += strings.ReplaceAll(split[i+1], "# hishtory", "hishtory")
			madeSubstitution = true
			skipLineIndex = i + 1
		} else if i == skipLineIndex {
			continue
		} else {
			ret += line
		}
		ret += "\n"
	}
	if !madeSubstitution {
		return "", fmt.Errorf("failed to find substitution line in configConents=%#v", configContents)
	}
	return ret, nil
}

func parseTimeGenerously(input string) (time.Time, error) {
	input = strings.ReplaceAll(input, "_", " ")
	return dateparse.ParseLocal(input)
}

func MakeWhereQueryFromSearch(ctx *context.Context, db *gorm.DB, query string) (*gorm.DB, error) {
	tokens, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %v", err)
	}
	tx := db.Model(&data.HistoryEntry{}).Where("true")
	for _, token := range tokens {
		if strings.HasPrefix(token, "-") {
			if strings.Contains(token, ":") {
				query, v1, v2, err := parseAtomizedToken(ctx, token[1:])
				if err != nil {
					return nil, err
				}
				tx = tx.Where("NOT "+query, v1, v2)
			} else {
				query, v1, v2, v3, err := parseNonAtomizedToken(token[1:])
				if err != nil {
					return nil, err
				}
				tx = tx.Where("NOT "+query, v1, v2, v3)
			}
		} else if strings.Contains(token, ":") {
			query, v1, v2, err := parseAtomizedToken(ctx, token)
			if err != nil {
				return nil, err
			}
			tx = tx.Where(query, v1, v2)
		} else {
			query, v1, v2, v3, err := parseNonAtomizedToken(token)
			if err != nil {
				return nil, err
			}
			tx = tx.Where(query, v1, v2, v3)
		}
	}
	return tx, nil
}

func Search(ctx *context.Context, db *gorm.DB, query string, limit int) ([]*data.HistoryEntry, error) {
	if ctx == nil && query != "" {
		return nil, fmt.Errorf("lib.Search called with a nil context and a non-empty query (this should never happen)")
	}

	tx, err := MakeWhereQueryFromSearch(ctx, db, query)
	if err != nil {
		return nil, err
	}
	tx = tx.Order("end_time DESC")
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	var historyEntries []*data.HistoryEntry
	result := tx.Find(&historyEntries)
	if result.Error != nil {
		return nil, fmt.Errorf("DB query error: %v", result.Error)
	}
	return historyEntries, nil
}

func parseNonAtomizedToken(token string) (string, interface{}, interface{}, interface{}, error) {
	wildcardedToken := "%" + token + "%"
	return "(command LIKE ? OR hostname LIKE ? OR current_working_directory LIKE ?)", wildcardedToken, wildcardedToken, wildcardedToken, nil
}

func parseAtomizedToken(ctx *context.Context, token string) (string, interface{}, interface{}, error) {
	splitToken := strings.SplitN(token, ":", 2)
	field := splitToken[0]
	val := splitToken[1]
	switch field {
	case "user":
		return "(local_username = ?)", val, nil, nil
	case "host":
		fallthrough
	case "hostname":
		return "(instr(hostname, ?) > 0)", val, nil, nil
	case "cwd":
		return "(instr(current_working_directory, ?) > 0 OR instr(REPLACE(current_working_directory, '~/', home_directory), ?) > 0)", strings.TrimSuffix(val, "/"), strings.TrimSuffix(val, "/"), nil
	case "exit_code":
		return "(exit_code = ?)", val, nil, nil
	case "before":
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse before:%s as a timestamp: %v", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) < ?)", t.Unix(), nil, nil
	case "after":
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse after:%s as a timestamp: %v", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) > ?)", t.Unix(), nil, nil
	default:
		knownCustomColumns := make([]string, 0)
		// Get custom columns that are defined on this machine
		conf := hctx.GetConf(ctx)
		for _, c := range conf.CustomColumns {
			knownCustomColumns = append(knownCustomColumns, c.ColumnName)
		}
		// Also get all ones that are in the DB
		names, err := getAllCustomColumnNames(ctx)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to get custom column names from the DB: %v", err)
		}
		knownCustomColumns = append(knownCustomColumns, names...)
		// Check if the atom is for a custom column that exists and if it isn't, return an error
		isCustomColumn := false
		for _, ccName := range knownCustomColumns {
			if ccName == field {
				isCustomColumn = true
			}
		}
		if !isCustomColumn {
			return "", nil, nil, fmt.Errorf("search query contains unknown search atom %s", field)
		}
		// Build the where clause for the custom column
		return "EXISTS (SELECT 1 FROM json_each(custom_columns) WHERE json_extract(value, '$.name') = ? and instr(json_extract(value, '$.value'), ?) > 0)", field, val, nil
	}
}

func getAllCustomColumnNames(ctx *context.Context) ([]string, error) {
	db := hctx.GetDb(ctx)
	query := `
	SELECT DISTINCT json_extract(value, '$.name') as cc_name
	FROM history_entries 
	JOIN json_each(custom_columns)
	WHERE value IS NOT NULL
	LIMIT 10`
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	ccNames := make([]string, 0)
	for rows.Next() {
		var ccName string
		err = rows.Scan(&ccName)
		if err != nil {
			return nil, err
		}
		ccNames = append(ccNames, ccName)
	}
	return ccNames, nil
}

func tokenize(query string) ([]string, error) {
	if query == "" {
		return []string{}, nil
	}
	return strings.Split(query, " "), nil
}

func stripLines(filePath, lines string) error {
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		// File does not exist, nothing to do
		return nil
	}
	origContents, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	linesToBeRemoved := make(map[string]bool, 0)
	for _, line := range strings.Split(lines, "\n") {
		if strings.TrimSpace(line) != "" {
			linesToBeRemoved[line] = true
		}
	}
	ret := ""
	for _, line := range strings.Split(string(origContents), "\n") {
		if !linesToBeRemoved[line] {
			ret += line
			ret += "\n"
		}
	}
	return os.WriteFile(filePath, []byte(ret), 0644)
}

func Uninstall(ctx *context.Context) error {
	homedir := hctx.GetHome(ctx)
	err := stripLines(path.Join(homedir, ".bashrc"), getBashConfigFragment(homedir))
	if err != nil {
		return err
	}
	err = stripLines(getZshRcPath(homedir), getZshConfigFragment(homedir))
	if err != nil {
		return err
	}
	err = stripLines(path.Join(homedir, ".config/fish/config.fish"), getFishConfigFragment(homedir))
	if err != nil {
		return err
	}
	err = os.RemoveAll(path.Join(homedir, ".hishtory"))
	if err != nil {
		return err
	}
	fmt.Println("Successfully uninstalled hishtory, please restart your terminal...")
	return nil
}
