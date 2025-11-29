package backend

import (
	"context"
	"fmt"
)

// Config holds the minimal configuration needed to create a backend.
// This avoids circular imports with the hctx package.
type Config struct {
	// BackendType is either "http" (default) or "s3"
	BackendType string

	// UserId is the hashed user secret, used as the folder name in S3
	UserId string

	// DeviceId is this device's unique identifier
	DeviceId string

	// Version is the client version for HTTP headers
	Version string

	// S3 configuration (only used when BackendType is "s3")
	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3Prefix    string
}

// NewBackendFromConfig creates the appropriate sync backend based on configuration.
// If BackendType is empty or "http", creates an HTTPBackend.
// If BackendType is "s3", creates an S3Backend.
func NewBackendFromConfig(ctx context.Context, cfg Config) (SyncBackend, error) {
	switch BackendType(cfg.BackendType) {
	case BackendTypeS3:
		s3cfg := &S3Config{
			Bucket:      cfg.S3Bucket,
			Region:      cfg.S3Region,
			Endpoint:    cfg.S3Endpoint,
			AccessKeyID: cfg.S3AccessKey,
			Prefix:      cfg.S3Prefix,
			// SecretAccessKey is loaded from environment by S3Config.Validate()
		}
		return NewS3Backend(ctx, s3cfg, cfg.UserId)

	case BackendTypeHTTP, "":
		// Default to HTTP backend
		return NewHTTPBackend(
			WithVersion(cfg.Version),
			WithHeadersCallback(func() (string, string) {
				return cfg.DeviceId, cfg.UserId
			}),
		), nil

	default:
		return nil, fmt.Errorf("unknown backend type: %q", cfg.BackendType)
	}
}
