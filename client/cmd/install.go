package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	offlineInit            *bool
	forceInit              *bool
	offlineInstall         *bool
	skipConfigModification *bool
)

var installCmd = &cobra.Command{
	Use:    "install",
	Hidden: true,
	Short:  "Copy this binary to ~/.hishtory/ and configure your shell to use it for recording your shell history",
	Args:   cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		secretKey := ""
		if len(args) > 0 {
			secretKey = args[0]
		}
		lib.CheckFatalError(install(secretKey, *offlineInstall, *skipConfigModification))
		if os.Getenv("HISHTORY_SKIP_INIT_IMPORT") == "" {
			db, err := hctx.OpenLocalSqliteDb()
			lib.CheckFatalError(err)
			count, err := lib.CountStoredEntries(db)
			lib.CheckFatalError(err)
			if count < 10 {
				fmt.Println("Importing existing shell history...")
				ctx := hctx.MakeContext()
				numImported, err := lib.ImportHistory(ctx, false, false)
				lib.CheckFatalError(err)
				if numImported > 0 {
					fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
				}
			}
		}
		lib.CheckFatalError(warnIfUnsupportedBashVersion())
	},
}

var initCmd = &cobra.Command{
	Use:     "init",
	Short:   "Re-initialize hiSHtory with a specified secret key",
	GroupID: GROUP_ID_CONFIG,
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		db, err := hctx.OpenLocalSqliteDb()
		lib.CheckFatalError(err)
		count, err := lib.CountStoredEntries(db)
		lib.CheckFatalError(err)
		if count > 0 && !(*forceInit) {
			fmt.Printf("Your current hishtory profile has saved history entries, are you sure you want to run `init` and reset?\nNote: This won't clear any imported history entries from your existing shell\n[y/N]")
			reader := bufio.NewReader(os.Stdin)
			resp, err := reader.ReadString('\n')
			lib.CheckFatalError(err)
			if strings.TrimSpace(resp) != "y" {
				fmt.Printf("Aborting init per user response of %#v\n", strings.TrimSpace(resp))
				return
			}
		}
		secretKey := ""
		if len(args) > 0 {
			secretKey = args[0]
		}
		lib.CheckFatalError(setup(secretKey, *offlineInit))
		if os.Getenv("HISHTORY_SKIP_INIT_IMPORT") == "" {
			fmt.Println("Importing existing shell history...")
			ctx := hctx.MakeContext()
			numImported, err := lib.ImportHistory(ctx, false, false)
			lib.CheckFatalError(err)
			if numImported > 0 {
				fmt.Printf("Imported %v history entries from your existing shell history\n", numImported)
			}
		}
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Completely uninstall hiSHtory and remove your shell history",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		fmt.Printf("Are you sure you want to uninstall hiSHtory and delete all locally saved history data [y/N]")
		reader := bufio.NewReader(os.Stdin)
		resp, err := reader.ReadString('\n')
		lib.CheckFatalError(err)
		if strings.TrimSpace(resp) != "y" {
			fmt.Printf("Aborting uninstall per user response of %#v\n", strings.TrimSpace(resp))
			return
		}
		fmt.Printf("Do you have any feedback on why you're uninstallying hiSHtory? Type any feedback and then hit enter.\nFeedback: ")
		feedbackTxt, err := reader.ReadString('\n')
		lib.CheckFatalError(err)
		feedback := shared.Feedback{
			Date:     time.Now(),
			Feedback: feedbackTxt,
			UserId:   data.UserId(hctx.GetConf(ctx).UserSecret),
		}
		reqBody, err := json.Marshal(feedback)
		lib.CheckFatalError(err)
		_, _ = lib.ApiPost(ctx, "/api/v1/feedback", "application/json", reqBody)
		lib.CheckFatalError(uninstall(ctx))
		_, err = lib.ApiPost(ctx, "/api/v1/uninstall?user_id="+data.UserId(hctx.GetConf(ctx).UserSecret)+"&device_id="+hctx.GetConf(ctx).DeviceId, "application/json", []byte{})
		if err == nil {
			fmt.Println("Successfully uninstalled hishtory, please restart your terminal...")
		} else {
			fmt.Printf("Uninstall completed, but received server error: %v", err)
		}
	},
}

