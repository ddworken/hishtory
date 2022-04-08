package shared

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
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
}

func BackupAndRestore(t *testing.T) func() {
	homedir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to retrieve homedir: %v", err)
	}

	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH), path.Join(homedir, HISHTORY_PATH, DB_PATH+".bak"))
	_ = os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+".bak"))
	return func() {
		_ = os.Rename(path.Join(homedir, HISHTORY_PATH, DB_PATH+".bak"), path.Join(homedir, HISHTORY_PATH, DB_PATH))
		_ = os.Rename(path.Join(homedir, HISHTORY_PATH, CONFIG_PATH+".bak"), path.Join(homedir, HISHTORY_PATH, CONFIG_PATH))
	}
}

func buildServer(t *testing.T) {
	err := os.Chdir("/home/david/code/hishtory/")
	if err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", "/tmp/server", "server/server.go")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Start()
	if err != nil {
		t.Fatalf("failed to start to build server: %v, stderr=%#v, stdout=%#v", err, stderr.String(), stdout.String())
	}
	err = cmd.Wait()
	if err != nil {
		t.Fatalf("failed to build server: %v, stderr=%#v, stdout=%#v", err, stderr.String(), stdout.String())
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
	time.Sleep(time.Second * 3)
	go func() {
		_ = cmd.Wait()
	}()
	return func() {
		err := cmd.Process.Kill()
		if err != nil {
			t.Fatalf("failed to kill process: %v", err)
		}
		fmt.Printf("stderr=%#v, stdout=%#v\n", stderr.String(), stdout.String())
	}
}

func Check(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func CheckWithInfo(t *testing.T, err error, additionalInfo string) {
	if err != nil {
		t.Fatalf("Unexpected error: %v! Additional info: %v", err, additionalInfo)
	}
}
