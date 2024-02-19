package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
		&Device{},
		&UsageData{},
		&shared.DumpRequest{},
		&shared.DeletionRequest{},
		&shared.Feedback{},
		&ActiveUserStats{},
	}

	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("db.AutoMigrate: %w", err)
		}
	}

	return nil
}

func (db *DB) CreateIndices() error {
	// Note: If adding a new index here, consider manually running it on the prod DB using CONCURRENTLY to
	// make server startup non-blocking. The benefit of this function is primarily for other people so they
	// don't have to manually create these indexes.
	// Example:
	// CREATE INDEX CONCURRENTLY IF NOT EXISTS redact_idx ON enc_history_entries USING btree(user_id, device_id, date)
	indices := []struct {
		name    string
		table   string
		columns []string
	}{
		{"entry_id_idx", "enc_history_entries", []string{"encrypted_id"}},
		{"device_id_idx", "enc_history_entries", []string{"device_id"}},
		{"read_count_idx", "enc_history_entries", []string{"read_count"}},
		{"redact_idx", "enc_history_entries", []string{"user_id", "device_id", "date"}},
		{"del_user_idx", "deletion_requests", []string{"user_id"}},
	}
	for _, index := range indices {
		sql := ""
		if db.Name() == "sqlite" {
			sql = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", index.name, index.table, strings.Join(index.columns, ","))
		} else {
			sql = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING btree(%s)", index.name, index.table, strings.Join(index.columns, ","))
		}
		r := db.Exec(sql)
		if r.Error != nil {
			return fmt.Errorf("failed to execute index creation sql=%#v: %w", index, r.Error)
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

func (db *DB) SetMaxIdleConns(n int) error {
	rawDB, err := db.DB.DB()
	if err != nil {
		return err
	}

	rawDB.SetMaxIdleConns(n)

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

func (db *DB) UserAlreadyExist(ctx context.Context, userID string) (bool, error) {
	var cnt int64
	tx := db.WithContext(ctx).Table("devices").Where("user_id = ?", userID).Count(&cnt)
	if tx.Error != nil {
		return false, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	if cnt > 0 {
		return true, nil
	}
	return false, nil
}

func (db *DB) DumpRequestCreate(ctx context.Context, req *shared.DumpRequest) error {
	tx := db.WithContext(ctx).Create(req)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) DumpRequestForUserAndDevice(ctx context.Context, userID, deviceID string) ([]*shared.DumpRequest, error) {
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	tx := db.WithContext(ctx).Where("user_id = ? AND requesting_device_id != ?", userID, deviceID).Find(&dumpRequests)
	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return dumpRequests, nil
}

func (db *DB) DumpRequestDeleteForUserAndDevice(ctx context.Context, userID, deviceID string) error {
	tx := db.WithContext(ctx).Delete(&shared.DumpRequest{}, "user_id = ? AND requesting_device_id = ?", userID, deviceID)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) ApplyDeletionRequestsToBackend(ctx context.Context, requests []*shared.DeletionRequest) (int64, error) {
	if len(requests) == 0 {
		return 0, nil
	}
	deletedMessages := make([]shared.MessageIdentifier, 0)
	userId := requests[0].UserId
	for _, request := range requests {
		deletedMessages = append(deletedMessages, request.Messages.Ids...)
		if request.UserId != userId {
			return 0, fmt.Errorf("deletion requests contain mismatched user IDs: %s vs %s", request.UserId, userId)
		}
	}
	return db.DeleteMessagesFromBackend(ctx, userId, deletedMessages)
}

func (db *DB) UninstallDevice(ctx context.Context, userId, deviceId string) (int64, error) {
	// Note that this is deleting entries that are destined to be *read* by this device. If there are other devices on this account,
	// those queues are unaffected.
	r1 := db.WithContext(ctx).Where("user_id = ? AND device_id = ?", userId, deviceId).Delete(&shared.EncHistoryEntry{})
	if r1.Error != nil {
		return 0, fmt.Errorf("UninstallDevice: failed to delete entries: %w", r1.Error)
	}
	// Note that this is deleting deletion requests that were destined to be processed by this device. So if this device deleted anything,
	// those requests are still pending.
	r2 := db.WithContext(ctx).Where("user_id = ? AND destination_device_id = ?", userId, deviceId).Delete(&shared.DeletionRequest{})
	if r2.Error != nil {
		return 0, fmt.Errorf("UninstallDevice: failed to delete deletion requests: %w", r2.Error)
	}
	// Similarly, note that this is deleting dump requests from this device so that other devices won't respond to these.
	r3 := db.WithContext(ctx).Where("user_id = ? AND requesting_device_id = ?", userId, deviceId).Delete(&shared.DumpRequest{})
	if r3.Error != nil {
		return 0, fmt.Errorf("UninstallDevice: failed to delete dump requests: %w", r3.Error)
	}
	// Lastly, update the flag so that we know this device has been deleted
	r := db.WithContext(ctx).Model(&Device{}).Where("user_id = ? AND device_id = ?", userId, deviceId).Update("uninstall_date", time.Now().UTC())
	if r.Error != nil {
		return 0, fmt.Errorf("UnisntallDevice: failed to update uninstall_date: %w", r.Error)
	}
	return r1.RowsAffected + r2.RowsAffected + r3.RowsAffected, nil
}

func (db *DB) DeleteMessagesFromBackend(ctx context.Context, userId string, deletedMessages []shared.MessageIdentifier) (int64, error) {
	tx := db.WithContext(ctx).Where("false")
	for _, message := range deletedMessages {
		if userId == "" {
			return 0, fmt.Errorf("failed to delete entry because userId is empty")
		}
		if message.DeviceId == "" {
			// The DeviceId is empty, so there is nothing to do for this since it won't match anything
			continue
		}
		if message.EndTime != (time.Time{}) && message.EntryId != "" {
			// Note that we do an OR with date or the ID matching since the ID is not always recorded for older history entries.
			tx = tx.Or(db.WithContext(ctx).Where("user_id = ? AND (date = ? OR encrypted_id = ?)", userId, message.EndTime, message.EntryId))
		} else if message.EndTime != (time.Time{}) && message.EntryId == "" {
			tx = tx.Or(db.WithContext(ctx).Where("user_id = ? AND (date = ?)", userId, message.EndTime))
		} else if message.EndTime == (time.Time{}) && message.EntryId != "" {
			tx = tx.Or(db.WithContext(ctx).Where("user_id = ? AND (encrypted_id = ?)", userId, message.EntryId))
		} else {
			return 0, fmt.Errorf("failed to delete entry because message.EndTime=%#v and message.EntryId=%#v are both empty", message.EndTime, message.EntryId)
		}
	}
	result := tx.Delete(&shared.EncHistoryEntry{})
	if result.Error != nil {
		return 0, fmt.Errorf("result.Error: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (db *DB) DeletionRequestInc(ctx context.Context, userID, deviceID string) error {
	tx := db.WithContext(ctx).Exec("UPDATE deletion_requests SET read_count = read_count + 1 WHERE user_id = ? AND destination_device_id = ?", userID, deviceID)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) DeletionRequestsForUserAndDevice(ctx context.Context, userID, deviceID string) ([]*shared.DeletionRequest, error) {
	var deletionRequests []*shared.DeletionRequest
	tx := db.WithContext(ctx).Where("user_id = ? AND destination_device_id = ?", userID, deviceID).Find(&deletionRequests)
	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return deletionRequests, nil
}

func (db *DB) DeletionRequestCreate(ctx context.Context, request *shared.DeletionRequest) error {
	userID := request.UserId

	devices, err := db.DevicesForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("db.DevicesForUser: %w", err)
	}

	if len(devices) == 0 {
		return fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", userID)
	}

	fmt.Printf("db.DeletionRequestCreate: Found %d devices\n", len(devices))

	err = db.Transaction(func(tx *gorm.DB) error {
		for _, device := range devices {
			request.DestinationDeviceId = device.DeviceId
			tx := tx.WithContext(ctx).Create(&request)
			if tx.Error != nil {
				return fmt.Errorf("create: tx.Error: %w", tx.Error)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	numDeleted, err := db.DeleteMessagesFromBackend(ctx, userID, request.Messages.Ids)
	if err != nil {
		return fmt.Errorf("db.DeleteMessagesFromBackend: %w", err)
	}
	fmt.Printf("addDeletionRequestHandler: Deleted %d rows in the backend\n", numDeleted)
	return nil
}

func (db *DB) FeedbackCreate(ctx context.Context, feedback *shared.Feedback) error {
	tx := db.WithContext(ctx).Create(feedback)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) Clean(ctx context.Context) error {
	r := db.WithContext(ctx).Exec("DELETE FROM enc_history_entries WHERE read_count > 10")
	if r.Error != nil {
		return r.Error
	}
	r = db.WithContext(ctx).Exec("DELETE FROM deletion_requests WHERE read_count > 100")
	if r.Error != nil {
		return r.Error
	}

	return nil
}

func extractInt64FromRow(row *sql.Row) (int64, error) {
	var ret int64
	err := row.Scan(&ret)
	if err != nil {
		return 0, fmt.Errorf("extractInt64FromRow: %w", err)
	}
	return ret, nil
}

type ActiveUserStats struct {
	Date                    time.Time
	TotalNumDevices         int64
	TotalNumUsers           int64
	DailyActiveSubmitUsers  int64
	DailyActiveQueryUsers   int64
	WeeklyActiveSubmitUsers int64
	WeeklyActiveQueryUsers  int64
	DailyInstalls           int64
	DailyUninstalls         int64
}

func (db *DB) GenerateAndStoreActiveUserStats(ctx context.Context) error {
	if db.DB.Name() == "sqlite" {
		// Not supported on sqlite
		return nil
	}

	totalNumDevices, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT devices.device_id) FROM devices WHERE NOT is_integration_test_device").Row())
	if err != nil {
		return err
	}
	totalNumUsers, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT devices.user_id) FROM devices WHERE NOT is_integration_test_device").Row())
	if err != nil {
		return err
	}
	dauSubmit, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT user_id) FROM usage_data WHERE last_used > (now()::date-1)::timestamp AND device_id NOT IN (SELECT device_id FROM devices WHERE is_integration_test_device)").Row())
	if err != nil {
		return err
	}
	dauQuery, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT user_id) FROM usage_data WHERE last_queried > (now()::date-1)::timestamp AND device_id NOT IN (SELECT device_id FROM devices WHERE is_integration_test_device)").Row())
	if err != nil {
		return err
	}
	wauSubmit, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT user_id) FROM usage_data WHERE last_used > (now()::date-7)::timestamp AND device_id NOT IN (SELECT device_id FROM devices WHERE is_integration_test_device)").Row())
	if err != nil {
		return err
	}
	wauQuery, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT user_id) FROM usage_data WHERE last_queried > (now()::date-7)::timestamp AND device_id NOT IN (SELECT device_id FROM devices WHERE is_integration_test_device)").Row())
	if err != nil {
		return err
	}
	dailyInstalls, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT count(distinct device_id) FROM devices WHERE registration_date > (now()::date-1)::timestamp AND NOT is_integration_test_device").Row())
	if err != nil {
		return err
	}
	dailyUninstalls, err := extractInt64FromRow(db.WithContext(ctx).Raw("SELECT COUNT(*) FROM feedbacks WHERE date > (now()::date-1)::timestamp AND user_id NOT IN (SELECT user_id FROM devices WHERE is_integration_test_device)").Row())
	if err != nil {
		return err
	}

	return db.Create(ActiveUserStats{
		Date:                    time.Now(),
		TotalNumDevices:         totalNumDevices,
		TotalNumUsers:           totalNumUsers,
		DailyActiveSubmitUsers:  dauSubmit,
		DailyActiveQueryUsers:   dauQuery,
		WeeklyActiveSubmitUsers: wauSubmit,
		WeeklyActiveQueryUsers:  wauQuery,
		DailyInstalls:           dailyInstalls,
		DailyUninstalls:         dailyUninstalls,
	}).Error
}

func (db *DB) DeepClean(ctx context.Context) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete entries for users that have one device and are inactive
		r := tx.Exec(`
		CREATE TEMP TABLE temp_users_with_one_device AS (
			SELECT user_id
			FROM devices
			GROUP BY user_id
			HAVING COUNT(DISTINCT device_id) = 1
		)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to create list of single device users: %w", r.Error)
		}
		r = tx.Exec(`
		CREATE TEMP TABLE temp_inactive_users AS (
			SELECT user_id
			FROM usage_data
			WHERE last_used <= (now() - INTERVAL '180 days')
		)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to create list of inactive users: %w", r.Error)
		}
		r = tx.Exec(`
		DELETE FROM enc_history_entries WHERE
			date <= (now() - INTERVAL '180 days')
			AND user_id IN (SELECT * FROM temp_users_with_one_device)
			AND user_id IN (SELECT * FROM temp_inactive_users)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to delete entries for inactive users: %w", r.Error)
		}
		fmt.Printf("Ran deep clean for inactive users and deleted %d rows\n", r.RowsAffected)
		return nil
	})
	if err != nil {
		return err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete entries for users that have tons and tons of entries (e.g. due to a mis-use or misconfiguration)
		r := tx.Exec(`
		CREATE TEMP TABLE users_with_too_many_entries AS (
			SELECT user_id
			FROM enc_history_entries
			GROUP BY user_id
			HAVING count(*) > 50000000
		)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to create list of users with too many entries: %w", r.Error)
		}
		r = tx.Raw(`
		DELETE FROM enc_history_entries WHERE
			date <= (now() - INTERVAL '90 days')
			AND user_id IN (SELECT * FROM users_with_too_many_entries)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to delete entries for overly active users: %w", r.Error)
		}
		fmt.Printf("Ran deep clean for overly active users and deleted %d rows\n", r.RowsAffected)
		return nil
	})
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete entries for integration test users
		r := tx.Exec(`
		CREATE TEMP TABLE temp_inactive_devices AS (
			SELECT device_id
			FROM usage_data
			WHERE last_used <= (now() - INTERVAL '90 days')
		)	
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to create list of inactive devices: %w", r.Error)
		}
		r = tx.Exec(`
		CREATE TEMP TABLE temp_integration_devices AS (
			SELECT device_id
			FROM devices
			WHERE is_integration_test_device
		)	
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to create list of integration test devices: %w", r.Error)
		}
		r = tx.Raw(`
		DELETE FROM enc_history_entries WHERE
			device_id IN (SELECT * FROM temp_inactive_devices)
			AND device_id IN (SELECT * FROM temp_integration_devices)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to delete entries for integration test devices: %w", r.Error)
		}
		r = tx.Raw(`
		DELETE FROM devices WHERE
			device_id IN (SELECT * FROM temp_inactive_devices)
			AND device_id IN (SELECT * FROM temp_integration_devices)
		`)
		if r.Error != nil {
			return fmt.Errorf("failed to delete integration test devices: %w", r.Error)
		}
		fmt.Printf("Ran deep clean for integration test devices and deleted %d rows\n", r.RowsAffected)
		return nil
	})
}
