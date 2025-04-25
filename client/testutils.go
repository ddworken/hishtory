package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"

	"github.com/stretchr/testify/require"
)

type shellTester interface {
	RunInteractiveShell(t testing.TB, script string) string
	RunInteractiveShellRelaxed(t testing.TB, script string) (string, error)
	RunInteractiveShellBackground(t testing.TB, script string) error
	ShellName() string
}
type bashTester struct{}

func (b bashTester) RunInteractiveShell(t testing.TB, script string) string {
	out, err := b.RunInteractiveShellRelaxed(t, "set -emo pipefail\n"+script)
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		require.NoError(t, err, fmt.Sprintf("error when running command at %s:%dv", filename, line))
	}
	return out
}

func (b bashTester) RunInteractiveShellRelaxed(t testing.TB, script string) (outStr string, err error) {
	cmd := exec.Command("bash", "-i")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("unexpected error when running commands, out=%#v, err=%#v: %w", stdout.String(), stderr.String(), err)
	}
	outStr = stdout.String()
	require.NotContains(t, outStr, "hishtory fatal error", "Ran command, but hishtory had a fatal error!")
	return outStr, nil
}

func (b bashTester) RunInteractiveShellBackground(t testing.TB, script string) (err error) {
	cmd := exec.Command("bash", "-i")
	// SetSid: true is required to prevent SIGTTIN signal killing the entire test
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func (b bashTester) ShellName() string {
	return "bash"
}

type zshTester struct{}

func (z zshTester) RunInteractiveShell(t testing.TB, script string) (res string) {
	res, err := z.RunInteractiveShellRelaxed(t, "set -eo pipefail\n"+script)
	require.NoError(t, err)
	return res
}

func (z zshTester) RunInteractiveShellRelaxed(t testing.TB, script string) (outStr string, err error) {
	cmd := exec.Command("zsh", "-is")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("unexpected error when running command=%#v, out=%#v, err=%#v: %w", script, stdout.String(), stderr.String(), err)
	}
	outStr = stdout.String()
	require.NotContains(t, outStr, "hishtory fatal error")
	return outStr, nil
}

