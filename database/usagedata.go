package database

import (
	"context"
	"time"
)

type UsageData struct {
	UserId            string    `json:"user_id" gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	DeviceId          string    `json:"device_id"  gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	LastUsed          time.Time `json:"last_used"`
	LastIp            string    `json:"last_ip"`
	NumEntriesHandled int       `json:"num_entries_handled"`
	LastQueried       time.Time `json:"last_queried"`
	NumQueries        int       `json:"num_queries"`
	Version           string    `json:"version"`
}

func (db *DB) GetUsageData(ctx context.Context, userID, deviceID string) ([]UsageData, error) {
	var usageData []UsageData
	result := db.DB.WithContext(ctx).Where("user_id = ? AND device_id = ?", userID, deviceID).Find(&usageData)

	return usageData, formatGormError(result)
}

func (db *DB) CreateUsageData(ctx context.Context, userID, deviceID, version string, numEntriesHandled int) error {
	result := db.DB.WithContext(ctx).Create(&UsageData{
		UserId:            userID,
		DeviceId:          deviceID,
		LastUsed:          time.Now(),
		NumEntriesHandled: numEntriesHandled,
		Version:           version,
	})

	return formatGormError(result)
}

func (db *DB) UpdateUsageData(ctx context.Context, usageData []UsageData, userID, deviceID, version string, numEntriesHandled int, remoteAddr string) error {
	usage := usageData[0]
	result := db.DB.WithContext(ctx).Model(&UsageData{}).Where("user_id = ? AND device_id = ?", userID, deviceID).Update("last_used", time.Now()).Update("last_ip", remoteAddr)
	if result.Error != nil {
		return formatGormError(result)
	}

	if numEntriesHandled > 0 {
		result := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET num_entries_handled = COALESCE(num_entries_handled, 0) + ? WHERE user_id = ? AND device_id = ?", numEntriesHandled, userID, deviceID)
		if result.Error != nil {
			return formatGormError(result)
		}
	}

	if usage.Version != version {
		result := db.DB.WithContext(ctx).Exec("UPDATE usage_data SET version = ? WHERE user_id = ? AND device_id = ?", version, userID, deviceID)
		if result.Error != nil {
			return formatGormError(result)
		}
	}

	return nil
}

func (db *DB) GetLastUsedSince(ctx context.Context, when time.Time) (int64, error) {
	var count int64
	resp := db.DB.WithContext(ctx).Model(&UsageData{}).Where("last_used > ?", when).Count(&count)

	return count, formatGormError(resp)
}

func (db *DB) GetLastQueriedSince(ctx context.Context, when time.Time) (int64, error) {
	var count int64
	resp := db.DB.WithContext(ctx).Model(&UsageData{}).Where("last_queried > ?", when).Count(&count)

	return count, formatGormError(resp)
}
