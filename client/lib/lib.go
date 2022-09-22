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

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/rodaine/table"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"
)

//go:embed config.sh
var ConfigShContents string

//go:embed test_config.sh
var TestConfigShContents string

//go:embed config.zsh
var ConfigZshContents string

//go:embed test_config.zsh
var TestConfigZshContents string

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
		hctx.GetLogger().Printf("BuildHistoryEntry called with args=%#v, which has too few entries! This can happen in specific edge cases for newly opened terminals and is likely not a problem.", args)
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
	} else if shell == "zsh" {
		cmd := strings.TrimSuffix(strings.TrimSuffix(args[4], "\n"), " ")
		if strings.HasPrefix(cmd, " ") {
			// Don't save commands that start with a space
			return nil, nil
		}
		entry.Command = cmd
	} else {
		return nil, fmt.Errorf("tried to save a hishtory entry from an unsupported shell=%#v", shell)
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

	return &entry, nil
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
	return strings.SplitN(strings.SplitN(strings.TrimSpace(history), " ", 2)[1], " ", 2)[1], nil
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
	if len(args) > 2 && args[2] != "" {
		userSecret = args[2]
	}
	fmt.Println("Setting secret hishtory key to " + string(userSecret))

	// Create and set the config
	var config hctx.ClientConfig
	config.UserSecret = userSecret
	config.IsEnabled = true
	config.DeviceId = uuid.Must(uuid.NewRandom()).String()
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
	}
}

func DisplayResults(results []*data.HistoryEntry) {
	headerFmt := color.New(color.FgGreen, color.Underline).SprintfFunc()
	tbl := table.New("Hostname", "CWD", "Timestamp", "Runtime", "Exit Code", "Command")
	tbl.WithHeaderFormatter(headerFmt)

	for _, result := range results {
		timestamp := result.StartTime.Format("Jan 2 2006 15:04:05 MST")
		duration := result.EndTime.Sub(result.StartTime).Round(time.Millisecond).String()
		tbl.AddRow(result.Hostname, result.CurrentWorkingDirectory, timestamp, duration, result.ExitCode, result.Command)
	}

	tbl.Print()
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

func ImportHistory(ctx *context.Context) (int, error) {
	config := hctx.GetConf(ctx)
	if config.HaveCompletedInitialImport {
		// Don't run an import if we already have run one. This avoids importing the same entry multiple times.
		return 0, nil
	}
	homedir := hctx.GetHome(ctx)
	historyEntries, err := parseBashHistory(homedir)
	if err != nil {
		return 0, fmt.Errorf("failed to parse bash history: %v", err)
	}
	extraEntries, err := parseZshHistory(homedir)
	if err != nil {
		return 0, fmt.Errorf("failed to parse zsh history: %v", err)
	}
	historyEntries = append(historyEntries, extraEntries...)
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
		entry := data.HistoryEntry{
			LocalUsername:           currentUser.Name,
			Hostname:                hostname,
			Command:                 cmd,
			CurrentWorkingDirectory: "Unknown",
			HomeDirectory:           homedir,
			ExitCode:                0, // Unknown, but assumed
			StartTime:               time.Now(),
			EndTime:                 time.Now(),
			DeviceId:                config.DeviceId,
		}
		err = ReliableDbCreate(db, entry)
		if err != nil {
			return 0, fmt.Errorf("failed to insert imported history entry: %v", err)
		}
	}
	config.HaveCompletedInitialImport = true
	err = hctx.SetConfig(config)
	if err != nil {
		return 0, fmt.Errorf("failed to mark initial import as completed, this may lead to duplicate history entries: %v", err)
	}
	return len(historyEntries), nil
}

func parseBashHistory(homedir string) ([]string, error) {
	return readFileToArray(filepath.Join(homedir, ".bash_history"))
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

func parseZshHistory(homedir string) ([]string, error) {
	histfile := os.Getenv("HISTFILE")
	if histfile == "" {
		histfile = filepath.Join(homedir, ".zsh_history")
	}
	return readFileToArray(histfile)
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
	_, err = hctx.GetConfig()
	if err != nil {
		// No config, so set up a new installation
		return Setup(os.Args)
	}
	return nil
}

func configureZshrc(homedir, binaryPath string) error {
	// Create the file we're going to source in our zshrc. Do this no matter what in case there are updates to it.
	zshConfigPath := path.Join(homedir, shared.HISHTORY_PATH, "config.zsh")
	configContents := ConfigZshContents
	if os.Getenv("HISHTORY_TEST") != "" {
		configContents = TestConfigZshContents
	}
	err := ioutil.WriteFile(zshConfigPath, []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.zsh file: %v", err)
	}
	// Check if we need to configure the zshrc
	zshIsConfigured, err := isZshConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.zshrc: %v", err)
	}
	if zshIsConfigured {
		return nil
	}
	// Add to zshrc
	f, err := os.OpenFile(path.Join(homedir, ".zshrc"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to append to zshrc: %v", err)
	}
	defer f.Close()
	_, err = f.WriteString("\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, shared.HISHTORY_PATH) + "\"\nsource " + zshConfigPath + "\n")
	if err != nil {
		return fmt.Errorf("failed to append to zshrc: %v", err)
	}
	return nil
}

func isZshConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".zshrc"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := ioutil.ReadFile(path.Join(homedir, ".zshrc"))
	if err != nil {
		return false, fmt.Errorf("failed to read zshrc: %v", err)
	}
	return strings.Contains(string(bashrc), "# Hishtory Config:"), nil
}

