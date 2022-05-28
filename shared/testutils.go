package shared

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	DB_WAL_PATH = DB_PATH + "-wal"
	DB_SHM_PATH = DB_PATH + "-shm"
)

func ResetLocalState(t *testing.T) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, DB_PATH))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, DB_WAL_PATH))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, "hishtory"))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, "config.sh"))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, "config.zsh"))
}

func BackupAndRestore(t *testing.T) func() {
	return BackupAndRestoreWithId(t, "")
}

func BackupAndRestoreWithId(t *testing.T, id string) func() {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH), path.Join(homedir, HISHTORY_PATH, DB_PATH+id+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_WAL_PATH), path.Join(homedir, HISHTORY_PATH, DB_WAL_PATH+id+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_SHM_PATH), path.Join(homedir, HISHTORY_PATH, DB_SHM_PATH+id+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+id+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "hishtory"), path.Join(homedir, HISHTORY_PATH, "hishtory"+id+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "config.sh"), path.Join(homedir, HISHTORY_PATH, "config.sh"+id+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "config.zsh"), path.Join(homedir, HISHTORY_PATH, "config.zsh"+id+".bak"))
	return func() {
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH+id+".bak"), path.Join(homedir, HISHTORY_PATH, DB_PATH)))
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, DB_WAL_PATH+id+".bak"), path.Join(homedir, HISHTORY_PATH, DB_WAL_PATH)))
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, DB_SHM_PATH+id+".bak"), path.Join(homedir, HISHTORY_PATH, DB_SHM_PATH)))
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+id+".bak"), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH)))
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, "hishtory"+id+".bak"), path.Join(homedir, HISHTORY_PATH, "hishtory")))
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, "config.sh"+id+".bak"), path.Join(homedir, HISHTORY_PATH, "config.sh")))
		checkError(os.Rename(path.Join(homedir, HISHTORY_PATH, "config.zsh"+id+".bak"), path.Join(homedir, HISHTORY_PATH, "config.zsh")))
	}
}

func checkError(err error) {
	if err != nil && os.Getenv("GITHUB_ACTION") == "" {
		_, filename, line, _ := runtime.Caller(1)
		_, cf, cl, _ := runtime.Caller(2)
		log.Fatalf("testutils fatal error at %s:%d (caller: %s:%d): %v", filename, line, cf, cl, err)
	}
}

func buildServer() {
	for {
		wd, err := os.Getwd()
		if err != nil {
			panic(fmt.Sprintf("failed to getwd: %v", err))
		}
		if strings.HasSuffix(wd, "/hishtory") {
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
		panic(fmt.Sprintf("failed to read VERSION file: %v", err))
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
