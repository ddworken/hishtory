package database

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ddworken/hishtory/shared"
	"github.com/jackc/pgx/v4/stdlib"
	sqltrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/database/sql"
	gormtrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/gorm.io/gorm.v1"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log"
	"os"
	"runtime"
	"time"
)

type DB struct {
	*gorm.DB
}

const postgresDb = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"

func OpenDB(testEnvironment bool) (*DB, error) {
	if testEnvironment {
		db, err := gorm.Open(sqlite.Open("file::memory:?_journal_mode=WAL&cache=shared"), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
		underlyingDb, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("failed to access underlying DB: %w", err)
		}
		underlyingDb.SetMaxOpenConns(1)
		db.Exec("PRAGMA journal_mode = WAL")
		addDatabaseTables(db)
		return &DB{db}, nil
	}

	// The same as the default logger, except with a higher SlowThreshold
	customLogger := logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             1000 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
	})

	var db *gorm.DB
	if sqliteDb := os.Getenv("HISHTORY_SQLITE_DB"); sqliteDb != "" {
		var err error
		db, err = gorm.Open(sqlite.Open(sqliteDb), &gorm.Config{Logger: customLogger})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %v", err)
		}
	} else {
		postgresDb := fmt.Sprintf(postgresDb, os.Getenv("POSTGRESQL_PASSWORD"))
		if postgresDbEnv := os.Getenv("HISHTORY_POSTGRES_DB"); postgresDbEnv != "" {
			postgresDb = postgresDbEnv
		}
		sqltrace.Register("pgx", &stdlib.Driver{}, sqltrace.WithServiceName("hishtory-api"))
		sqlDb, err := sqltrace.Open("pgx", postgresDb)
		if err != nil {
			return nil, fmt.Errorf("sqltrace.Open: %w", err)
		}
		db, err = gormtrace.Open(postgres.New(postgres.Config{Conn: sqlDb}), &gorm.Config{Logger: customLogger})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	}
	addDatabaseTables(db)
	return &DB{db}, nil
}

func addDatabaseTables(db *gorm.DB) {
	db.AutoMigrate(&shared.EncHistoryEntry{})
	db.AutoMigrate(&shared.Device{})
	db.AutoMigrate(&UsageData{})
	db.AutoMigrate(&shared.DumpRequest{})
	db.AutoMigrate(&shared.DeletionRequest{})
	db.AutoMigrate(&shared.Feedback{})
}

func (db *DB) SqlDB() (*sql.DB, error) {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("db.DB.DB: %w", err)
	}

	return sqlDB, nil
}

func (db *DB) Ping() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("db.DB.DB: %w", err)
	}

	return sqlDB.Ping()
}

func (db *DB) Stats() (sql.DBStats, error) {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return sql.DBStats{}, fmt.Errorf("db.DB.DB: %w", err)
	}

	return sqlDB.Stats(), nil
}

func (db *DB) IncrementReadCount(ctx context.Context, userID, deviceID string) error {
	result := db.DB.WithContext(ctx).Exec("UPDATE deletion_requests SET read_count = read_count + 1 WHERE destination_device_id = ? AND user_id = ?", deviceID, userID)

	return formatGormError(result)
}

func (db *DB) IncrementReadCounts(ctx context.Context, deviceID string) error {
	result := db.DB.WithContext(ctx).Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceID)

	return formatGormError(result)
}

func (db *DB) GetDeletionRequests(ctx context.Context, userID, deviceID string) ([]*shared.DeletionRequest, error) {
	var deletionRequests []*shared.DeletionRequest

	result := db.DB.WithContext(ctx).Where("user_id = ? AND destination_device_id = ?", userID, deviceID).Find(&deletionRequests)

	return deletionRequests, formatGormError(result)
}

func (db *DB) GetDumpRequests(ctx context.Context, userID, deviceID string) ([]*shared.DumpRequest, error) {
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	result := db.DB.WithContext(ctx).Where("user_id = ? AND requesting_device_id != ?", userID, deviceID).Find(&dumpRequests)

	return dumpRequests, formatGormError(result)
}

