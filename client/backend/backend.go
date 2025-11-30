// Package backend provides the interface and implementations for syncing history entries
// across devices. It supports multiple backend types:
//   - HTTPBackend: syncs via the hishtory server API (default)
//   - S3Backend: syncs directly to an S3 bucket (self-hosted option)
package backend

import (
	"github.com/ddworken/hishtory/shared"
)

// SyncBackend is an alias to shared.SyncBackend.
// The interface is defined in shared to avoid circular imports between hctx and backend.
type SyncBackend = shared.SyncBackend

// BackendType represents the type of sync backend
type BackendType string

const (
	BackendTypeHTTP BackendType = "http"
	BackendTypeS3   BackendType = "s3"
)
