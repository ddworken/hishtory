package database

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ddworken/hishtory/shared"
	"github.com/jackc/pgx/v4/stdlib"
	_ "github.com/lib/pq"
	sqltrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/database/sql"
	gormtrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/gorm.io/gorm.v1"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

func OpenSQLite(dsn string, config *gorm.Config) (*DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), config)
	if err != nil {
		return nil, fmt.Errorf("gorm.Open: %w", err)
	}

	return &DB{db}, nil
}

func OpenPostgres(dsn string, config *gorm.Config) (*DB, error) {
	sqltrace.Register("pgx", &stdlib.Driver{}, sqltrace.WithServiceName("hishtory-api"))
	sqlDb, err := sqltrace.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqltrace.Open: %w", err)
	}
	db, err := gormtrace.Open(postgres.New(postgres.Config{Conn: sqlDb}), config)
	if err != nil {
		return nil, fmt.Errorf("gormtrace.Open: %w", err)
	}

	return &DB{db}, nil
}

func (db *DB) AddDatabaseTables() error {
	models := []any{
		&shared.EncHistoryEntry{},
		&shared.Device{},
		&shared.UsageData{},
		&shared.DumpRequest{},
		&shared.DeletionRequest{},
		&shared.Feedback{},
	}

	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("db.AutoMigrate: %w", err)
		}
	}

	return nil
}

func (db *DB) Close() error {
	rawDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("db.DB.DB: %w", err)
	}

	if err := rawDB.Close(); err != nil {
		return fmt.Errorf("rawDB.Close: %w", err)
	}

	return nil
}

func (db *DB) Ping() error {
	rawDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("db.DB.DB: %w", err)
	}

	if err := rawDB.Ping(); err != nil {
		return fmt.Errorf("rawDB.Ping: %w", err)
	}

	return nil
}

func (db *DB) Stats() (sql.DBStats, error) {
	rawDB, err := db.DB.DB()
	if err != nil {
		return sql.DBStats{}, fmt.Errorf("db.DB.DB: %w", err)
	}

	return rawDB.Stats(), nil
}

func (db *DB) DistinctUsers(ctx context.Context) (int64, error) {
	row := db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT devices.user_id) FROM devices").Row()
	var numDistinctUsers int64
	err := row.Scan(&numDistinctUsers)
	if err != nil {
		return 0, fmt.Errorf("row.Scan: %w", err)
	}

	return numDistinctUsers, nil
}

func (db *DB) DevicesCountForUser(ctx context.Context, userID string) (int64, error) {
	var existingDevicesCount int64
	tx := db.WithContext(ctx).Model(&shared.Device{}).Where("user_id = ?", userID).Count(&existingDevicesCount)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return existingDevicesCount, nil
}

func (db *DB) DevicesCount(ctx context.Context) (int64, error) {
	var numDevices int64 = 0
	tx := db.WithContext(ctx).Model(&shared.Device{}).Count(&numDevices)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return numDevices, nil
}

func (db *DB) DeviceCreate(ctx context.Context, device *shared.Device) error {
	tx := db.WithContext(ctx).Create(device)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) DumpRequestCreate(ctx context.Context, req *shared.DumpRequest) error {
	tx := db.WithContext(ctx).Create(req)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) EncHistoryEntryCount(ctx context.Context) (int64, error) {
	var numDbEntries int64
	tx := db.WithContext(ctx).Model(&shared.EncHistoryEntry{}).Count(&numDbEntries)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return numDbEntries, nil
}
