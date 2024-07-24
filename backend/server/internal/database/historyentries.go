package database

import (
	"context"
	"fmt"

	"github.com/ddworken/hishtory/shared"

	"gorm.io/gorm"
)

func (db *DB) CountApproximateHistoryEntries(ctx context.Context) (int64, error) {
	var numDbEntries int64
	err := db.WithContext(ctx).Raw("SELECT reltuples::bigint FROM pg_class WHERE relname = 'enc_history_entries'").Row().Scan(&numDbEntries)
	if err != nil {
		return 0, fmt.Errorf("DB Error: %w", err)
	}

	return numDbEntries, nil
}

func (db *DB) AllHistoryEntriesForUser(ctx context.Context, userID string) ([]*shared.EncHistoryEntry, error) {
	var historyEntries []*shared.EncHistoryEntry
	tx := db.WithContext(ctx).Where("user_id = ?", userID).Find(&historyEntries)

	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return historyEntries, nil
}

func (db *DB) HistoryEntriesForDevice(ctx context.Context, deviceID string, limit int) ([]*shared.EncHistoryEntry, error) {
	var historyEntries []*shared.EncHistoryEntry
	tx := db.WithContext(ctx).Where("device_id = ? AND read_count < ? AND NOT is_from_same_device", deviceID, limit).Find(&historyEntries)

	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return historyEntries, nil
}

func (db *DB) AddHistoryEntries(ctx context.Context, entries ...*shared.EncHistoryEntry) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, entry := range entries {
			resp := tx.Create(&entry)
			if resp.Error != nil {
				return fmt.Errorf("resp.Error: %w", resp.Error)
			}
		}
		return nil
	})
}

func (db *DB) AddHistoryEntriesForAllDevices(ctx context.Context, sourceDeviceId string, devices []*Device, entries []*shared.EncHistoryEntry) error {
	chunkSize := 1000
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, device := range devices {
			for _, entry := range entries {
				entry.DeviceId = device.DeviceId
				entry.IsFromSameDevice = sourceDeviceId == device.DeviceId
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

func (db *DB) Unsafe_DeleteAllHistoryEntries(ctx context.Context) error {
	tx := db.WithContext(ctx).Exec("DELETE FROM enc_history_entries")
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) IncrementEntryReadCountsForDevice(ctx context.Context, deviceID string) error {
	return db.WithContext(ctx).Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceID).Error
}