func warnIfUnsupportedBashVersion() error {
	_, err := exec.LookPath("bash")
	if err != nil {
		// bash is not installed, do nothing
		return nil
	}
	cmd := exec.Command("bash", "--version")
	bashVersion, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check bash version: %w", err)
	}
	if strings.Contains(string(bashVersion), "version 3.") {
		fmt.Printf("Warning: Your current bash version does not support overriding control-r. Please upgrade to at least bash 5 to enable the control-r integration.\n")
	}
	return nil
}

func install(secretKey string, offline, skipConfigModification bool) error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %w", err)
	}
	err = hctx.MakeHishtoryDir()
	if err != nil {
		return err
	}
	path, err := installBinary(homedir)
	if err != nil {
		return err
	}
	err = configureBashrc(homedir, path, skipConfigModification)
	if err != nil {
		return err
	}
	err = configureZshrc(homedir, path, skipConfigModification)
	if err != nil {
		return err
	}
	err = configureFish(homedir, path, skipConfigModification)
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
		return setup(secretKey, offline)
	}
	// TODO: Only trigger this if the version is old enough
	err = handleDbUpgrades(hctx.MakeContext())
	if err != nil {
		return err
	}
	return nil
}

// Handles people running `hishtory update` when the DB needs updating.
func handleDbUpgrades(ctx context.Context) error {
	db := hctx.GetDb(ctx)
	return lib.RetryingDbFunction(func() error {
		return db.Exec(`UPDATE history_entries SET entry_id = lower(hex(randomblob(12))) WHERE entry_id IS NULL`).Error
	})
}

// Handles people running `hishtory update` from an old version of hishtory that
// doesn't support certain config options that we now default to true. This ensures
// that we can customize the behavior for upgrades while still respecting the option
// if  someone has it explicitly set.
func handleUpgradedFeatures() error {
	configContents, err := hctx.GetConfigContents()
	if err != nil {
		// No config, so this is a new install and thus there is nothing to do
		return nil
	}
	config, err := hctx.GetConfig()
	if err != nil {
		return err
	}
	if !strings.Contains(string(configContents), "enable_control_r_search") {
		// control-r search is not yet configured, so enable it
		config.ControlRSearchEnabled = true
	}
	if !strings.Contains(string(configContents), "highlight_matches") {
		// highlighting is not yet configured, so enable it
		config.HighlightMatches = true
	}
	if !strings.Contains(string(configContents), "enable_presaving") {
		// Presaving is not yet configured, so enable it
		config.EnablePresaving = true
	}
	if !strings.Contains(string(configContents), "ai_completion") {
		// AI completion is not yet configured, disable it for upgrades since this is a new feature
		config.AiCompletion = false
	}
	return hctx.SetConfig(&config)
}

func installBinary(homedir string) (string, error) {
	clientPath, err := exec.LookPath("hishtory")
	if err != nil {
		clientPath = path.Join(homedir, data.GetHishtoryPath(), "hishtory")
	}
	if _, err := os.Stat(clientPath); err == nil {
		err = syscall.Unlink(clientPath)
		if err != nil {
			return "", fmt.Errorf("failed to unlink %s for install: %w", clientPath, err)
		}
	}
	err = copyFile(os.Args[0], clientPath)
	if err != nil {
		return "", fmt.Errorf("failed to copy hishtory binary to $PATH: %w", err)
	}
	err = os.Chmod(clientPath, 0o700)
	if err != nil {
		return "", fmt.Errorf("failed to set permissions on hishtory binary: %w", err)
	}
	return clientPath, nil
}

func getFishConfigPath(homedir string) string {
	return path.Join(homedir, data.GetHishtoryPath(), "config.fish")
}