func (db *DB) ApplyDeletionRequests(ctx context.Context, request shared.DeletionRequest) (int, error) {
	tx := db.DB.WithContext(ctx).Where("false")
	for _, message := range request.Messages.Ids {
		tx = tx.Or(db.DB.WithContext(ctx).Where("user_id = ? AND device_id = ? AND date = ?", request.UserId, message.DeviceId, message.Date))
	}
	result := tx.Delete(&shared.EncHistoryEntry{})

	return int(result.RowsAffected), formatGormError(result)
}

func (db *DB) GetHistoryEntriesForDevice(ctx context.Context, deviceID string) ([]*shared.EncHistoryEntry, error) {
	var historyEntries []*shared.EncHistoryEntry
	result := db.DB.WithContext(ctx).Where("device_id = ? AND read_count < 5", deviceID).Find(&historyEntries)

	return historyEntries, formatGormError(result)
}

func (db *DB) GetHistoryEntriesForUser(ctx context.Context, userID string) ([]*shared.EncHistoryEntry, error) {
	var historyEntries []*shared.EncHistoryEntry
	result := db.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&historyEntries)

	return historyEntries, formatGormError(result)
}

func (db *DB) UpdateNumQueries(ctx context.Context, userID, deviceID string) error {
	result := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET num_queries = COALESCE(num_queries, 0) + 1, last_queried = ? WHERE user_id = ? AND device_id = ?", time.Now(), userID, deviceID)

	return formatGormError(result)
}

func (db *DB) GetDevicesForUser(ctx context.Context, userID string) ([]*shared.Device, error) {
	var devices []*shared.Device
	result := db.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&devices)

	return devices, formatGormError(result)
}

func (db *DB) Clean(ctx context.Context) error {
	r := db.DB.WithContext(ctx).Exec("DELETE FROM enc_history_entries WHERE read_count > 10")
	if err := formatGormError(r); err != nil {
		return fmt.Errorf("failed to clean enc_history_entries: %w", err)
	}
	r = db.DB.WithContext(ctx).Exec("DELETE FROM deletion_requests WHERE read_count > 100")
	if err := formatGormError(r); err != nil {
		return fmt.Errorf("failed to clean deletion_requests: %w", err)
	}
	return nil
}

func (db *DB) DeepCleanDatabase(ctx context.Context) error {
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := tx.Exec(`
		CREATE TEMP TABLE temp_users_with_one_device AS (
			SELECT user_id
			FROM devices
			GROUP BY user_id
			HAVING COUNT(DISTINCT device_id) > 1
		)	
		`)
		if err := formatGormError(r); err != nil {
			return fmt.Errorf("failed to create temp_users_with_one_device table: %w", err)
		}
		r = tx.Exec(`
		CREATE TEMP TABLE temp_inactive_users AS (
			SELECT user_id
			FROM usage_data
			WHERE last_used <= (now() - INTERVAL '90 days')
		)	
		`)
		if err := formatGormError(r); err != nil {
			return fmt.Errorf("failed to create temp_inactive_users table: %w", err)
		}
		r = tx.Exec(`
		SELECT COUNT(*) FROM enc_history_entries WHERE
			date <= (now() - INTERVAL '90 days')
			AND user_id IN (SELECT * FROM temp_users_with_one_device)
			AND user_id IN (SELECT * FROM temp_inactive_users)
		`)
		if err := formatGormError(r); err != nil {
			return fmt.Errorf("failed to count enc_history_entries: %w", err)
		}
		fmt.Printf("Ran deep clean and deleted %d rows\n", r.RowsAffected)
		return nil
	})
}

func (db *DB) GetDistinctUserCount(ctx context.Context) (int64, error) {
	row := db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT devices.user_id) FROM devices").Row()
	var numDistinctUsers int64 = 0
	err := row.Scan(&numDistinctUsers)
	if err != nil {
		return 0, fmt.Errorf("failed to get distinct user count: %w", err)
	}

	return numDistinctUsers, nil
}

func formatGormError(result *gorm.DB) error {
	if result.Error == nil {
		return nil
	}

	_, filename, line, _ := runtime.Caller(1)
	return fmt.Errorf("DB error at %s:%d: %w", filename, line, result.Error)
}
