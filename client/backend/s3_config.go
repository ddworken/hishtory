package backend

import (
	"fmt"
	"os"
)

// S3Config holds configuration for the S3 backend.
type S3Config struct {
	// Bucket is the S3 bucket name (required)
	Bucket string `json:"bucket"`

	// Region is the AWS region (required)
	Region string `json:"region"`

	// Endpoint is a custom S3-compatible endpoint (optional, for MinIO, Backblaze, etc.)
	Endpoint string `json:"endpoint,omitempty"`

	// AccessKeyID is the AWS access key ID (optional if using IAM roles or env vars)
	AccessKeyID string `json:"access_key_id,omitempty"`

	// SecretAccessKey is loaded from environment variable HISHTORY_S3_SECRET_ACCESS_KEY
	// Never stored in config file for security
	SecretAccessKey string `json:"-"`

	// Prefix is an optional path prefix within the bucket (e.g., "hishtory/")
	Prefix string `json:"prefix,omitempty"`
}

// Validate checks that required fields are set and loads the secret from environment.
func (c *S3Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("S3 bucket is required")
	}
	if c.Region == "" {
		return fmt.Errorf("S3 region is required")
	}

	// Load secret from environment if not already set
	if c.SecretAccessKey == "" {
		c.SecretAccessKey = os.Getenv("HISHTORY_S3_SECRET_ACCESS_KEY")
	}

	// If access key is provided, secret must also be provided
	if c.AccessKeyID != "" && c.SecretAccessKey == "" {
		return fmt.Errorf("S3 access key ID provided but secret access key is missing (set HISHTORY_S3_SECRET_ACCESS_KEY)")
	}

	return nil
}

// DeviceInfo represents a registered device in S3 storage.
type DeviceInfo struct {
	DeviceId         string `json:"device_id"`
	UserId           string `json:"user_id"`
	RegistrationDate string `json:"registration_date"`
}

// DeviceList is the structure stored in devices.json.
type DeviceList struct {
	Devices []DeviceInfo `json:"devices"`
}