func configureFish(homedir, binaryPath string, skipConfigModification bool) error {
	// Check if fish is installed
	_, err := exec.LookPath("fish")
	if err != nil {
		return nil
	}
	// Create the file we're going to source. Do this no matter what in case there are updates to it.
	configContents := lib.ConfigFishContents
	if os.Getenv("HISHTORY_TEST") != "" {
		testConfig, err := tweakConfigForTests(configContents)
		if err != nil {
			return err
		}
		configContents = testConfig
	}
	err = os.WriteFile(getFishConfigPath(homedir), []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.fish file: %w", err)
	}
	// Check if we need to configure the fishrc
	fishIsConfigured, err := isFishConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.config/fish/config.fish: %w", err)
	}
	if fishIsConfigured {
		return nil
	}
	// Add to fishrc
	if _, err := exec.LookPath("fish"); err != nil && skipConfigModification {
		// fish is not installed, so avoid prompting the user to configure fish
		return nil
	}
	err = os.MkdirAll(path.Join(homedir, ".config/fish"), 0o744)
	if err != nil {
		return fmt.Errorf("failed to create fish config directory: %w", err)
	}
	return addToShellConfig(path.Join(homedir, ".config/fish/config.fish"), getFishConfigFragment(homedir), skipConfigModification)
}

func getFishConfigFragment(homedir string) string {
	return "\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, data.GetHishtoryPath()) + "\"\nsource " + getFishConfigPath(homedir) + "\n"
}

func isFishConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".config/fish/config.fish"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	fishConfig, err := os.ReadFile(path.Join(homedir, ".config/fish/config.fish"))
	if err != nil {
		return false, fmt.Errorf("failed to read ~/.config/fish/config.fish: %w", err)
	}
	return strings.Contains(string(fishConfig), getFishConfigFragment(homedir)), nil
}

func getZshConfigPath(homedir string) string {
	return path.Join(homedir, data.GetHishtoryPath(), "config.zsh")
}

func configureZshrc(homedir, binaryPath string, skipConfigModification bool) error {
	// Create the file we're going to source in our zshrc. Do this no matter what in case there are updates to it.
	configContents := lib.ConfigZshContents
	if os.Getenv("HISHTORY_TEST") != "" {
		testConfig, err := tweakConfigForTests(configContents)
		if err != nil {
			return err
		}
		configContents = testConfig
	}
	err := os.WriteFile(getZshConfigPath(homedir), []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.zsh file: %w", err)
	}
	// Check if we need to configure the zshrc
	zshIsConfigured, err := isZshConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check .zshrc: %w", err)
	}
	if zshIsConfigured {
		return nil
	}
	// Add to zshrc
	return addToShellConfig(getZshRcPath(homedir), getZshConfigFragment(homedir), skipConfigModification)
}

func getZshRcPath(homedir string) string {
	if zdotdir := os.Getenv("ZDOTDIR"); zdotdir != "" {
		return path.Join(zdotdir, ".zshrc")
	}
	return path.Join(homedir, ".zshrc")
}

func getZshConfigFragment(homedir string) string {
	return "\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, data.GetHishtoryPath()) + "\"\nsource " + getZshConfigPath(homedir) + "\n"
}

func isZshConfigured(homedir string) (bool, error) {
	_, err := os.Stat(getZshRcPath(homedir))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := os.ReadFile(getZshRcPath(homedir))
	if err != nil {
		return false, fmt.Errorf("failed to read zshrc: %w", err)
	}
	return strings.Contains(string(bashrc), getZshConfigFragment(homedir)), nil
}

func getBashConfigPath(homedir string) string {
	return path.Join(homedir, data.GetHishtoryPath(), "config.sh")
}

