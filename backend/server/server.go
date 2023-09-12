package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ddworken/hishtory/internal/server"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/internal/database"
	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	PostgresDb   = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"
	StatsdSocket = "unix:///var/run/datadog/dsd.socket"
)

var (
	GLOBAL_DB      *database.DB
	GLOBAL_STATSD  *statsd.Client
	ReleaseVersion string = "UNKNOWN"
)

func isTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func isProductionEnvironment() bool {
	return os.Getenv("HISHTORY_ENV") == "prod"
}

func OpenDB() (*database.DB, error) {
	if isTestEnvironment() {
		db, err := database.OpenSQLite("file::memory:?_journal_mode=WAL&cache=shared", &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
		underlyingDb, err := db.DB.DB()
		if err != nil {
			return nil, fmt.Errorf("failed to access underlying DB: %w", err)
		}
		underlyingDb.SetMaxOpenConns(1)
		db.Exec("PRAGMA journal_mode = WAL")
		err = db.AddDatabaseTables()
		if err != nil {
			return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
		}
		return db, nil
	}

	// The same as the default logger, except with a higher SlowThreshold
	customLogger := logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             1000 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
	})

	var sqliteDb string
	if os.Getenv("HISHTORY_SQLITE_DB") != "" {
		sqliteDb = os.Getenv("HISHTORY_SQLITE_DB")
	}

	config := gorm.Config{Logger: customLogger}

	var db *database.DB
	if sqliteDb != "" {
		var err error
		db, err = database.OpenSQLite(sqliteDb, &config)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	} else {
		var err error
		postgresDb := fmt.Sprintf(PostgresDb, os.Getenv("POSTGRESQL_PASSWORD"))
		if os.Getenv("HISHTORY_POSTGRES_DB") != "" {
			postgresDb = os.Getenv("HISHTORY_POSTGRES_DB")
		}

		db, err = database.OpenPostgres(postgresDb, &config)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	}
	err := db.AddDatabaseTables()
	if err != nil {
		return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
	}
	return db, nil
}

func init() {
	if ReleaseVersion == "UNKNOWN" && !isTestEnvironment() {
		panic("server.go was built without a ReleaseVersion!")
	}
	InitDB()
	go runBackgroundJobs(context.Background())
}

func cron(ctx context.Context, db *database.DB, stats *statsd.Client) error {
	if err := updateReleaseVersion(); err != nil {
		return fmt.Errorf("updateReleaseVersion: %w", err)
	}

	if err := db.Clean(ctx); err != nil {
		return fmt.Errorf("db.Clean: %w", err)
	}
	if stats != nil {
		if err := stats.Flush(); err != nil {
			return fmt.Errorf("stats.Flush: %w", err)
		}
	}
	return nil
}

func runBackgroundJobs(ctx context.Context) {
	time.Sleep(5 * time.Second)
	for {
		err := cron(ctx, GLOBAL_DB, GLOBAL_STATSD)
		if err != nil {
			fmt.Printf("Cron failure: %v", err)

			// cron no longer panics, panicking here.
			panic(err)
		}
		time.Sleep(10 * time.Minute)
	}
}

type releaseInfo struct {
	Name string `json:"name"`
}

func updateReleaseVersion() error {
	resp, err := http.Get("https://api.github.com/repos/ddworken/hishtory/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to get latest release version: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read github API response body: %w", err)
	}
	if resp.StatusCode == 403 && strings.Contains(string(respBody), "API rate limit exceeded for ") {
		return nil
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to call github API, status_code=%d, body=%#v", resp.StatusCode, string(respBody))
	}
	var info releaseInfo
	err = json.Unmarshal(respBody, &info)
	if err != nil {
		return fmt.Errorf("failed to parse github API response: %w", err)
	}
	latestVersionTag := info.Name
	ReleaseVersion = decrementVersionIfInvalid(latestVersionTag)
	return nil
}

func decrementVersionIfInvalid(initialVersion string) string {
	// Decrements the version up to 5 times if the version doesn't have valid binaries yet.
	version := initialVersion
	for i := 0; i < 5; i++ {
		updateInfo := buildUpdateInfo(version)
		err := assertValidUpdate(updateInfo)
		if err == nil {
			fmt.Printf("Found a valid version: %v\n", version)
			return version
		}
		fmt.Printf("Found %s to be an invalid version: %v\n", version, err)
		version, err = decrementVersion(version)
		if err != nil {
			fmt.Printf("Failed to decrement version after finding the latest version was invalid: %v\n", err)
			return initialVersion
		}
	}
	fmt.Printf("Decremented the version 5 times and failed to find a valid version version number, initial version number: %v, last checked version number: %v\n", initialVersion, version)
	return initialVersion
}

