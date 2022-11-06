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
)

const (
	DB_WAL_PATH = data.DB_PATH + "-wal"
	DB_SHM_PATH = data.DB_PATH + "-shm"
)

func ResetLocalState(t *testing.T) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	_ = os.RemoveAll(path.Join(homedir, data.HISHTORY_PATH))
}

func BackupAndRestore(t *testing.T) func() {
	return BackupAndRestoreWithId(t, "")
}

func getBackPath(file, id string) string {
	if strings.Contains(file, "/"+data.HISHTORY_PATH+"/") {
		return strings.Replace(file, data.HISHTORY_PATH, data.HISHTORY_PATH+".test", 1) + id
	}
	return file + ".bak" + id
}

func BackupAndRestoreWithId(t *testing.T, id string) func() {
	ResetFakeHistoryTimestamp()
	homedir, err := os.UserHomeDir()
	Check(t, err)
	initialWd, err := os.Getwd()
	Check(t, err)
	Check(t, os.MkdirAll(path.Join(homedir, data.HISHTORY_PATH+".test"), os.ModePerm))

	renameFiles := []string{
		path.Join(homedir, data.HISHTORY_PATH, data.DB_PATH),
		path.Join(homedir, data.HISHTORY_PATH, DB_WAL_PATH),
		path.Join(homedir, data.HISHTORY_PATH, DB_SHM_PATH),
		path.Join(homedir, data.HISHTORY_PATH, data.CONFIG_PATH),
		path.Join(homedir, data.HISHTORY_PATH, "hishtory"),
		path.Join(homedir, data.HISHTORY_PATH, "config.sh"),
		path.Join(homedir, data.HISHTORY_PATH, "config.zsh"),
		path.Join(homedir, data.HISHTORY_PATH, "config.fish"),
		path.Join(homedir, ".bash_history"),
		path.Join(homedir, ".zsh_history"),
		path.Join(homedir, ".local/share/fish/fish_history"),
	}
	for _, file := range renameFiles {
		touchFile(file)
		_ = os.Rename(file, getBackPath(file, id))
	}
	copyFiles := []string{
		path.Join(homedir, ".zshrc"),
		path.Join(homedir, ".bashrc"),
		path.Join(homedir, ".bash_profile"),
	}
	for _, file := range copyFiles {
		touchFile(file)
		_ = copy(file, getBackPath(file, id))
	}
	configureZshrc(homedir)
	touchFile(path.Join(homedir, ".bash_history"))
	touchFile(path.Join(homedir, ".zsh_history"))
	touchFile(path.Join(homedir, ".local/share/fish/fish_history"))
	return func() {
		Check(t, os.MkdirAll(path.Join(homedir, data.HISHTORY_PATH), os.ModePerm))
		for _, file := range renameFiles {
			checkError(os.Rename(getBackPath(file, id), file))
		}
		for _, file := range copyFiles {
			checkError(copy(getBackPath(file, id), file))
		}
		if runtime.GOOS != "windows" {
			cmd := exec.Command("killall", "hishtory")
			stdout, err := cmd.Output()
			if err != nil && err.Error() != "exit status 1" {
				t.Fatalf("failed to execute killall hishtory, stdout=%#v: %v", string(stdout), err)
			}
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
	f, err := os.OpenFile(path.Join(homedir, ".zshrc"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	checkError(err)
	defer f.Close()
	_, err = f.WriteString(`export HISTFILE=~/.zsh_history
export HISTSIZE=10000
export SAVEHIST=1000
setopt SHARE_HISTORY
`)
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
		os.Setenv(k, origValue)
	}
}

func checkError(err error) {
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		_, cf, cl, _ := runtime.Caller(2)
		log.Fatalf("testutils fatal error at %s:%d (caller: %s:%d): %v", filename, line, cf, cl, err)
	}
}

func buildServer() {
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
	cmd := exec.Command("go", "build", "-o", "/tmp/server", "-ldflags", fmt.Sprintf("-X main.ReleaseVersion=v0.%s", version), "backend/server/server.go")
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
}

func RunTestServer() func() {
	os.Setenv("HISHTORY_SERVER", "http://localhost:8080")
	buildServer()
	cmd := exec.Command("/tmp/server")
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
		if strings.Contains(stderr.String()+stdout.String(), "failed to") && IsOnline() {
			panic(fmt.Sprintf("server failed to do something: stderr=%#v, stdout=%#v", stderr.String(), stdout.String()))
		}
		if strings.Contains(stderr.String()+stdout.String(), "ERROR:") {
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