func configureBashrc(homedir, binaryPath string, skipConfigModification bool) error {
	// Create the file we're going to source in our bashrc. Do this no matter what in case there are updates to it.
	configContents := lib.ConfigShContents
	if os.Getenv("HISHTORY_TEST") != "" {
		testConfig, err := tweakConfigForTests(configContents)
		if err != nil {
			return err
		}
		configContents = testConfig
	}
	err := os.WriteFile(getBashConfigPath(homedir), []byte(configContents), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config.sh file: %w", err)
	}
	// Check if we need to configure the bashrc and configure it if so
	bashRcIsConfigured, err := isBashRcConfigured(homedir)
	if err != nil {
		return fmt.Errorf("failed to check ~/.bashrc: %w", err)
	}
	if !bashRcIsConfigured {
		err = addToShellConfig(path.Join(homedir, ".bashrc"), getBashConfigFragment(homedir), skipConfigModification)
		if err != nil {
			return err
		}
	}
	// Check if we need to configure the bash_profile and configure it if so
	if doesBashProfileNeedConfig(homedir) {
		bashProfileIsConfigured, err := isBashProfileConfigured(homedir)
		if err != nil {
			return fmt.Errorf("failed to check ~/.bash_profile: %w", err)
		}
		if !bashProfileIsConfigured {
			err = addToShellConfig(path.Join(homedir, ".bash_profile"), getBashConfigFragment(homedir), skipConfigModification)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func addToShellConfig(shellConfigPath, configFragment string, skipConfigModification bool) error {
	if skipConfigModification {
		fmt.Printf("Please edit %q to add:\n\n```\n%s\n```\n\n", convertToRelativePath(shellConfigPath), strings.TrimSpace(configFragment))
		return nil
	}
	f, err := os.OpenFile(shellConfigPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to append to %s: %w", shellConfigPath, err)
	}
	defer f.Close()
	_, err = f.WriteString(configFragment)
	if err != nil {
		return fmt.Errorf("failed to append to %s: %w", shellConfigPath, err)
	}
	return nil
}

func convertToRelativePath(path string) string {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, homedir) {
		return strings.Replace(path, homedir, "~", 1)
	}
	return path
}

func getBashConfigFragment(homedir string) string {
	return "\n# Hishtory Config:\nexport PATH=\"$PATH:" + path.Join(homedir, data.GetHishtoryPath()) + "\"\nsource " + getBashConfigPath(homedir) + "\n"
}

func isBashRcConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".bashrc"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := os.ReadFile(path.Join(homedir, ".bashrc"))
	if err != nil {
		return false, fmt.Errorf("failed to read bashrc: %w", err)
	}
	return strings.Contains(string(bashrc), getBashConfigFragment(homedir)), nil
}

func doesBashProfileNeedConfig(homedir string) bool {
	if runtime.GOOS == "darwin" {
		// Darwin always needs it configured for #14
		return true
	}
	if runtime.GOOS == "linux" {
		// Only configure it on linux if .bash_profile already exists
		_, err := os.Stat(path.Join(homedir, ".bash_profile"))
		return !errors.Is(err, os.ErrNotExist)
	}
	// Default to not configuring it
	return false
}

func isBashProfileConfigured(homedir string) (bool, error) {
	_, err := os.Stat(path.Join(homedir, ".bash_profile"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	bashrc, err := os.ReadFile(path.Join(homedir, ".bash_profile"))
	if err != nil {
		return false, fmt.Errorf("failed to read bash_profile: %w", err)
	}
	return strings.Contains(string(bashrc), getBashConfigFragment(homedir)), nil
}

func tweakConfigForTests(configContents string) (string, error) {
	substitutionCount := 0
	removedCount := 0
	ret := ""
	split := strings.Split(configContents, "\n")
	for i, line := range split {
		if strings.Contains(line, "# Background Run") {
			ret += strings.ReplaceAll(split[i+1], "# hishtory", "hishtory")
			substitutionCount += 1
		} else if strings.Contains(line, "# Foreground Run") {
			removedCount += 1
			continue
		} else {
			ret += line
		}
		ret += "\n"
	}
	if !(substitutionCount == 2 && removedCount == 2) {
		return "", fmt.Errorf("failed to find substitution line in configContents=%#v", configContents)
	}
	return ret, nil
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

func uninstall(ctx context.Context) error {
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
	err = os.RemoveAll(path.Join(homedir, data.GetHishtoryPath()))
	if err != nil {
		return err
	}
	return nil
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
	return os.WriteFile(filePath, []byte(ret), 0o644)
}

func setup(userSecret string, isOffline bool) error {
	if userSecret == "" {
		userSecret = uuid.Must(uuid.NewRandom()).String()
	}
	fmt.Println("Setting secret hishtory key to " + string(userSecret))

	// Create and set the config with the defaults that we want for new installs
	var config hctx.ClientConfig
	config.UserSecret = userSecret
	config.IsEnabled = true
	config.DeviceId = uuid.Must(uuid.NewRandom()).String()
	config.ControlRSearchEnabled = true
	config.HighlightMatches = true
	config.AiCompletion = true
	config.IsOffline = isOffline
	if isOffline {
		// By default, offline mode disables AI completion. Users can still enable it if they want it. See #220.
		config.AiCompletion = false
	}
	config.EnablePresaving = true
	err := hctx.SetConfig(&config)
	if err != nil {
		return fmt.Errorf("failed to persist config to disk: %w", err)
	}

	// Drop all existing data
	db, err := hctx.OpenLocalSqliteDb()
	if err != nil {
		return err
	}
	err = db.Exec("DELETE FROM history_entries").Error
	if err != nil {
		return fmt.Errorf("failed to reset local DB during setup: %w", err)
	}

	// Bootstrap from remote data
	if config.IsOffline {
		return nil
	}
	return registerAndBootstrapDevice(hctx.MakeContext(), &config, db, userSecret)
}

func registerAndBootstrapDevice(ctx context.Context, config *hctx.ClientConfig, db *gorm.DB, userSecret string) error {
	registerPath := "/api/v1/register?user_id=" + data.UserId(userSecret) + "&device_id=" + config.DeviceId
	if isIntegrationTestDevice() {
		registerPath += "&is_integration_test_device=true"
	}
	_, err := lib.ApiGet(ctx, registerPath)
	if err != nil {
		return fmt.Errorf("failed to register device with backend: %w", err)
	}

	respBody, err := lib.ApiGet(ctx, "/api/v1/bootstrap?user_id="+data.UserId(userSecret)+"&device_id="+config.DeviceId)
	if err != nil {
		return fmt.Errorf("failed to bootstrap device from the backend: %w", err)
	}
	var retrievedEntries []*shared.EncHistoryEntry
	err = json.Unmarshal(respBody, &retrievedEntries)
	if err != nil {
		return fmt.Errorf("failed to load JSON response: %w", err)
	}
	hctx.GetLogger().Infof("Bootstrapping new device: Found %d entries", len(retrievedEntries))
	for _, entry := range retrievedEntries {
		decEntry, err := data.DecryptHistoryEntry(userSecret, *entry)
		if err != nil {
			return fmt.Errorf("failed to decrypt history entry from server: %w", err)
		}
		lib.AddToDbIfNew(db, decEntry)
	}

	return nil
}

func isIntegrationTestDevice() bool {
	if os.Getenv("HISHTORY_TEST") != "" {
		return true
	}
	if os.Getenv("GITHUB_ACTION_REPOSITORY") == "ddworken/hishtory" {
		return true
	}
	return false
}

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(uninstallCmd)

	offlineInit = initCmd.Flags().Bool("offline", false, "Install hiSHtory in offline mode wiht all syncing capabilities disabled")
	forceInit = initCmd.Flags().Bool("force", false, "Force re-init without any prompts")
	offlineInstall = installCmd.Flags().Bool("offline", false, "Install hiSHtory in offline mode with all syncing capabilities disabled")
	skipConfigModification = installCmd.Flags().Bool("skip-config-modification", false, "Skip modifying shell configs and instead instruct the user on how to modify their configs")
}
