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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
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

func ResetLocalState(t testing.TB) {
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	persistLog()
	_ = BackupAndRestoreWithId(t, "-reset-local-state")
	_ = os.RemoveAll(path.Join(homedir, data.GetHishtoryPath()))
}

func BackupAndRestore(t testing.TB) func() {
	return BackupAndRestoreWithId(t, "")
}

func getBackPath(file, id string) string {
	if strings.Contains(file, "/"+data.GetHishtoryPath()+"/") {
		return strings.Replace(file, data.GetHishtoryPath(), data.GetHishtoryPath()+".test", 1) + id
	}
	return file + ".bak" + id
}

func BackupAndRestoreWithId(t testing.TB, id string) func() {
	ResetFakeHistoryTimestamp()
	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	initialWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(path.Join(homedir, data.GetHishtoryPath()+".test"), os.ModePerm))

	renameFiles := []string{
		path.Join(homedir, data.GetHishtoryPath(), data.DB_PATH),
		path.Join(homedir, data.GetHishtoryPath(), DB_WAL_PATH),
		path.Join(homedir, data.GetHishtoryPath(), DB_SHM_PATH),
		path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH),
		path.Join(homedir, data.GetHishtoryPath(), "config.sh"),
		path.Join(homedir, data.GetHishtoryPath(), "config.zsh"),
		path.Join(homedir, data.GetHishtoryPath(), "config.fish"),
		path.Join(homedir, data.GetHishtoryPath(), "hishtory"),
		path.Join(homedir, ".bash_history"),
		path.Join(homedir, ".zsh_history"),
		path.Join(homedir, ".zhistory"),
		path.Join(homedir, ".local/share/fish/fish_history"),
	}
	for _, file := range renameFiles {
		touchFile(file)
		require.NoError(t, os.Rename(file, getBackPath(file, id)))
	}
	copyFiles := []string{
		path.Join(homedir, ".zshrc"),
		path.Join(homedir, ".bashrc"),
		path.Join(homedir, ".bash_profile"),
		path.Join(homedir, ".profile"),
	}
	for _, file := range copyFiles {
		touchFile(file)
		require.NoError(t, copy(file, getBackPath(file, id)))
	}
	configureZshrc(homedir)
	touchFile(path.Join(homedir, ".bash_history"))
	touchFile(path.Join(homedir, ".zsh_history"))
	touchFile(path.Join(homedir, ".local/share/fish/fish_history"))
	restoreHishtoryOffline := BackupAndRestoreEnv("HISHTORY_SIMULATE_NETWORK_ERROR")
	os.Setenv("HISHTORY_SIMULATE_NETWORK_ERROR", "")
	return func() {
		cmd := exec.Command("killall", "hishtory", "tmux")
		stdout, err := cmd.Output()
		if err != nil && err.Error() != "exit status 1" {
			t.Fatalf("failed to execute killall hishtory, stdout=%#v: %v", string(stdout), err)
		}
		persistLog()
		require.NoError(t, os.RemoveAll(path.Join(homedir, data.GetHishtoryPath())))
		require.NoError(t, os.MkdirAll(path.Join(homedir, data.GetHishtoryPath()), os.ModePerm))
		for _, file := range renameFiles {
			checkError(os.Rename(getBackPath(file, id), file))
		}
		for _, file := range copyFiles {
			checkError(copy(getBackPath(file, id), file))
		}
		checkError(os.Chdir(initialWd))
		restoreHishtoryOffline()
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
	f, err := os.OpenFile(path.Join(homedir, ".zshrc"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	checkError(err)
	defer f.Close()
	_, err = f.WriteString(zshrcHistConfig)
	checkError(err)
}

func copy(src, dst string) error {
	// Copy the contents of the file
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
	err = out.Close()
	if err != nil {
		return err
	}

	// And copy the permissions
	srcStat, err := in.Stat()
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcStat.Mode())
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
	for range 100 {
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
		panic(fmt.Sprintf("failed to read VERSION file: %v", err))
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

func killExistingTestServers() {
	_ = exec.Command("bash", "-c", "lsof -i tcp:8080 | grep LISTEN | awk '{print $2}' | xargs kill -9").Run()
}

func RunTestServer() func() {
	killExistingTestServers()
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
	expectedSuffix := "Listening on :8080\n"
	if !strings.HasSuffix(stdout.String(), expectedSuffix) {
		panic(fmt.Errorf("expected server stdout to end with %#v, but it doesn't: %#v", expectedSuffix, stdout.String()))
	}
	return func() {
		// Kill the server process to guarantee the next test can run
		err := cmd.Process.Kill()
		if err != nil && err.Error() != "os: process already finished" {
			panic(fmt.Sprintf("failed to kill server process: %v", err))
		}
		// Delete the built server binary to avoid wasting disk space
		_ = os.Remove(fn)
		// Now that we've cleaned up, check the output to see if the server had any errors
		allOutput := stdout.String() + stderr.String()
		if strings.Contains(allOutput, "failed to") && IsOnline() {
			panic(fmt.Sprintf("server experienced an error\n\n\nstderr=\n%s\n\n\nstdout=%s", stderr.String(), stdout.String()))
		}
		if strings.Contains(allOutput, "ERROR:") || strings.Contains(allOutput, "http: panic serving") {
			panic(fmt.Sprintf("server experienced an error\n\n\nstderr=\n%s\n\n\nstdout=%s", stderr.String(), stdout.String()))
		}
		// Persist test server logs for debugging
		f, err := os.OpenFile("/tmp/hishtory-server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		checkError(err)
		defer f.Close()
		_, err = f.Write([]byte(stdout.String() + "\n"))
		checkError(err)
		_, err = f.Write([]byte(stderr.String() + "\n"))
		checkError(err)
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
		StartTime:               time.Unix(fakeHistoryTimestamp, 0).UTC(),
		EndTime:                 time.Unix(fakeHistoryTimestamp+3, 0).UTC(),
		DeviceId:                "fake_device_id",
		EntryId:                 uuid.Must(uuid.NewRandom()).String(),
	}
}

func IsGithubAction() bool {
	return os.Getenv("GITHUB_ACTION") != ""
}

func TestLog(t testing.TB, line string) {
	f, err := os.OpenFile("/tmp/test.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		require.NoError(t, err)
	}
	defer f.Close()
	_, err = f.WriteString(time.Now().UTC().Format(time.RFC3339) + ": " + line + "\n")
	if err != nil {
		require.NoError(t, err)
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
	f, err := os.OpenFile("/tmp/hishtory.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	checkError(err)
	defer f.Close()
	_, err = f.Write(log)
	checkError(err)
	_, err = f.WriteString("\n")
	checkError(err)
}

func recordUsingGolden(t testing.TB, goldenName string) {
	f, err := os.OpenFile("/tmp/goldens-used.txt",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("failed to open file to record using golden: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(goldenName + "\n"); err != nil {
		t.Fatalf("failed to append to file to record using golden: %v", err)
	}
}

func CompareGoldens(t testing.TB, out, goldenName string) {
	recordUsingGolden(t, goldenName)
	out = normalizeHostnames(out)
	goldenPath := path.Join(initialWd, "client/testdata/", goldenName)
	expected, err := os.ReadFile(goldenPath)
	expected = []byte(normalizeHostnames(string(expected)))
	if err != nil {
		if os.IsNotExist(err) {
			expected = []byte("ERR_FILE_NOT_FOUND:" + goldenPath)
		} else {
			require.NoError(t, err)
		}
	}
	if diff := cmp.Diff(string(expected), out); diff != "" {
		if err := os.Mkdir("/tmp/test-goldens", os.ModePerm); err != nil && !os.IsExist(err) {
			log.Fatal(err)
		}
		require.NoError(t, os.WriteFile(path.Join("/tmp/test-goldens", goldenName), []byte(out), 0o644))
		if os.Getenv("HISHTORY_UPDATE_GOLDENS") == "" {
			_, filename, line, _ := runtime.Caller(1)
			t.Fatalf("hishtory golden mismatch for %s at %s:%d (-expected +got):\n%s\nactual=\n%s", goldenName, filename, line, diff, out)
		} else {
			require.NoError(t, os.WriteFile(goldenPath, []byte(out), 0o644))
		}
	}
}

func normalizeHostnames(data string) string {
	hostnames := []string{"Davids-MacBook-Air", "Davids-MacBook-Air.local", "ghaction-runner-hostname", "Davids-Air"}
	for _, hostname := range hostnames {
		data = strings.ReplaceAll(data, hostname, "ghaction-runner-hostname")
	}
	return data
}

func GetOsVersion(t *testing.T) string {
	if runtime.GOOS == "linux" {
		return "actions"
	}
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		panic(err)
	}

	version := unix.ByteSliceToString(uts.Release[:])
	return strings.Split(version, ".")[0]
}

const DefaultGitBranchName = "master"

// MinIO test configuration
const (
	MinioEndpoint        = "http://127.0.0.1:9000"
	MinioAccessKeyID     = "minioadmin"
	MinioSecretAccessKey = "minioadmin"
	MinioBucket          = "hishtory-test"
	MinioRegion          = "us-east-1"
)

// RunMinioServer starts a MinIO server for S3 backend testing.
// On macOS, it prefers using a native MinIO binary (installed via brew).
// On other platforms, it uses Docker to run MinIO.
// If neither is available, it prints a warning and returns a no-op cleanup function.
func RunMinioServer() func() {
	// On macOS, prefer native MinIO binary (more reliable than Docker/colima)
	if runtime.GOOS == "darwin" {
		if cleanup := runNativeMinioServer(); cleanup != nil {
			return cleanup
		}
		// Fall through to try Docker if native MinIO failed
	}

	// Try Docker-based MinIO
	return runDockerMinioServer()
}

// runNativeMinioServer starts MinIO using a native binary (e.g., installed via brew).
// Returns nil if MinIO binary is not available.
func runNativeMinioServer() func() {
	minioPath, err := exec.LookPath("minio")
	if err != nil {
		fmt.Println("Native MinIO not found, will try Docker instead.")
		return nil
	}

	// Create a temporary directory for MinIO data
	dataDir, err := os.MkdirTemp("", "hishtory-minio-test-*")
	if err != nil {
		fmt.Printf("WARNING: Failed to create temp dir for MinIO: %v\n", err)
		return nil
	}

	// Start MinIO server as a background process
	cmd := exec.Command(minioPath, "server", dataDir, "--address", ":9000", "--console-address", ":9001")
	cmd.Env = append(os.Environ(),
		"MINIO_ROOT_USER="+MinioAccessKeyID,
		"MINIO_ROOT_PASSWORD="+MinioSecretAccessKey,
	)

	// Redirect output to avoid cluttering test output
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		fmt.Printf("WARNING: Failed to start native MinIO: %v\n", err)
		_ = os.RemoveAll(dataDir)
		return nil
	}

	// Wait for MinIO to be ready
	time.Sleep(2 * time.Second)

	// Create the test bucket using mc (MinIO client)
	if mcPath, err := exec.LookPath("mc"); err == nil {
		aliasCmd := exec.Command(mcPath, "alias", "set", "local", "http://localhost:9000", MinioAccessKeyID, MinioSecretAccessKey)
		_ = aliasCmd.Run()

		mbCmd := exec.Command(mcPath, "mb", "local/"+MinioBucket, "--ignore-existing")
		_ = mbCmd.Run()
	}

	// Set HISHTORY_S3_SECRET_ACCESS_KEY environment variable for tests
	os.Setenv("HISHTORY_S3_SECRET_ACCESS_KEY", MinioSecretAccessKey)

	fmt.Println("Started native MinIO server for S3 backend tests")
	return func() {
		// Kill the MinIO process
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		// Clean up data directory
		_ = os.RemoveAll(dataDir)
		os.Unsetenv("HISHTORY_S3_SECRET_ACCESS_KEY")
	}
}

// runDockerMinioServer starts MinIO using Docker.
func runDockerMinioServer() func() {
	// Check if Docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("WARNING: Docker not available, skipping MinIO server startup. S3 tests will be skipped.")
		return func() {}
	}

	// Kill any existing MinIO container
	_ = exec.Command("docker", "rm", "-f", "hishtory-minio-test").Run()

	// Start MinIO container
	cmd := exec.Command("docker", "run", "-d",
		"--name", "hishtory-minio-test",
		"-p", "9000:9000",
		"-p", "9001:9001",
		"-e", "MINIO_ROOT_USER="+MinioAccessKeyID,
		"-e", "MINIO_ROOT_PASSWORD="+MinioSecretAccessKey,
		"minio/minio", "server", "/data", "--console-address", ":9001",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("WARNING: Failed to start MinIO container: %v. S3 tests will be skipped.\n", err)
		return func() {}
	}

	// Wait for MinIO to be ready
	time.Sleep(3 * time.Second)

	// Create the test bucket using mc (MinIO client) in container
	createBucketCmd := exec.Command("docker", "exec", "hishtory-minio-test",
		"mc", "alias", "set", "local", "http://localhost:9000", MinioAccessKeyID, MinioSecretAccessKey)
	_ = createBucketCmd.Run()

	createBucketCmd = exec.Command("docker", "exec", "hishtory-minio-test",
		"mc", "mb", "local/"+MinioBucket, "--ignore-existing")
	_ = createBucketCmd.Run()

	// Set HISHTORY_S3_SECRET_ACCESS_KEY environment variable for tests
	os.Setenv("HISHTORY_S3_SECRET_ACCESS_KEY", MinioSecretAccessKey)

	return func() {
		// Stop and remove the container
		_ = exec.Command("docker", "rm", "-f", "hishtory-minio-test").Run()
		os.Unsetenv("HISHTORY_S3_SECRET_ACCESS_KEY")
	}
}

// IsMinioRunning checks if the MinIO server is running and accessible
func IsMinioRunning() bool {
	resp, err := http.Get(MinioEndpoint + "/minio/health/live")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func GetCurrentGitBranch(t *testing.T) string {
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to get current git branch: %v", err)
	}

	return strings.TrimSpace(out.String())
}

// IsShardedTestRun returns whether this is a sharded test run (i.e., in GitHub Actions).
func IsShardedTestRun() bool {
	return NumTestShards() != -1 && CurrentShardNumber() != -1
}

// NumTestShards returns the total number of test shards, or -1 if not sharding.
func NumTestShards() int {
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

// CurrentShardNumber returns the current shard number, or -1 if not sharding.
func CurrentShardNumber() int {
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

// MarkTestForSharding marks the given test for sharding with the given test ID number.
// Tests with the same shard number will run on the same shard.
func MarkTestForSharding(t *testing.T, testShardNumber int) {
	if IsShardedTestRun() {
		if testShardNumber%NumTestShards() != CurrentShardNumber() {
			t.Skip("Skipping sharded test")
		}
	}
}
