package testutils

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/google/go-cmp/cmp"
)

const (
	DB_WAL_PATH = data.DB_PATH + "-wal"
	DB_SHM_PATH = data.DB_PATH + "-shm"
)

var initialWd string

func init() {
	initialWd = getInitialWd()
}

func getInitialWd() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	if !strings.Contains(cwd, "/hishtory/") {
		return cwd
	}
	components := strings.Split(cwd, "/hishtory/")
	dir := components[0] + "/hishtory"
	if IsGithubAction() {
		dir += "/hishtory"
	}
	return dir
}

func ResetLocalState(t *testing.T) {
	homedir, err := os.UserHomeDir()
	Check(t, err)
	persistLog()
	_ = BackupAndRestoreWithId(t, "-reset-local-state")
	_ = os.RemoveAll(path.Join(homedir, data.GetHishtoryPath()))
}

func BackupAndRestore(t *testing.T) func() {
	return BackupAndRestoreWithId(t, "")
}

func getBackPath(file, id string) string {
	if strings.Contains(file, "/"+data.GetHishtoryPath()+"/") {
		return strings.Replace(file, data.GetHishtoryPath(), data.GetHishtoryPath()+".test", 1) + id
	}
	return file + ".bak" + id
}

func BackupAndRestoreWithId(t *testing.T, id string) func() {
	ResetFakeHistoryTimestamp()
	homedir, err := os.UserHomeDir()
	Check(t, err)
	initialWd, err := os.Getwd()
	Check(t, err)
	Check(t, os.MkdirAll(path.Join(homedir, data.GetHishtoryPath()+".test"), os.ModePerm))

	renameFiles := []string{
		path.Join(homedir, data.GetHishtoryPath(), data.DB_PATH),
		path.Join(homedir, data.GetHishtoryPath(), DB_WAL_PATH),
		path.Join(homedir, data.GetHishtoryPath(), DB_SHM_PATH),
		path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH),
		path.Join(homedir, data.GetHishtoryPath(), "hishtory"),
		path.Join(homedir, data.GetHishtoryPath(), "config.sh"),
		path.Join(homedir, data.GetHishtoryPath(), "config.zsh"),
		path.Join(homedir, data.GetHishtoryPath(), "config.fish"),
		path.Join(homedir, ".bash_history"),
		path.Join(homedir, ".zsh_history"),
		path.Join(homedir, ".local/share/fish/fish_history"),
	}
	for _, file := range renameFiles {
		touchFile(file)
		Check(t, os.Rename(file, getBackPath(file, id)))
	}
	copyFiles := []string{
		path.Join(homedir, ".zshrc"),
		path.Join(homedir, ".bashrc"),
		path.Join(homedir, ".bash_profile"),
	}
	for _, file := range copyFiles {
		touchFile(file)
		Check(t, copy(file, getBackPath(file, id)))
	}
	configureZshrc(homedir)
	touchFile(path.Join(homedir, ".bash_history"))
	touchFile(path.Join(homedir, ".zsh_history"))
	touchFile(path.Join(homedir, ".local/share/fish/fish_history"))
	return func() {
		if runtime.GOOS != "windows" {
			cmd := exec.Command("killall", "hishtory", "tmux")
			stdout, err := cmd.Output()
			if err != nil && err.Error() != "exit status 1" {
				t.Fatalf("failed to execute killall hishtory, stdout=%#v: %v", string(stdout), err)
			}
		}
		persistLog()
		Check(t, os.RemoveAll(path.Join(homedir, data.GetHishtoryPath())))
		Check(t, os.MkdirAll(path.Join(homedir, data.GetHishtoryPath()), os.ModePerm))
		for _, file := range renameFiles {
			checkError(os.Rename(getBackPath(file, id), file))
		}
		for _, file := range copyFiles {
			checkError(copy(getBackPath(file, id), file))
		}
		checkError(os.Chdir(initialWd))
	}
}

func touchFile(p string) {
	_, err := os.Stat(p)
	if os.IsNotExist(err) {
		checkError(os.MkdirAll(filepath.Dir(p), os.ModePerm))
		file, err := os.Create(p)
		checkError(err)
		defer file.Close()
	} else {
		currentTime := time.Now().Local()
		err := os.Chtimes(p, currentTime, currentTime)
		checkError(err)
	}
}

func configureZshrc(homedir string) {
	zshrcHistConfig := `export HISTFILE=~/.zsh_history
export HISTSIZE=10000
export SAVEHIST=1000
setopt SHARE_HISTORY
`
	dat, err := os.ReadFile(path.Join(homedir, ".zshrc"))
	checkError(err)
	if strings.Contains(string(dat), zshrcHistConfig) {
		return
	}
	f, err := os.OpenFile(path.Join(homedir, ".zshrc"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	checkError(err)
	defer f.Close()
	_, err = f.WriteString(zshrcHistConfig)
	checkError(err)
}

func copy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

func BackupAndRestoreEnv(k string) func() {
	origValue := os.Getenv(k)
	return func() {
		if origValue == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, origValue)
		}
	}
}

func checkError(err error) {
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		_, cf, cl, _ := runtime.Caller(2)
		log.Fatalf("testutils fatal error at %s:%d (caller: %s:%d): %v", filename, line, cf, cl, err)
	}
}