func (z zshTester) RunInteractiveShellBackground(t testing.TB, script string) (err error) {
	cmd := exec.Command("zsh", "-is")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func (z zshTester) ShellName() string {
	return "zsh"
}

type OnlineStatus int64

const (
	Online OnlineStatus = iota
	Offline
)

func assertOnlineStatus(t testing.TB, onlineStatus OnlineStatus) {
	config := hctx.GetConf(hctx.MakeContext())
	if onlineStatus == Online && config.IsOffline {
		t.Fatalf("We're supposed to be online, yet config.IsOffline=%#v (config=%#v)", config.IsOffline, config)
	}
	if onlineStatus == Offline && !config.IsOffline {
		t.Fatalf("We're supposed to be offline, yet config.IsOffline=%#v (config=%#v)", config.IsOffline, config)
	}
}

func hishtoryQuery(t testing.TB, tester shellTester, query string) string {
	return tester.RunInteractiveShell(t, "hishtory query "+query)
}

func manuallySubmitHistoryEntry(t testing.TB, userSecret string, entry data.HistoryEntry) {
	encEntry, err := data.EncryptHistoryEntry(userSecret, entry)
	require.NoError(t, err)
	if encEntry.Date != entry.EndTime {
		t.Fatalf("encEntry.Date does not match the entry")
	}
	jsonValue, err := json.Marshal([]shared.EncHistoryEntry{encEntry})
	require.NoError(t, err)
	require.NotEqual(t, "", entry.DeviceId)
	resp, err := http.Post("http://localhost:8080/api/v1/submit?source_device_id="+entry.DeviceId, "application/json", bytes.NewBuffer(jsonValue))
	require.NoError(t, err)
	if resp.StatusCode != 200 {
		t.Fatalf("failed to submit result to backend, status_code=%d", resp.StatusCode)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read resp.Body: %v", err)
	}
	submitResp := shared.SubmitResponse{}
	err = json.Unmarshal(respBody, &submitResp)
	if err != nil {
		t.Fatalf("failed to deserialize SubmitResponse: %v", err)
	}
}

func captureTerminalOutput(t testing.TB, tester shellTester, commands []string) string {
	return captureTerminalOutputWithShellName(t, tester, tester.ShellName(), commands)
}

func captureTerminalOutputWithComplexCommands(t testing.TB, tester shellTester, commands []TmuxCommand) string {
	return captureTerminalOutputWithShellNameAndDimensions(t, tester, tester.ShellName(), 200, 50, commands)
}

type TmuxCommand struct {
	Keys       string
	ResizeX    int
	ResizeY    int
	ExtraDelay float64
	NoSleep    bool
}

func captureTerminalOutputWithShellName(t testing.TB, tester shellTester, overriddenShellName string, commands []string) string {
	sCommands := make([]TmuxCommand, 0)
	for _, command := range commands {
		sCommands = append(sCommands, TmuxCommand{Keys: command})
	}
	return captureTerminalOutputWithShellNameAndDimensions(t, tester, overriddenShellName, 200, 50, sCommands)
}

func captureTerminalOutputWithShellNameAndDimensions(t testing.TB, tester shellTester, overriddenShellName string, width, height int, commands []TmuxCommand) string {
	return captureTerminalOutputComplex(t,
		TmuxCaptureConfig{
			tester:              tester,
			overriddenShellName: overriddenShellName,
			width:               width,
			height:              height,
			complexCommands:     commands,
		})
}

type TmuxCaptureConfig struct {
	tester                 shellTester
	overriddenShellName    string
	commands               []string
	complexCommands        []TmuxCommand
	width, height          int
	includeEscapeSequences bool
}

func buildTmuxInputCommands(t testing.TB, captureConfig TmuxCaptureConfig) string {
	if captureConfig.overriddenShellName == "" {
		captureConfig.overriddenShellName = captureConfig.tester.ShellName()
	}
	if captureConfig.width == 0 {
		captureConfig.width = 200
	}
	if captureConfig.height == 0 {
		captureConfig.height = 50
	}
	sleepAmount := "0.1"
	if runtime.GOOS == "linux" {
		sleepAmount = "0.2"
	}
	if captureConfig.overriddenShellName == "fish" {
		// Fish is considerably slower so this is sadly necessary
		sleepAmount = "0.5"
	}
	if testutils.IsGithubAction() {
		sleepAmount = "0.5"
	}
	fullCommand := ""
	fullCommand += " tmux kill-session -t foo || true\n"
	fullCommand += fmt.Sprintf(" tmux -u new-session -d -x %d -y %d -s foo %s\n", captureConfig.width, captureConfig.height, captureConfig.overriddenShellName)
	fullCommand += " sleep 1\n"
	if captureConfig.overriddenShellName == "bash" {
		fullCommand += " tmux send -t foo SPACE source SPACE ~/.bashrc ENTER\n"
	}
	fullCommand += " sleep " + sleepAmount + "\n"
	if len(captureConfig.commands) > 0 {
		require.Empty(t, captureConfig.complexCommands)
		for _, command := range captureConfig.commands {
			captureConfig.complexCommands = append(captureConfig.complexCommands, TmuxCommand{Keys: command})
		}
	}
	require.NotEmpty(t, captureConfig.complexCommands)
	for _, cmd := range captureConfig.complexCommands {
		if cmd.Keys != "" {
			fullCommand += " tmux send -t foo -- "
			fullCommand += cmd.Keys
			fullCommand += "\n"
		}
		if cmd.ResizeX != 0 && cmd.ResizeY != 0 {
			fullCommand += fmt.Sprintf(" tmux resize-window -t foo -x %d -y %d\n", cmd.ResizeX, cmd.ResizeY)
		}
		if cmd.ExtraDelay != 0 {
			fullCommand += fmt.Sprintf(" sleep %f\n", cmd.ExtraDelay)
		}
		if !cmd.NoSleep {
			fullCommand += " sleep " + sleepAmount + "\n"
		}
	}
	fullCommand += " sleep 2.5\n"
	if testutils.IsGithubAction() {
		fullCommand += " sleep 2.5\n"
	}
	return fullCommand
}

func captureTerminalOutputComplex(t testing.TB, captureConfig TmuxCaptureConfig) string {
	require.NotNil(t, captureConfig.tester)
	fullCommand := ""
	fullCommand += buildTmuxInputCommands(t, captureConfig)
	fullCommand += " tmux capture-pane -t foo -p"
	if captureConfig.includeEscapeSequences {
		// -e ensures that tmux runs the command in an environment that supports escape sequences. Used for rendering colors in the TUI.
		fullCommand += "e"
	}
	fullCommand += "\n"
	fullCommand += " tmux kill-session -t foo\n"
	testutils.TestLog(t, "Running tmux command: "+fullCommand)
	return strings.TrimSpace(captureConfig.tester.RunInteractiveShell(t, fullCommand))
}

func assertNoLeakedConnections(t testing.TB) {
	resp, err := lib.ApiGet(makeTestOnlyContextWithFakeConfig(), "/api/v1/get-num-connections")
	require.NoError(t, err)
	numConnections, err := strconv.Atoi(string(resp))
	require.NoError(t, err)
	if numConnections > 1 {
		t.Fatalf("DB has %d open connections, expected to have 1 or less", numConnections)
	}
}

func getPidofCommand() string {
	if runtime.GOOS == "darwin" {
		// MacOS doesn't have pidof by default
		return "pgrep"
	}
	return "pidof"
}

func makeTestOnlyContextWithFakeConfig() context.Context {
	fakeConfig := hctx.ClientConfig{
		UserSecret: "FAKE_TEST_DEVICE",
		DeviceId:   "FAKE_TEST_DEVICE",
	}
	ctx := context.Background()
	ctx = context.WithValue(ctx, hctx.ConfigCtxKey, &fakeConfig)
	// Note: We don't create a DB here
	homedir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("failed to get homedir: %w", err))
	}
	return context.WithValue(ctx, hctx.HomedirCtxKey, homedir)
}