func configureBashrc(homedir, binaryPath string) error {
	// Create the file we're going to source in our bashrc. Do this no matter what in case there are updates to it.
	bashConfigPath := path.Join(homedir, shared.HISHTORY_PATH, "config.sh")
	configContents := ConfigShContents
	if os.Getenv("HISHTORY_TEST") != "" {
		configContents = TestConfigShContents
	}
	err := ioutil.WriteFile(bashConfigPath, []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.sh file: %v", err)
	}
	// Check if we need to configure the bashrc
	bashIsConfigured, err := isBashConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.bashrc: %v", err)
	}
	if bashIsConfigured {
		return nil
	}
	// Add to bashrc
	f, err := os.OpenFile(path.Join(homedir, ".bashrc"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to append to bashrc: %v", err)
	}
	defer f.Close()
	_, err = f.WriteString("\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, shared.HISHTORY_PATH) + "\"\nsource " + bashConfigPath + "\n")
	if err != nil {
		return fmt.Errorf("failed to append to bashrc: %v", err)
	}
	return nil
}

func isBashConfigured(homedir string) (bool, error) {
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

func installBinary(homedir string) (string, error) {
	clientPath, err := exec.LookPath("hishtory")
	if err != nil {
		clientPath = path.Join(homedir, shared.HISHTORY_PATH, "hishtory")
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
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
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

func Update() error {
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
	if runtime.GOOS == "darwin" {
		err = verifyBinaryMac("/tmp/hishtory-client", downloadData)
	} else {
		err = verifyBinary("/tmp/hishtory-client", "/tmp/hishtory-client.intoto.jsonl", downloadData.Version)
	}
	if err != nil {
		return fmt.Errorf("failed to verify SLSA provenance of the updated binary, aborting update (to bypass, set `export HISHTORY_DISABLE_SLSA_ATTESTATION=true`): %v", err)
	}

	// Unlink the existing binary so we can overwrite it even though it is still running
	if runtime.GOOS == "linux" {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user's home directory: %v", err)
		}
		err = syscall.Unlink(path.Join(homedir, shared.HISHTORY_PATH, "hishtory"))
		if err != nil {
			return fmt.Errorf("failed to unlink %s for update: %v", path.Join(homedir, shared.HISHTORY_PATH, "hishtory"), err)
		}
	}

	// Install the new one
	cmd := exec.Command("chmod", "+x", "/tmp/hishtory-client")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to chmod +x the update (out=%#v, err=%#v): %v", stdout.String(), stderr.String(), err)
	}
	cmd = exec.Command("/tmp/hishtory-client", "install")
	stdout = bytes.Buffer{}
	stderr = bytes.Buffer{}
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to install update (out=%#v, err=%#v): %v", stdout.String(), stderr.String(), err)
	}
	fmt.Printf("Successfully updated hishtory from v0.%s to %s\n", Version, downloadData.Version)
	return nil
}

func verifyBinaryMac(binaryPath string, downloadData shared.UpdateInfo) error {
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
	return verifyBinary(unsignedBinaryPath, "/tmp/hishtory-client.intoto.jsonl", downloadData.Version)
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
	logger := hctx.GetLogger()
	for _, d := range differences {
		logger.Printf("comparing binaries: %#v\n", d)
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
	err := downloadFile("/tmp/hishtory-client", clientUrl)
	if err != nil {
		return err
	}
	err = downloadFile("/tmp/hishtory-client.intoto.jsonl", clientProvenanceUrl)
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

func ApiGet(path string) ([]byte, error) {
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return nil, fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}
	start := time.Now()
	resp, err := http.Get(getServerHostname() + path)
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
	hctx.GetLogger().Printf("ApiGet(%#v): %s\n", path, duration.String())
	return respBody, nil
}

func ApiPost(path, contentType string, data []byte) ([]byte, error) {
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return nil, fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}
	start := time.Now()
	resp, err := http.Post(getServerHostname()+path, contentType, bytes.NewBuffer(data))
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
	hctx.GetLogger().Printf("ApiPost(%#v): %s\n", path, duration.String())
	return respBody, nil
}

func IsOfflineError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "dial tcp: lookup api.hishtory.dev") || strings.Contains(err.Error(), "read: connection reset by peer") || strings.Contains(err.Error(), ": EOF")
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
			return err
		}
		if err != nil && err.Error() != "database is locked (5) (SQLITE_BUSY)" {
			return err
		}
	}
	return fmt.Errorf("failed to create DB entry even with %d retries: %v", i, err)
}

func EncryptAndMarshal(config hctx.ClientConfig, entry *data.HistoryEntry) ([]byte, error) {
	encEntry, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt history entry")
	}
	encEntry.DeviceId = config.DeviceId
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	if err != nil {
		return jsonValue, fmt.Errorf("failed to marshal encrypted history entry: %v", err)
	}
	return jsonValue, nil
}

func Redact(ctx *context.Context, query string, force bool) error {
	tx, err := data.MakeWhereQueryFromSearch(hctx.GetDb(ctx), query)
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
		// TODO: Find a way to test the prompting
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
	tx, err = data.MakeWhereQueryFromSearch(hctx.GetDb(ctx), query)
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
