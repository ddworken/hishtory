package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/internal/database"
	"github.com/ddworken/hishtory/internal/release"
	"github.com/ddworken/hishtory/internal/server"
	_ "github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	PostgresDb   = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"
	StatsdSocket = "unix:///var/run/datadog/dsd.socket"
)

var (
	// Filled in via ldflags with the latest released version as of the server getting built
	ReleaseVersion string
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

	fmt.Println("Connecting to DB")
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
	fmt.Println("AutoMigrating DB tables")
	err := db.AddDatabaseTables()
	if err != nil {
		return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
	}
	return db, nil
}

func cron(ctx context.Context, db *database.DB, stats *statsd.Client) error {
	if err := release.UpdateReleaseVersion(); err != nil {
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

func runBackgroundJobs(ctx context.Context, srv *server.Server, db *database.DB, stats *statsd.Client) {
	time.Sleep(5 * time.Second)
	for {
		err := cron(ctx, db, stats)
		if err != nil {
			fmt.Printf("Cron failure: %v", err)

			// cron no longer panics, panicking here.
			panic(err)
		}
		srv.UpdateReleaseVersion(release.Version, release.BuildUpdateInfo(release.Version))
		time.Sleep(10 * time.Minute)
	}
}

func InitDB() *database.DB {
	fmt.Println("Opening DB")
	db, err := OpenDB()
	if err != nil {
		panic(fmt.Errorf("OpenDB: %w", err))
	}

	fmt.Println("Pinging DB to confirm liveness")
	if err := db.Ping(); err != nil {
		panic(fmt.Errorf("ping: %w", err))
	}
	if isProductionEnvironment() {
		if err := db.SetMaxIdleConns(10); err != nil {
			panic(fmt.Errorf("failed to set max idle conns: %w", err))
		}
	}
	if isTestEnvironment() {
		if err := db.SetMaxIdleConns(1); err != nil {
			panic(fmt.Errorf("failed to set max idle conns: %w", err))
		}
	}
	fmt.Println("Done initializing DB")
	return db
}

func main() {
	// Startup check:
	release.Version = ReleaseVersion
	if release.Version == "UNKNOWN" && !isTestEnvironment() {
		panic("server.go was built without a ReleaseVersion!")
	}

	// Create DB and stats
	db := InitDB()
	stats, err := statsd.New(StatsdSocket)
	if err != nil {
		fmt.Printf("Failed to start DataDog statsd: %v\n", err)
	}

	srv := server.NewServer(
		db,
		server.WithStatsd(stats),
		server.WithReleaseVersion(release.Version),
		server.IsTestEnvironment(isTestEnvironment()),
		server.IsProductionEnvironment(isProductionEnvironment()),
		server.WithCron(cron),
		server.WithUpdateInfo(release.BuildUpdateInfo(release.Version)),
	)

	go runBackgroundJobs(context.Background(), srv, db, stats)

	if err := srv.Run(context.Background(), ":8080"); err != nil {
		panic(err)
	}
}

// TODO(optimization): Maybe optimize the endpoints a bit to reduce the number of round trips required?
// TODO: Add error checking for the calls to updateUsageData(...) that logs it/triggers an alert in prod, but is an error in test