type deviceSet struct {
	deviceMap     *map[device]deviceOp
	currentDevice *device
}

type device struct {
	key      string
	deviceId string
}

type deviceOp struct {
	backup  func()
	restore func()
}

func createDevice(t testing.TB, tester shellTester, devices *deviceSet, key, deviceId string) {
	d := device{key, deviceId}
	_, ok := (*devices.deviceMap)[d]
	if ok {
		t.Fatalf("cannot create device twice for key=%s deviceId=%s", key, deviceId)
	}
	installHishtory(t, tester, key)
	(*devices.deviceMap)[d] = deviceOp{
		backup:  func() { testutils.BackupAndRestoreWithId(t, key+deviceId) },
		restore: testutils.BackupAndRestoreWithId(t, key+deviceId),
	}
}

func switchToDevice(devices *deviceSet, d device) {
	if devices.currentDevice != nil && d == *devices.currentDevice {
		return
	}
	if devices.currentDevice != nil {
		(*devices.deviceMap)[*devices.currentDevice].backup()
	}
	devices.currentDevice = &d
	(*devices.deviceMap)[d].restore()
}

func installHishtory(t testing.TB, tester shellTester, userSecret string) string {
	out := tester.RunInteractiveShell(t, ` /tmp/client install `+userSecret)
	r := regexp.MustCompile(`Setting secret hishtory key to (.*)`)
	matches := r.FindStringSubmatch(out)
	if len(matches) != 2 {
		t.Fatalf("Failed to extract userSecret from output=%#v: matches=%#v", out, matches)
	}
	return matches[1]
}

func stripShellPrefix(out string) string {
	if strings.Contains(out, "\n\n\n") {
		return strings.TrimSpace(strings.Split(out, "\n\n\n")[1])
	}
	return out
}

func stripRequiredPrefix(t *testing.T, out, prefix string) string {
	require.Contains(t, out, prefix)
	return strings.TrimSpace(strings.SplitN(out, prefix, 2)[1])
}

func stripTuiCommandPrefix(t *testing.T, out string) string {
	return stripRequiredPrefix(t, out, "hishtory tquery")
}

// Wrap the given test so that it can be run on Github Actions with sharding. This
// makes it possible to run only 1/N tests on each of N github action jobs, speeding
// up test execution through parallelization. This is necessary since the wrapped
// integration tests rely on OS-level globals (the shell history) that can't otherwise
// be parallelized.
func wrapTestForSharding(test func(t *testing.T)) func(t *testing.T) {
	shardNumberAllocator += 1
	return func(t *testing.T) {
		testShardNumber := shardNumberAllocator
		markTestForSharding(t, testShardNumber)
		test(t)
	}
}

var shardNumberAllocator int = 0

// Returns whether this is a sharded test run. false during all normal non-github action operations.
func isShardedTestRun() bool {
	return numTestShards() != -1 && currentShardNumber() != -1
}

// Get the total number of test shards
func numTestShards() int {
	numTestShardsStr := os.Getenv("NUM_TEST_SHARDS")
	if numTestShardsStr == "" {
		return -1
	}
	numTestShards, err := strconv.Atoi(numTestShardsStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse NUM_TEST_SHARDS: %v", err))
	}
	return numTestShards
}

// Get the current shard number
func currentShardNumber() int {
	currentShardNumberStr := os.Getenv("CURRENT_SHARD_NUM")
	if currentShardNumberStr == "" {
		return -1
	}
	currentShardNumber, err := strconv.Atoi(currentShardNumberStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse CURRENT_SHARD_NUM: %v", err))
	}
	return currentShardNumber
}

// Mark the given test for sharding with the given test ID number.
func markTestForSharding(t *testing.T, testShardNumber int) {
	if isShardedTestRun() {
		if testShardNumber%numTestShards() != currentShardNumber() {
			t.Skip("Skipping sharded test")
		}
	}
}
