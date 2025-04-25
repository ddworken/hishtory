package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ddworken/hishtory/backend/server/internal/database"
	"github.com/ddworken/hishtory/backend/server/internal/release"
	"github.com/ddworken/hishtory/backend/server/internal/server"

	"github.com/DataDog/datadog-go/statsd"
	_ "github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	PostgresDb   = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"
	StatsdSocket = "unix:///var/run/datadog/dsd.socket"
)

// Filled in via ldflags with the latest released version as of the server getting built
var ReleaseVersion string

func isTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func isProductionEnvironment() bool {
	return os.Getenv("HISHTORY_ENV") == "prod"
}

func getLoggerConfig() logger.Interface {
	// The same as the default logger, except with a higher SlowThreshold
	return logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             1000 * time.Millisecond,
		LogLevel:                  logger.Info,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
	})
}

func OpenDB() (dbPtr *database.DB, err error) {
	if isTestEnvironment() {
		db, err := database.OpenSQLite("file::memory:?_journal_mode=WAL&cache=shared", &gorm.Config{Logger: getLoggerConfig()})
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

	var sqliteDb string
	if os.Getenv("HISHTORY_SQLITE_DB") != "" {
		sqliteDb = os.Getenv("HISHTORY_SQLITE_DB")
	}

	config := gorm.Config{Logger: getLoggerConfig()}

	fmt.Println("Connecting to DB")
	if sqliteDb != "" {
		var err error
		dbPtr, err = database.OpenSQLite(sqliteDb, &config)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	} else {
		var err error
		postgresDb := fmt.Sprintf(PostgresDb, os.Getenv("POSTGRESQL_PASSWORD"))
		if os.Getenv("HISHTORY_POSTGRES_DB") != "" {
			postgresDb = os.Getenv("HISHTORY_POSTGRES_DB")
		}

		dbPtr, err = database.OpenPostgres(postgresDb, &config)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	}
	if !isProductionEnvironment() {
		fmt.Println("AutoMigrating DB tables")
		err := dbPtr.AddDatabaseTables()
		if err != nil {
			return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
		}
		err = dbPtr.CreateIndices()
		if err != nil {
			return nil, fmt.Errorf("failed to create indices: %w", err)
		}
	}
	if os.Getenv("HISHTORY_COMPOSE_TEST") != "" {
		// Run an extra round of migrations to test the migration code path to prevent issues like #241
		fmt.Println("AutoMigrating DB tables a second time for test coverage")
		err := dbPtr.AddDatabaseTables()
		if err != nil {
			return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
		}
		err = dbPtr.CreateIndices()
		if err != nil {
			return nil, fmt.Errorf("failed to create indices: %w", err)
		}
	}
	return dbPtr, nil
}

var (
	LAST_USER_STATS_RUN = time.Unix(0, 0)
	LAST_DEEP_CLEAN     = time.Unix(0, 0)
)

func cron(ctx context.Context, db *database.DB, stats *statsd.Client) (err error) {
	// Determine the latest released version of hishtory to serve via the /api/v1/download
	// endpoint for hishtory updates.
	if err := release.UpdateReleaseVersion(); err != nil {
		return fmt.Errorf("updateReleaseVersion: %w", err)
	}

	// Clean the DB to remove entries that have already been read
	if err := db.Clean(ctx); err != nil {
		return fmt.Errorf("db.Clean: %w", err)
	}

	// Flush out datadog statsd
	if stats != nil {
		if err := stats.Flush(); err != nil {
			return fmt.Errorf("stats.Flush: %w", err)
		}
	}

	// Run a deep clean less often to cover some more edge cases that hurt DB performance
	if time.Since(LAST_DEEP_CLEAN) > 24*3*time.Hour {
		LAST_DEEP_CLEAN = time.Now()
		if isProductionEnvironment() {
			if err := db.DeepClean(ctx); err != nil {
				return fmt.Errorf("db.DeepClean: %w", err)
			}
		}
		if !isProductionEnvironment() && !isTestEnvironment() {
			if err := db.SelfHostedDeepClean(ctx); err != nil {
				return fmt.Errorf("db.SelfHostedDeepClean: %w", err)
			}
		}
	}

	// Collect and store metrics on active users so we can track trends over time. This doesn't
	// have to be run as often, so only run it periodically.
	if time.Since(LAST_USER_STATS_RUN) > 12*time.Hour {
		LAST_USER_STATS_RUN = time.Now()
		if err := db.GenerateAndStoreActiveUserStats(ctx); err != nil {
			return fmt.Errorf("db.GenerateAndStoreActiveUserStats: %w", err)
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
		server.TrackUsageData(isProductionEnvironment() || isTestEnvironment() || os.Getenv("HISHTORY_ENABLE_USAGE_STATS") != ""),
	)

	go runBackgroundJobs(context.Background(), srv, db, stats)

	port := os.Getenv("HISHTORY_SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	if err := srv.Run(context.Background(), ":"+port); err != nil {
		panic(err)
	}
}
