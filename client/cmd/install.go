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
	"github.com/spf13/cobra"
)

var offlineInit *bool
var offlineInstall *bool

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
		lib.CheckFatalError(install(secretKey, *offlineInstall))
		if os.Getenv("HISHTORY_SKIP_INIT_IMPORT") == "" {
			db, err := hctx.OpenLocalSqliteDb()
			lib.CheckFatalError(err)
			var count int64
			lib.CheckFatalError(db.Model(&data.HistoryEntry{}).Count(&count).Error)
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
		var count int64
		lib.CheckFatalError(db.Model(&data.HistoryEntry{}).Count(&count).Error)
		if count > 0 {
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
		lib.CheckFatalError(lib.Setup(secretKey, *offlineInit))
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
		_, _ = lib.ApiPost("/api/v1/feedback", "application/json", reqBody)
		lib.CheckFatalError(uninstall(ctx))
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

func install(secretKey string, offline bool) error {
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
		return lib.Setup(secretKey, offline)
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

func configureFish(homedir, binaryPath string) error {
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
		return fmt.Errorf("failed to write config.zsh file: %w", err)
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
	err = os.MkdirAll(path.Join(homedir, ".config/fish"), 0o744)
	if err != nil {
		return fmt.Errorf("failed to create fish config directory: %w", err)
	}
	return addToShellConfig(path.Join(homedir, ".config/fish/config.fish"), getFishConfigFragment(homedir))
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

func configureZshrc(homedir, binaryPath string) error {
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
	return addToShellConfig(getZshRcPath(homedir), getZshConfigFragment(homedir))
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

func configureBashrc(homedir, binaryPath string) error {
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
		err = addToShellConfig(path.Join(homedir, ".bashrc"), getBashConfigFragment(homedir))
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
			err = addToShellConfig(path.Join(homedir, ".bash_profile"), getBashConfigFragment(homedir))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func addToShellConfig(shellConfigPath, configFragment string) error {
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
	fmt.Println("Successfully uninstalled hishtory, please restart your terminal...")
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
	return os.WriteFile(filePath, []byte(ret), 0644)
}

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(uninstallCmd)

	offlineInit = initCmd.Flags().Bool("offline", false, "Install hiSHtory in offline mode wiht all syncing capabilities disabled")
	offlineInstall = installCmd.Flags().Bool("offline", false, "Install hiSHtory in offline mode wiht all syncing capabilities disabled")
}
