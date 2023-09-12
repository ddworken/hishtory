package database

import (
	"context"
	"fmt"
	"github.com/ddworken/hishtory/shared"
	"gorm.io/gorm"
)

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

func (db *DB) DeviceEntriesCreateChunk(ctx context.Context, devices []*shared.Device, entries []*shared.EncHistoryEntry, chunkSize int) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, device := range devices {
			for _, entry := range entries {
				entry.DeviceId = device.DeviceId
			}
			// Chunk the inserts to prevent the `extended protocol limited to 65535 parameters` error
			for _, entriesChunk := range shared.Chunks(entries, chunkSize) {
				resp := tx.Create(&entriesChunk)
				if resp.Error != nil {
					return fmt.Errorf("resp.Error: %w", resp.Error)
				}
			}
		}
		return nil
	})
}

func (db *DB) DevicesForUser(ctx context.Context, userID string) ([]*shared.Device, error) {
	var devices []*shared.Device
	tx := db.WithContext(ctx).Where("user_id = ?", userID).Find(&devices)
	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return devices, nil
}

func (db *DB) DeviceIncrementReadCounts(ctx context.Context, deviceID string) error {
	return db.WithContext(ctx).Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceID).Error
}
