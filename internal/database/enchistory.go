package database

import (
	"context"
	"fmt"
	"github.com/ddworken/hishtory/shared"
	"gorm.io/gorm"
)

func (db *DB) EncHistoryEntryCount(ctx context.Context) (int64, error) {
	var numDbEntries int64
	tx := db.WithContext(ctx).Model(&shared.EncHistoryEntry{}).Count(&numDbEntries)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return numDbEntries, nil
}

func (db *DB) EncHistoryEntriesForUser(ctx context.Context, userID string) ([]*shared.EncHistoryEntry, error) {
	var historyEntries []*shared.EncHistoryEntry
	tx := db.WithContext(ctx).Where("user_id = ?", userID).Find(&historyEntries)

	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return historyEntries, nil
}

func (db *DB) EncHistoryEntriesForDevice(ctx context.Context, deviceID string, limit int) ([]*shared.EncHistoryEntry, error) {
	var historyEntries []*shared.EncHistoryEntry
	tx := db.WithContext(ctx).Where("device_id = ? AND read_count < ?", deviceID, limit).Find(&historyEntries)

	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return historyEntries, nil
}

func (db *DB) EncHistoryCreate(ctx context.Context, entry *shared.EncHistoryEntry) error {
	tx := db.WithContext(ctx).Create(entry)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) EncHistoryCreateMulti(ctx context.Context, entries ...*shared.EncHistoryEntry) error {
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

func (db *DB) EncHistoryClear(ctx context.Context) error {
	tx := db.WithContext(ctx).Exec("DELETE FROM enc_history_entries")
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}
