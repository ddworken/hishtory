// Package backend provides the interface and implementations for syncing history entries
// across devices. It supports multiple backend types:
//   - HTTPBackend: syncs via the hishtory server API (default)
//   - S3Backend: syncs directly to an S3 bucket (self-hosted option)
package backend

import (
	"context"

	"github.com/ddworken/hishtory/shared"
)

// SyncBackend defines the interface for syncing history entries.
// This interface is implemented by:
//   - HTTPBackend: wraps existing API calls to api.hishtory.dev
//   - S3Backend: syncs directly to an S3 bucket
type SyncBackend interface {
	// RegisterDevice registers a new device for the user.
	// For HTTP: GET /api/v1/register?user_id=...&device_id=...
	// For S3: Creates device entry in devices.json, may create dump request
	RegisterDevice(ctx context.Context, userId, deviceId string) error

	// Bootstrap returns all history entries for a user (used when initializing a new device).
	// For HTTP: GET /api/v1/bootstrap?user_id=...&device_id=...
	// For S3: Lists and returns all entries from entries/ prefix
	Bootstrap(ctx context.Context, userId, deviceId string) ([]*shared.EncHistoryEntry, error)

	// SubmitEntries submits new encrypted history entries and fans them out to all devices.
	// Returns dump requests and deletion requests that need to be processed.
	// For HTTP: POST /api/v1/submit?source_device_id=...
	// For S3: Writes entry to entries/, creates inbox entries for other devices
	SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error)

	// SubmitDump handles bulk transfer of entries to a requesting device (responds to DumpRequest).
	// For HTTP: POST /api/v1/submit-dump?user_id=...&requesting_device_id=...&source_device_id=...
	// For S3: Writes entries to requesting device's inbox, clears dump request
	SubmitDump(ctx context.Context, entries []*shared.EncHistoryEntry, userId, requestingDeviceId, sourceDeviceId string) error

	// QueryEntries retrieves new entries for this device (entries not yet synced).
	// For HTTP: GET /api/v1/query?device_id=...&user_id=...
	// For S3: Lists and returns entries from inbox/{device_id}/, marks as read
	QueryEntries(ctx context.Context, deviceId, userId string) ([]*shared.EncHistoryEntry, error)

	// GetDeletionRequests returns pending deletion requests for a device.
	// For HTTP: GET /api/v1/get-deletion-requests?user_id=...&device_id=...
	// For S3: Lists and returns from deletions/{device_id}/
	GetDeletionRequests(ctx context.Context, userId, deviceId string) ([]*shared.DeletionRequest, error)

	// AddDeletionRequest adds a deletion request to be propagated to all devices.
	// For HTTP: POST /api/v1/add-deletion-request
	// For S3: Creates deletion request files in deletions/{device_id}/ for each device
	AddDeletionRequest(ctx context.Context, request shared.DeletionRequest) error

	// Uninstall removes a device and its pending data.
	// For HTTP: POST /api/v1/uninstall?user_id=...&device_id=...
	// For S3: Removes device from devices.json, cleans up inbox and deletion requests
	Uninstall(ctx context.Context, userId, deviceId string) error

	// Ping checks if the backend is reachable.
	// For HTTP: GET /api/v1/ping
	// For S3: HeadBucket or similar S3 operation
	Ping(ctx context.Context) error

	// Type returns the backend type identifier ("http" or "s3").
	Type() string
}

// BackendType represents the type of sync backend
type BackendType string

const (
	BackendTypeHTTP BackendType = "http"
	BackendTypeS3   BackendType = "s3"
)
