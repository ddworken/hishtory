package backend

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
)

// Config holds the configuration needed to create a backend.
type Config struct {
	// BackendType is either "http" (default) or "s3"
	BackendType string

	// Version is the client version for HTTP headers (from lib.Version, can't be grabbed at runtime due to circular import)
	Version string

	// S3 configuration (only used when BackendType is "s3")
	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3Prefix    string

	// HTTPClient is the HTTP client to use for HTTP backends (required for offline builds)
	HTTPClient *http.Client
}

// NewBackendFromConfig creates the appropriate sync backend based on configuration.
// If BackendType is empty or "http", creates an HTTPBackend.
// If BackendType is "s3", creates an S3Backend.
func NewBackendFromConfig(ctx context.Context, cfg Config) (SyncBackend, error) {
	// Get userId and deviceId from context
	conf := hctx.GetConf(ctx)
	userId := data.UserId(conf.UserSecret)
	deviceId := conf.DeviceId

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
		return NewS3Backend(ctx, s3cfg, userId)

	case BackendTypeHTTP, "":
		// Default to HTTP backend
		opts := []HTTPBackendOption{
			WithVersion(cfg.Version),
			WithAuth(deviceId, userId),
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, WithHTTPClient(cfg.HTTPClient))
		}
		return NewHTTPBackend(opts...), nil

	default:
		return nil, fmt.Errorf("unknown backend type: %q", cfg.BackendType)
	}
}
