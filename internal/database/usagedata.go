package database

import (
	"context"
	"fmt"
	"github.com/ddworken/hishtory/shared"
	"time"
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

func (db *DB) UsageDataCreate(ctx context.Context, usageData *shared.UsageData) error {
	tx := db.DB.WithContext(ctx).Create(usageData)
	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Create: %w", tx.Error)
	}

	return nil
}

// UsageDataUpdate updates the entry for a given userID/deviceID pair with the lastUsed and lastIP values
func (db *DB) UsageDataUpdate(ctx context.Context, userId, deviceId string, lastUsed time.Time, lastIP string) error {
	tx := db.DB.WithContext(ctx).Model(&shared.UsageData{}).
		Where("user_id = ? AND device_id = ?", userId, deviceId).
		Update("last_used", lastUsed).
		Update("last_ip", lastIP)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Model.Where.Update: %w", tx.Error)
	}

	return nil
}

func (db *DB) UsageDataUpdateNumEntriesHandled(ctx context.Context, userId, deviceId string, numEntriesHandled int) error {
	tx := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET num_entries_handled = COALESCE(num_entries_handled, 0) + ? WHERE user_id = ? AND device_id = ?", numEntriesHandled, userId, deviceId)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Exec: %w", tx.Error)
	}

	return nil
}

func (db *DB) UsageDataUpdateVersion(ctx context.Context, userID, deviceID string, version string) error {
	tx := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET version = ? WHERE user_id = ? AND device_id = ?", version, userID, deviceID)

	if tx.Error != nil {
		return fmt.Errorf("db.WithContext.Exec: %w", tx.Error)
	}

	return nil
}

func (db *DB) UsageDataUpdateNumQueries(ctx context.Context, userID, deviceID string) error {
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
