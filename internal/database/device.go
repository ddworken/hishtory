package database

import (
	"context"
	"fmt"

	"github.com/ddworken/hishtory/shared"
)

func (db *DB) CountAllDevices(ctx context.Context) (int64, error) {
	var numDevices int64 = 0
	tx := db.WithContext(ctx).Model(&shared.Device{}).Count(&numDevices)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return numDevices, nil
}

func (db *DB) CountDevicesForUser(ctx context.Context, userID string) (int64, error) {
	var existingDevicesCount int64
	tx := db.WithContext(ctx).Model(&shared.Device{}).Where("user_id = ?", userID).Count(&existingDevicesCount)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return existingDevicesCount, nil
}

func (db *DB) CreateDevice(ctx context.Context, device *shared.Device) error {
	tx := db.WithContext(ctx).Create(device)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) DevicesForUser(ctx context.Context, userID string) ([]*shared.Device, error) {
	var devices []*shared.Device
	tx := db.WithContext(ctx).Where("user_id = ?", userID).Find(&devices)
	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return devices, nil
}
