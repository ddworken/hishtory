package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/stretchr/testify/require"
)

var GLOBAL_STATSD *statsd.Client

type shellTester interface {
	RunInteractiveShell(t testing.TB, script string) string
	RunInteractiveShellRelaxed(t testing.TB, script string) (string, error)
	ShellName() string
}
type bashTester struct {
	shellTester
}

func (b bashTester) RunInteractiveShell(t testing.TB, script string) string {
	out, err := b.RunInteractiveShellRelaxed(t, "set -emo pipefail\n"+script)
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		t.Fatalf("error when running command at %s:%d: %v", filename, line, err)
	}
	return out
}

func (b bashTester) RunInteractiveShellRelaxed(t testing.TB, script string) (string, error) {
	cmd := exec.Command("bash", "-i")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("unexpected error when running commands, out=%#v, err=%#v: %w", stdout.String(), stderr.String(), err)
	}
	outStr := stdout.String()
	require.NotContains(t, outStr, "hishtory fatal error", "Ran command, but hishtory had a fatal error!")
	return outStr, nil
}

func (b bashTester) ShellName() string {
	return "bash"
}

type zshTester struct {
	shellTester
}

func (z zshTester) RunInteractiveShell(t testing.TB, script string) string {
	res, err := z.RunInteractiveShellRelaxed(t, "set -eo pipefail\n"+script)
	require.NoError(t, err)
	return res
}

func (z zshTester) RunInteractiveShellRelaxed(t testing.TB, script string) (string, error) {
	cmd := exec.Command("zsh", "-is")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("unexpected error when running command=%#v, out=%#v, err=%#v: %w", script, stdout.String(), stderr.String(), err)
	}
	outStr := stdout.String()
	require.NotContains(t, outStr, "hishtory fatal error")
	return outStr, nil
}

func (z zshTester) ShellName() string {
	return "zsh"
}

func runTestsWithRetries(parentT *testing.T, testName string, testFunc func(t testing.TB)) {
	numRetries := 3
	if testutils.IsGithubAction() {
		numRetries = 5
	}
	runTestsWithExtraRetries(parentT, testName, testFunc, numRetries)
}

func runTestsWithExtraRetries(parentT *testing.T, testName string, testFunc func(t testing.TB), numRetries int) {
	for i := 1; i <= numRetries; i++ {
		rt := &retryingTester{nil, i == numRetries, true, testName, numRetries}
		parentT.Run(fmt.Sprintf("%s/%d", testName, i), func(t *testing.T) {
			rt.T = t
			testFunc(rt)
		})
		if rt.succeeded {
			if GLOBAL_STATSD != nil {
				GLOBAL_STATSD.Incr("test_status", []string{"result:passed", "test:" + testName, "os:" + runtime.GOOS}, 1.0)
				GLOBAL_STATSD.Distribution("test_retry_count", float64(i), []string{"test:" + testName, "os:" + runtime.GOOS}, 1.0)
			}
			break
		} else {
			if GLOBAL_STATSD != nil {
				GLOBAL_STATSD.Incr("test_status", []string{"result:failed", "test:" + testName, "os:" + runtime.GOOS}, 1.0)
			}
		}
	}
}

type retryingTester struct {
	*testing.T
	isFinalRun bool
	succeeded  bool
	testName   string
	numRetries int
}

func (t *retryingTester) Fatalf(format string, args ...any) {
	t.T.Helper()
	t.succeeded = false
	if t.isFinalRun {
		if GLOBAL_STATSD != nil {
			GLOBAL_STATSD.Incr("test_failure", []string{"test:" + t.testName, "os:" + runtime.GOOS}, 1.0)
			GLOBAL_STATSD.Distribution("test_retry_count", float64(t.numRetries), []string{"test:" + t.testName, "os:" + runtime.GOOS}, 1.0)
		}
		t.T.Fatalf(format, args...)
	} else {
		testutils.TestLog(t.T, fmt.Sprintf("retryingTester: Ignoring fatalf for non-final run: %#v", fmt.Sprintf(format, args...)))
	}
	t.SkipNow()
}

func (t *retryingTester) Errorf(format string, args ...any) {
	t.T.Helper()
	t.succeeded = false
	if t.isFinalRun {
		t.T.Errorf(format, args...)
	} else {
		testutils.TestLog(t.T, fmt.Sprintf("retryingTester: Ignoring errorf for non-final run: %#v", fmt.Sprintf(format, args...)))
	}
	t.SkipNow()
}

func (t *retryingTester) FailNow() {
	t.succeeded = false
	if t.isFinalRun {
		t.T.FailNow()
	} else {
		testutils.TestLog(t.T, "retryingTester: Ignoring FailNow for non-final run")
		// Still terminate execution via SkipNow() since FailNow() means we should stop the current test
		t.T.SkipNow()
	}
}

func (t *retryingTester) Fail() {
	t.succeeded = false
	if t.isFinalRun {
		t.T.Fail()
	} else {
		testutils.TestLog(t.T, "retryingTester: Ignoring Fail for non-final run")
	}
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
}

func captureTerminalOutputWithShellName(t testing.TB, tester shellTester, overriddenShellName string, commands []string) string {
	sCommands := make([]TmuxCommand, 0)
	for _, command := range commands {
		sCommands = append(sCommands, TmuxCommand{Keys: command})
	}
	return captureTerminalOutputWithShellNameAndDimensions(t, tester, overriddenShellName, 200, 50, sCommands)
}

func captureTerminalOutputWithShellNameAndDimensions(t testing.TB, tester shellTester, overriddenShellName string, width, height int, commands []TmuxCommand) string {
	sleepAmount := "0.1"
	if runtime.GOOS == "linux" {
		sleepAmount = "0.2"
	}
	if overriddenShellName == "fish" {
		// Fish is considerably slower so this is sadly necessary
		sleepAmount = "0.5"
	}
	if testutils.IsGithubAction() {
		sleepAmount = "0.5"
	}
	fullCommand := ""
	fullCommand += " tmux kill-session -t foo || true\n"
	fullCommand += fmt.Sprintf(" tmux -u new-session -d -x %d -y %d -s foo %s\n", width, height, overriddenShellName)
	fullCommand += " sleep 1\n"
	if overriddenShellName == "bash" {
		fullCommand += " tmux send -t foo SPACE source SPACE ~/.bashrc ENTER\n"
	}
	fullCommand += " sleep " + sleepAmount + "\n"
	for _, cmd := range commands {
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
		fullCommand += " sleep " + sleepAmount + "\n"
	}
	fullCommand += " sleep 2.5\n"
	if testutils.IsGithubAction() {
		fullCommand += " sleep 2.5\n"
	}
	fullCommand += " tmux capture-pane -t foo -p\n"
	fullCommand += " tmux kill-session -t foo\n"
	testutils.TestLog(t, "Running tmux command: "+fullCommand)
	return strings.TrimSpace(tester.RunInteractiveShell(t, fullCommand))
}

func assertNoLeakedConnections(t testing.TB) {
	resp, err := lib.ApiGet("/api/v1/get-num-connections")
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