func assertValidUpdate(updateInfo shared.UpdateInfo) error {
	urls := []string{updateInfo.LinuxAmd64Url, updateInfo.LinuxAmd64AttestationUrl, updateInfo.LinuxArm64Url, updateInfo.LinuxArm64AttestationUrl,
		updateInfo.LinuxArm7Url, updateInfo.LinuxArm7AttestationUrl,
		updateInfo.DarwinAmd64Url, updateInfo.DarwinAmd64UnsignedUrl, updateInfo.DarwinAmd64AttestationUrl,
		updateInfo.DarwinArm64Url, updateInfo.DarwinArm64UnsignedUrl, updateInfo.DarwinArm64AttestationUrl}
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to retrieve URL %#v: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 {
			return fmt.Errorf("URL %#v returned 404", url)
		}
	}
	return nil
}

func InitDB() {
	var err error
	GLOBAL_DB, err = OpenDB()
	if err != nil {
		panic(fmt.Errorf("OpenDB: %w", err))
	}

	if err := GLOBAL_DB.Ping(); err != nil {
		panic(fmt.Errorf("ping: %w", err))
	}
	if isProductionEnvironment() {
		if err := GLOBAL_DB.SetMaxIdleConns(10); err != nil {
			panic(fmt.Errorf("failed to set max idle conns: %w", err))
		}
	}
	if isTestEnvironment() {
		if err := GLOBAL_DB.SetMaxIdleConns(1); err != nil {
			panic(fmt.Errorf("failed to set max idle conns: %w", err))
		}
	}
}

func decrementVersion(version string) (string, error) {
	if version == "UNKNOWN" {
		return "", fmt.Errorf("cannot decrement UNKNOWN")
	}
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid version: %s", version)
	}
	versionNumber, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid version: %s", version)
	}
	return parts[0] + "." + strconv.Itoa(versionNumber-1), nil
}

func buildUpdateInfo(version string) shared.UpdateInfo {
	return shared.UpdateInfo{
		LinuxAmd64Url:             fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64", version),
		LinuxAmd64AttestationUrl:  fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64.intoto.jsonl", version),
		LinuxArm64Url:             fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm64", version),
		LinuxArm64AttestationUrl:  fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm64.intoto.jsonl", version),
		LinuxArm7Url:              fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm", version),
		LinuxArm7AttestationUrl:   fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm.intoto.jsonl", version),
		DarwinAmd64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64", version),
		DarwinAmd64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64-unsigned", version),
		DarwinAmd64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64.intoto.jsonl", version),
		DarwinArm64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64", version),
		DarwinArm64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64-unsigned", version),
		DarwinArm64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64.intoto.jsonl", version),
		Version:                   version,
	}
}

func main() {
	s, err := statsd.New(StatsdSocket)
	if err != nil {
		fmt.Printf("Failed to start DataDog statsd: %v\n", err)
	}

	// TODO: remove this global once we have a better way to pass it around
	GLOBAL_STATSD = s

	srv := server.NewServer(
		GLOBAL_DB,
		server.WithStatsd(s),
		server.WithReleaseVersion(ReleaseVersion),
		server.IsTestEnvironment(isTestEnvironment()),
		server.IsProductionEnvironment(isProductionEnvironment()),
		server.WithCron(cron),
		server.WithUpdateInfo(buildUpdateInfo(ReleaseVersion)),
	)

	if err := srv.Run(context.Background(), ":8080"); err != nil {
		panic(err)
	}
}

func checkGormResult(result *gorm.DB) {
	checkGormError(result.Error, 1)
}

func checkGormError(err error, skip int) {
	if err == nil {
		return
	}

	_, filename, line, _ := runtime.Caller(skip + 1)
	panic(fmt.Sprintf("DB error at %s:%d: %v", filename, line, err))
}

// TODO(optimization): Maybe optimize the endpoints a bit to reduce the number of round trips required?
// TODO: Add error checking for the calls to updateUsageData(...) that logs it/triggers an alert in prod, but is an error in test
