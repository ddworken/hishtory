package database

import (
	"context"
	"fmt"
	"time"
)

type Device struct {
	UserId   string `json:"user_id"`
	DeviceId string `json:"device_id"`
	// The IP address that was used to register the device. Recorded so
	// that I can count how many people are using hishtory and roughly
	// from where. If you would like this deleted, please email me at
	// david@daviddworken.com and I can clear it from your device entries.
	RegistrationIp   string    `json:"registration_ip"`
	RegistrationDate time.Time `json:"registration_date"`
	// Test devices, that should be aggressively cleaned from the DB
	IsIntegrationTestDevice bool `json:"is_integration_test_device"`
	// Whether this device was uninstalled
	UninstallDate time.Time `json:"uninstall_date"`
}

func (db *DB) CountAllDevices(ctx context.Context) (int64, error) {
	var numDevices int64 = 0
	tx := db.WithContext(ctx).Model(&Device{}).Count(&numDevices)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return numDevices, nil
}

func (db *DB) CountDevicesForUser(ctx context.Context, userID string) (int64, error) {
	var existingDevicesCount int64
	tx := db.WithContext(ctx).Model(&Device{}).Where("user_id = ?", userID).Count(&existingDevicesCount)
	if tx.Error != nil {
		return 0, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return existingDevicesCount, nil
}

func (db *DB) CreateDevice(ctx context.Context, device *Device) error {
	tx := db.WithContext(ctx).Create(device)
	if tx.Error != nil {
		return fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return nil
}

func (db *DB) DevicesForUser(ctx context.Context, userID string) ([]*Device, error) {
	var devices []*Device
	tx := db.WithContext(ctx).Where("user_id = ? AND (uninstall_date IS NULL OR uninstall_date < '1971-01-01')", userID).Find(&devices)
	if tx.Error != nil {
		return nil, fmt.Errorf("tx.Error: %w", tx.Error)
	}

	return devices, nil
}
