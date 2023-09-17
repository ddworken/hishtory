package database

import (
	"context"
	"fmt"
	"time"

	"github.com/ddworken/hishtory/shared"
)

func (db *DB) UsageDataFindByUserAndDevice(ctx context.Context, userId, deviceId string) ([]shared.UsageData, error) {
	var usageData []shared.UsageData

	tx := db.DB.WithContext(ctx).Where("user_id = ? AND device_id = ?", userId, deviceId).Find(&usageData)
	if tx.Error != nil {
		return nil, fmt.Errorf("db.WithContext.Where.Find: %w", tx.Error)
	}

	if err := db.Where("user_id = ? AND device_id = ?", userId, deviceId).First(&usageData).Error; err != nil {
		return nil, fmt.Errorf("db.Where: %w", err)
	}

	return usageData, nil
}

func (db *DB) CreateUsageData(ctx context.Context, usageData *shared.UsageData) error {
	tx := db.DB.WithContext(ctx).Create(usageData)
	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Create: %w", tx.Error)
	}

	return nil
}

// UpdateUsageData updates the entry for a given userID/deviceID pair with the lastUsed and lastIP values
func (db *DB) UpdateUsageData(ctx context.Context, userId, deviceId string, lastUsed time.Time, lastIP string) error {
	tx := db.DB.WithContext(ctx).Model(&shared.UsageData{}).
		Where("user_id = ? AND device_id = ?", userId, deviceId).
		Update("last_used", lastUsed).
		Update("last_ip", lastIP)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Model.Where.Update: %w", tx.Error)
	}

	return nil
}

func (db *DB) UpdateUsageDataForNumEntriesHandled(ctx context.Context, userId, deviceId string, numEntriesHandled int) error {
	tx := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET num_entries_handled = COALESCE(num_entries_handled, 0) + ? WHERE user_id = ? AND device_id = ?", numEntriesHandled, userId, deviceId)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Exec: %w", tx.Error)
	}

	return nil
}

func (db *DB) UpdateUsageDataClientVersion(ctx context.Context, userID, deviceID string, version string) error {
	tx := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET version = ? WHERE user_id = ? AND device_id = ?", version, userID, deviceID)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Exec: %w", tx.Error)
	}

	return nil
}

func (db *DB) UpdateUsageDataNumberQueries(ctx context.Context, userID, deviceID string) error {
	tx := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET num_queries = COALESCE(num_queries, 0) + 1, last_queried = ? WHERE user_id = ? AND device_id = ?", time.Now(), userID, deviceID)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Exec: %w", tx.Error)
	}

	return nil
}

type UsageDataStats struct {
	RegistrationDate time.Time
	NumDevices       int
	NumEntries       int
	LastUsedDate     time.Time
	IpAddresses      string
	NumQueries       int
	LastQueried      time.Time
	Versions         string
}

const usageDataStatsQuery = `
	SELECT 
		MIN(devices.registration_date) as registration_date, 
		COUNT(DISTINCT devices.device_id) as num_devices,
		SUM(usage_data.num_entries_handled) as num_history_entries,
		MAX(usage_data.last_used) as last_active,
		COALESCE(STRING_AGG(DISTINCT usage_data.last_ip, ', ') FILTER (WHERE usage_data.last_ip != 'Unknown' AND usage_data.last_ip != 'UnknownIp'), 'Unknown')  as ip_addresses,
		COALESCE(SUM(usage_data.num_queries), 0) as num_queries,
		COALESCE(MAX(usage_data.last_queried), 'January 1, 1970') as last_queried,
		STRING_AGG(DISTINCT usage_data.version, ', ') as versions
	FROM devices
	INNER JOIN usage_data ON devices.device_id = usage_data.device_id
	GROUP BY devices.user_id
	ORDER BY registration_date
	`

func (db *DB) UsageDataStats(ctx context.Context) ([]*UsageDataStats, error) {
	var resp []*UsageDataStats

	rows, err := db.DB.WithContext(ctx).Raw(usageDataStatsQuery).Rows()
	if err != nil {
		return nil, fmt.Errorf("db.WithContext.Raw.Rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var usageData UsageDataStats

		err := rows.Scan(
			&usageData.RegistrationDate,
			&usageData.NumDevices,
			&usageData.NumEntries,
			&usageData.LastUsedDate,
			&usageData.IpAddresses,
			&usageData.NumQueries,
			&usageData.LastQueried,
			&usageData.Versions,
		)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}

		resp = append(resp, &usageData)
	}

	return resp, nil
}

func (db *DB) UsageDataTotal(ctx context.Context) (int64, error) {
	type numEntriesProcessed struct {
		Total int
	}
	nep := numEntriesProcessed{}

	tx := db.WithContext(ctx).Model(&shared.UsageData{}).Select("SUM(num_entries_handled) as total").Find(&nep)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return int64(nep.Total), nil
}

func (db *DB) CountActiveInstalls(ctx context.Context, since time.Duration) (int64, error) {
	var activeInstalls int64
	tx := db.WithContext(ctx).Model(&shared.UsageData{}).Where("last_used > ?", time.Now().Add(-since)).Count(&activeInstalls)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return activeInstalls, nil
}

func (db *DB) CountQueryUsers(ctx context.Context, since time.Duration) (int64, error) {
	var activeQueryUsers int64
	tx := db.WithContext(ctx).Model(&shared.UsageData{}).Where("last_queried > ?", time.Now().Add(-since)).Count(&activeQueryUsers)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return activeQueryUsers, nil
}

func (db *DB) DateOfLastRegistration(ctx context.Context) (string, error) {
	var lastRegistration string
	row := db.WithContext(ctx).Raw("SELECT to_char(max(registration_date), 'DD Month YYYY HH24:MI') FROM devices").Row()
	if err := row.Scan(&lastRegistration); err != nil {
		return "", fmt.Errorf("row.Scan: %w", err)
	}

	return lastRegistration, nil
}