func buildServer() string {
	for i := 0; i < 100; i++ {
		wd, err := os.Getwd()
		if err != nil {
			panic(fmt.Sprintf("failed to getwd: %v", err))
		}
		if strings.HasSuffix(wd, "hishtory") {
			break
		}
		err = os.Chdir("../")
		if err != nil {
			panic(fmt.Sprintf("failed to chdir: %v", err))
		}
		if wd == "/" {
			panic("failed to cd into hishtory dir!")
		}
	}
	version, err := os.ReadFile("VERSION")
	if err != nil {
		if runtime.GOOS == "windows" {
			// TODO: Figure out why it can't read the VERSION file
			version = []byte("174")
		} else {
			panic(fmt.Sprintf("failed to read VERSION file: %v", err))
		}
	}
	f, err := os.CreateTemp("", "server")
	checkError(err)
	fn := f.Name()
	cmd := exec.Command("go", "build", "-o", fn, "-ldflags", fmt.Sprintf("-X main.ReleaseVersion=v0.%s", version), "backend/server/server.go")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Start()
	if err != nil {
		panic(fmt.Sprintf("failed to start to build server: %v, stderr=%#v, stdout=%#v", err, stderr.String(), stdout.String()))
	}
	err = cmd.Wait()
	if err != nil {
		wd, _ := os.Getwd()
		panic(fmt.Sprintf("failed to build server: %v, wd=%#v, stderr=%#v, stdout=%#v", err, wd, stderr.String(), stdout.String()))
	}
	return fn
}

func RunTestServer() func() {
	os.Setenv("HISHTORY_SERVER", "http://localhost:8080")
	fn := buildServer()
	cmd := exec.Command(fn)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Start()
	if err != nil {
		panic(fmt.Sprintf("failed to start server: %v", err))
	}
	time.Sleep(time.Second * 5)
	go func() {
		_ = cmd.Wait()
	}()
	return func() {
		err := cmd.Process.Kill()
		if err != nil && err.Error() != "os: process already finished" {
			panic(fmt.Sprintf("failed to kill server process: %v", err))
		}
		allOutput := stdout.String() + stderr.String()
		if strings.Contains(allOutput, "failed to") && IsOnline() {
			panic(fmt.Sprintf("server failed to do something: stderr=%#v, stdout=%#v", stderr.String(), stdout.String()))
		}
		if strings.Contains(allOutput, "ERROR:") || strings.Contains(allOutput, "http: panic serving") {
			panic(fmt.Sprintf("server experienced an error: stderr=%#v, stdout=%#v", stderr.String(), stdout.String()))
		}
		// fmt.Printf("stderr=%#v, stdout=%#v\n", stderr.String(), stdout.String())
	}
}

func Check(t *testing.T, err error) {
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		t.Fatalf("Unexpected error at %s:%d: %v", filename, line, err)
	}
}

func CheckWithInfo(t *testing.T, err error, additionalInfo string) {
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		t.Fatalf("Unexpected error: %v at %s:%d! Additional info: %v", err, filename, line, additionalInfo)
	}
}

func IsOnline() bool {
	_, err := http.Get("https://hishtory.dev")
	return err == nil
}

var fakeHistoryTimestamp int64 = 1666068191

func ResetFakeHistoryTimestamp() {
	fakeHistoryTimestamp = 1666068191
}

func MakeFakeHistoryEntry(command string) data.HistoryEntry {
	fakeHistoryTimestamp += 5
	return data.HistoryEntry{
		LocalUsername:           "david",
		Hostname:                "localhost",
		Command:                 command,
		CurrentWorkingDirectory: "/tmp/",
		HomeDirectory:           "/home/david/",
		ExitCode:                2,
		StartTime:               time.Unix(fakeHistoryTimestamp, 0),
		EndTime:                 time.Unix(fakeHistoryTimestamp+3, 0),
	}
}

func IsGithubAction() bool {
	return os.Getenv("GITHUB_ACTION") != ""
}

func TestLog(t *testing.T, line string) {
	f, err := os.OpenFile("/tmp/test.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		Check(t, err)
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	if err != nil {
		Check(t, err)
	}
}

func persistLog() {
	homedir, err := os.UserHomeDir()
	checkError(err)
	fp := path.Join(homedir, data.GetHishtoryPath(), "hishtory.log")
	log, err := os.ReadFile(fp)
	if err != nil {
		return
	}
	f, err := os.OpenFile("/tmp/hishtory.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	checkError(err)
	defer f.Close()
	_, err = f.Write(log)
	checkError(err)
	_, err = f.WriteString("\n")
	checkError(err)
}

func CompareGoldens(t *testing.T, out, goldenName string) {
	out = normalizeHostnames(out)
	goldenPath := path.Join(initialWd, "client/lib/goldens/", goldenName)
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			expected = []byte("ERR_FILE_NOT_FOUND:" + goldenPath)
		} else {
			Check(t, err)
		}
	}
	if diff := cmp.Diff(string(expected), out); diff != "" {
		if os.Getenv("HISHTORY_UPDATE_GOLDENS") == "" {
			_, filename, line, _ := runtime.Caller(1)
			t.Fatalf("hishtory golden mismatch for %s at %s:%d (-expected +got):\n%s\nactual=\n%s", goldenName, filename, line, diff, out)
		} else {
			Check(t, os.WriteFile(goldenPath, []byte(out), 0644))
		}
	}
}

func normalizeHostnames(data string) string {
	hostnames := []string{"Davids-MacBook-Air.local", "ghaction-runner-hostname"}
	for _, hostname := range hostnames {
		data = strings.ReplaceAll(data, hostname, "ghaction-runner-hostname")
	}
	return data
}
