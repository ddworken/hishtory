package shared

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"
)

func ResetLocalState(t *testing.T) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, DB_PATH))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, "hishtory"))
	_ = os.Remove(path.Join(homedir, HISHTORY_PATH, "config.sh"))
}

func BackupAndRestore(t *testing.T) func() {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH), path.Join(homedir, HISHTORY_PATH, DB_PATH+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "hishtory"), path.Join(homedir, HISHTORY_PATH, "hishtory.bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "config.sh"), path.Join(homedir, HISHTORY_PATH, "config.sh.bak"))
	return func() {
		_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH+".bak"), path.Join(homedir, HISHTORY_PATH, DB_PATH))
		_ = os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+".bak"), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH))
		_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "hishtory.bak"), path.Join(homedir, HISHTORY_PATH, "hishtory"))
		_ = os.Rename(path.Join(homedir, HISHTORY_PATH, "config.sh.bak"), path.Join(homedir, HISHTORY_PATH, "config.sh"))
	}
}

func buildServer(t *testing.T) {
	for {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to getwd: %v", err)
		}
		if strings.HasSuffix(wd, "/hishtory") {
			break
		}
		err = os.Chdir("../")
		if err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}
		if wd == "/" {
			t.Fatalf("failed to cd into hishtory dir!")
		}
	}
	cmd := exec.Command("go", "build", "-o", "/tmp/server", "backend/server/server.go")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Start()
	if err != nil {
		t.Fatalf("failed to start to build server: %v, stderr=%#v, stdout=%#v", err, stderr.String(), stdout.String())
	}
	err = cmd.Wait()
	if err != nil {
		wd, _ := os.Getwd()
		t.Fatalf("failed to build server: %v, wd=%#v, stderr=%#v, stdout=%#v", err, wd, stderr.String(), stdout.String())
	}
}

func RunTestServer(t *testing.T) func() {
	os.Setenv("HISHTORY_SERVER", "http://localhost:8080")
	buildServer(t)
	cmd := exec.Command("/tmp/server")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	// TODO: Optimize this by streaming stdout and waiting until we see the "listening ..." message
	time.Sleep(time.Second * 3)
	go func() {
		_ = cmd.Wait()
	}()
	return func() {
		err := cmd.Process.Kill()
		if err != nil && err.Error() != "os: process already finished" {
			t.Fatalf("failed to kill process: %v", err)
		}
		if strings.Contains(stderr.String()+stdout.String(), "failed to") {
			t.Fatalf("server failed to do something: stderr=%#v, stdout=%#v", stderr.String(), stdout.String())
		}
		fmt.Printf("stderr=%#v, stdout=%#v\n", stderr.String(), stdout.String())
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
