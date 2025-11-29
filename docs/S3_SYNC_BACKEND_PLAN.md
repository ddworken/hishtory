# S3 Sync Backend Implementation Plan

## Overview

This document outlines a detailed plan for implementing an alternate syncing backend built on top of Amazon S3 (or S3-compatible storage like MinIO, Backblaze B2, Wasabi, etc.). This would allow users to self-host their history sync without running the hishtory server, using only an S3 bucket.

## Goals

1. **Self-hosted simplicity**: Users can sync history using only an S3 bucket - no server required
2. **End-to-end encryption**: Maintain existing encryption model (data encrypted before upload)
3. **Multi-device support**: Support the existing device fan-out sync model
4. **Backward compatibility**: Existing server-based sync continues to work unchanged
5. **Cost-effective**: Minimize S3 API calls and storage costs

## Current Architecture Summary

The existing sync architecture (see `client/lib/lib.go` and `backend/server/internal/server/api_handlers.go`):

- **Server-based fan-out model**: When Device A submits an entry, the server creates copies for all devices (A, B, C, etc.)
- **Encrypted entries**: `shared.EncHistoryEntry` contains AES-256-GCM encrypted data
- **Device registration**: Devices register with server, which tracks all devices per user
- **Read tracking**: Server tracks which entries have been read by each device
- **Deletion propagation**: Deletion requests are distributed to all devices

Key API endpoints:
- `/api/v1/register` - Register new device
- `/api/v1/submit` - Submit new history entries (fan-out to all devices)
- `/api/v1/query` - Retrieve entries for a specific device
- `/api/v1/bootstrap` - Get all entries for new device initialization
- `/api/v1/add-deletion-request` - Request entry deletion across devices
- `/api/v1/get-deletion-requests` - Get pending deletion requests

## S3 Data Model Design

### Bucket Structure

```
s3://bucket-name/
  {user_id}/
    devices/
      {device_id}.json              # Device registration metadata
    entries/
      {timestamp}_{entry_id}.json   # Individual encrypted entries
    inbox/
      {device_id}/
        {timestamp}_{entry_id}.json # Entries pending sync to this device
    deletions/
      {device_id}/
        {timestamp}_{request_id}.json # Pending deletion requests
    metadata/
      sync_state.json               # Global sync state and version info
```

### Alternative: Optimized Structure for Reduced API Calls

```
s3://bucket-name/
  {user_id}/
    devices.json                    # All device registrations (single file)
    entries/
      {YYYY-MM-DD}/
        {HH}.json                   # Hourly batched entries (reduces object count)
    sync/
      {device_id}_cursor.json       # Last sync timestamp per device
    deletions.json                  # All pending deletions (single file)
```

### Data Formats

#### Device Registration (`devices/{device_id}.json`)
```json
{
  "device_id": "uuid",
  "user_id": "hash",
  "registration_date": "2024-01-15T10:30:00Z",
  "last_seen": "2024-01-15T15:45:00Z"
}
```

#### Encrypted Entry (`entries/{timestamp}_{entry_id}.json`)
```json
{
  "enc_data": "base64...",
  "nonce": "base64...",
  "user_id": "hash",
  "device_id": "source_device_id",
  "time": "2024-01-15T10:30:00Z",
  "encrypted_id": "uuid",
  "source_device_id": "uuid"
}
```

#### Sync Cursor (`sync/{device_id}_cursor.json`)
```json
{
  "device_id": "uuid",
  "last_sync_timestamp": "2024-01-15T10:30:00Z",
  "last_entry_id": "uuid"
}
```

#### Deletion Request (`deletions/{device_id}/{request_id}.json`)
```json
{
  "user_id": "hash",
  "destination_device_id": "uuid",
  "send_time": "2024-01-15T10:30:00Z",
  "messages": {
    "message_ids": [
      {"device_id": "uuid", "date": "...", "entry_id": "uuid"}
    ]
  }
}
```

## Implementation Plan

### Phase 1: Create Backend Abstraction Layer

**Files to create/modify:**

#### 1.1 Define Backend Interface (`client/backend/backend.go`)

```go
package backend

import (
    "context"
    "github.com/ddworken/hishtory/shared"
)

// SyncBackend defines the interface for syncing history entries
type SyncBackend interface {
    // Register a new device for the user
    RegisterDevice(ctx context.Context, userId, deviceId string) error

    // Submit encrypted history entries (distributes to all devices)
    SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error)

    // Query entries pending for a specific device
    QueryEntries(ctx context.Context, deviceId, userId string) ([]*shared.EncHistoryEntry, error)

    // Bootstrap a new device with all existing entries
    Bootstrap(ctx context.Context, userId, deviceId string) ([]*shared.EncHistoryEntry, error)

    // Add a deletion request
    AddDeletionRequest(ctx context.Context, request shared.DeletionRequest) error

    // Get pending deletion requests for a device
    GetDeletionRequests(ctx context.Context, userId, deviceId string) ([]*shared.DeletionRequest, error)

    // Uninstall/unregister a device
    Uninstall(ctx context.Context, userId, deviceId string) error

    // Health check
    Ping(ctx context.Context) error

    // Get type identifier
    Type() string
}
```

#### 1.2 HTTP Backend Implementation (`client/backend/http_backend.go`)

Wrap existing `ApiGet`/`ApiPost` functions into the new interface:

```go
package backend

type HTTPBackend struct {
    serverURL string
    client    *http.Client
}

func NewHTTPBackend(serverURL string) *HTTPBackend {
    return &HTTPBackend{
        serverURL: serverURL,
        client:    &http.Client{Timeout: 30 * time.Second},
    }
}

func (b *HTTPBackend) Type() string {
    return "http"
}

// Implement all interface methods wrapping existing API calls...
```

#### 1.3 Refactor `client/lib/lib.go`

- Extract API calls into the HTTP backend implementation
- Modify sync functions to use the backend interface
- Add backend selection based on configuration

### Phase 2: Implement S3 Backend

#### 2.1 S3 Backend Core (`client/backend/s3_backend.go`)

```go
package backend

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/ddworken/hishtory/shared"
)

type S3Backend struct {
    client     *s3.Client
    bucket     string
    prefix     string  // optional path prefix within bucket
    userId     string
}

type S3Config struct {
    Bucket          string
    Region          string
    Endpoint        string  // for S3-compatible services
    AccessKeyID     string
    SecretAccessKey string
    Prefix          string
}

func NewS3Backend(ctx context.Context, config S3Config, userId string) (*S3Backend, error) {
    // Initialize AWS SDK v2 client with config
    // Support custom endpoints for MinIO, Backblaze, etc.
}

func (b *S3Backend) Type() string {
    return "s3"
}
```

#### 2.2 S3 Backend Operations

**Device Registration:**
```go
func (b *S3Backend) RegisterDevice(ctx context.Context, userId, deviceId string) error {
    // 1. Check if devices.json exists, download it
    // 2. Add new device to list
    // 3. Upload updated devices.json
    // 4. If this is not the first device, mark existing entries for sync
}
```

**Submit Entries:**
```go
func (b *S3Backend) SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error) {
    // 1. Get list of all devices
    // 2. For each entry:
    //    a. Upload to entries/{date}/{entry_id}.json
    //    b. For each device (except source), create inbox entry
    // 3. Check for pending dump requests and deletion requests
    // 4. Return SubmitResponse with dump/deletion requests
}
```

**Query Entries:**
```go
func (b *S3Backend) QueryEntries(ctx context.Context, deviceId, userId string) ([]*shared.EncHistoryEntry, error) {
    // 1. List objects in inbox/{device_id}/
    // 2. Download and parse each entry
    // 3. Delete processed entries from inbox
    // 4. Return entries
}
```

**Bootstrap:**
```go
func (b *S3Backend) Bootstrap(ctx context.Context, userId, deviceId string) ([]*shared.EncHistoryEntry, error) {
    // 1. List all objects in entries/
    // 2. Download and parse each entry
    // 3. Return all entries
}
```

### Phase 3: Configuration System

#### 3.1 Extend ClientConfig (`client/hctx/hctx.go`)

```go
type ClientConfig struct {
    // ... existing fields ...

    // Backend configuration
    BackendType string `json:"backend_type"` // "http" (default), "s3", "offline"

    // S3 backend configuration
    S3Config *S3BackendConfig `json:"s3_config,omitempty"`
}

type S3BackendConfig struct {
    Bucket          string `json:"bucket"`
    Region          string `json:"region"`
    Endpoint        string `json:"endpoint,omitempty"`        // For S3-compatible services
    AccessKeyID     string `json:"access_key_id,omitempty"`   // Optional, can use env/IAM
    SecretAccessKey string `json:"-" yaml:"-"`                // Never persisted, use env var
    Prefix          string `json:"prefix,omitempty"`
}
```

#### 3.2 Environment Variables

```
HISHTORY_BACKEND_TYPE=s3
HISHTORY_S3_BUCKET=my-hishtory-bucket
HISHTORY_S3_REGION=us-east-1
HISHTORY_S3_ENDPOINT=https://s3.amazonaws.com  # or MinIO endpoint
HISHTORY_S3_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
HISHTORY_S3_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
HISHTORY_S3_PREFIX=hishtory/  # optional
```

#### 3.3 CLI Commands for S3 Setup

```bash
# Configure S3 backend
hishtory config-set backend s3
hishtory config-set s3-bucket my-bucket
hishtory config-set s3-region us-east-1

# Or interactive setup
hishtory init --backend=s3

# Migrate from HTTP to S3
hishtory migrate-backend s3 --bucket=my-bucket --region=us-east-1
```

### Phase 4: Sync Logic Adaptations

#### 4.1 Handle S3 Eventual Consistency

S3 provides strong read-after-write consistency, but we still need to handle:

```go
// Use sync cursors to track what's been processed
type SyncCursor struct {
    DeviceId           string    `json:"device_id"`
    LastSyncTimestamp  time.Time `json:"last_sync_timestamp"`
    ProcessedEntryIDs  []string  `json:"processed_entry_ids"` // Recent IDs to avoid duplicates
}
```

#### 4.2 Optimistic Locking for Concurrent Access

```go
// Use S3 conditional writes (If-None-Match for creates, ETags for updates)
func (b *S3Backend) updateDevices(ctx context.Context, updateFn func([]Device) []Device) error {
    for retries := 0; retries < 5; retries++ {
        // Get current state with ETag
        devices, etag, err := b.getDevicesWithETag(ctx)
        if err != nil {
            return err
        }

        // Apply update
        newDevices := updateFn(devices)

        // Conditional put with ETag
        err = b.putDevicesWithETag(ctx, newDevices, etag)
        if err == ErrPreconditionFailed {
            time.Sleep(time.Duration(retries*100) * time.Millisecond)
            continue
        }
        return err
    }
    return ErrTooManyRetries
}
```

#### 4.3 Batch Operations for Efficiency

```go
// Batch small entries to reduce S3 API calls
const batchWindow = 5 * time.Minute
const maxBatchSize = 100

func (b *S3Backend) submitEntriesBatched(ctx context.Context, entries []*shared.EncHistoryEntry) error {
    // Group entries by hour
    // Append to existing batch file or create new one
    // This significantly reduces object count and API costs
}
```

### Phase 5: Testing Strategy

#### 5.1 Unit Tests (`client/backend/s3_backend_test.go`)

- Mock S3 client for unit tests
- Test all CRUD operations
- Test concurrent access scenarios
- Test error handling (network failures, permission errors)

#### 5.2 Integration Tests

```go
func TestS3BackendIntegration(t *testing.T) {
    // Use MinIO in Docker for integration tests
    // Test full sync workflow: register -> submit -> query -> delete
}
```

#### 5.3 Add to CI/CD

```yaml
# .github/workflows/test.yml addition
s3-integration-tests:
  runs-on: ubuntu-latest
  services:
    minio:
      image: minio/minio
      ports:
        - 9000:9000
      env:
        MINIO_ROOT_USER: minioadmin
        MINIO_ROOT_PASSWORD: minioadmin
```

### Phase 6: Migration Path

#### 6.1 Export from HTTP Backend

```go
func ExportHistory(ctx context.Context, httpBackend *HTTPBackend) ([]*shared.EncHistoryEntry, error) {
    // Use bootstrap endpoint to get all entries
    return httpBackend.Bootstrap(ctx, userId, deviceId)
}
```

#### 6.2 Import to S3 Backend

```go
func ImportHistory(ctx context.Context, s3Backend *S3Backend, entries []*shared.EncHistoryEntry) error {
    // Upload all entries to S3
    // Set up device registrations
    // Initialize sync cursors
}
```

#### 6.3 Migration Command

```bash
# One-liner migration
hishtory migrate-backend s3 \
    --bucket=my-bucket \
    --region=us-east-1 \
    --preserve-http-backup
```

## File Changes Summary

### New Files to Create

| File | Description |
|------|-------------|
| `client/backend/backend.go` | Backend interface definition |
| `client/backend/http_backend.go` | HTTP backend implementation (refactored from lib.go) |
| `client/backend/s3_backend.go` | S3 backend implementation |
| `client/backend/s3_backend_test.go` | S3 backend unit tests |
| `client/backend/s3_config.go` | S3 configuration structures |
| `client/cmd/migrate.go` | Migration command implementation |
| `docs/S3_BACKEND.md` | User documentation for S3 setup |

### Files to Modify

| File | Changes |
|------|---------|
| `client/hctx/hctx.go` | Add S3Config to ClientConfig |
| `client/lib/lib.go` | Refactor to use backend interface |
| `client/cmd/install.go` | Add S3 backend initialization |
| `client/cmd/configSet.go` | Add S3 configuration commands |
| `go.mod` | Add AWS SDK v2 dependency |
| `.github/workflows/go-test.yml` | Add MinIO integration tests |

## Security Considerations

1. **Credentials Management**
   - Never store S3 secret keys in config file
   - Support IAM roles for EC2/ECS/Lambda
   - Support environment variables
   - Support AWS credentials file (~/.aws/credentials)

2. **Bucket Permissions**
   - Provide IAM policy template with minimum required permissions
   - Support bucket policies for additional security
   - Document cross-account access patterns

3. **Data Encryption**
   - Client-side encryption already exists (AES-256-GCM)
   - Optionally enable S3 server-side encryption (SSE-S3 or SSE-KMS)
   - Document encryption options

4. **Access Logging**
   - Document how to enable S3 access logging
   - Provide CloudWatch alerting templates

## Cost Estimation

For typical usage (1000 commands/day, 5 devices):

| Operation | Count/Day | Cost/Month (us-east-1) |
|-----------|-----------|------------------------|
| PUT requests | ~1000 | ~$0.05 |
| GET requests | ~5000 | ~$0.02 |
| LIST requests | ~500 | ~$0.025 |
| Storage (1MB) | - | ~$0.023 |
| **Total** | | **~$0.12/month** |

With batching optimization, costs could be reduced by 50-80%.

## Dependencies to Add

```go
// go.mod additions
require (
    github.com/aws/aws-sdk-go-v2 v1.24.0
    github.com/aws/aws-sdk-go-v2/config v1.26.0
    github.com/aws/aws-sdk-go-v2/credentials v1.16.0
    github.com/aws/aws-sdk-go-v2/service/s3 v1.47.0
)
```

## Rollout Plan

1. **Alpha**: Implement core S3 backend, test with MinIO
2. **Beta**: Release with `--experimental-s3` flag, gather feedback
3. **GA**: Full documentation, migration tools, remove experimental flag

## Open Questions / Design Decisions

1. **Inbox cleanup policy**: How long to retain entries in device inbox before cleanup?
   - Recommendation: 30 days, with configurable retention

2. **Entry batching granularity**: Hourly vs daily batches?
   - Recommendation: Hourly for balance of cost and sync latency

3. **Support for S3-compatible services**: Which to officially support?
   - Recommendation: MinIO, Backblaze B2, Wasabi, DigitalOcean Spaces, Cloudflare R2

4. **Versioning**: Should we use S3 versioning for entries?
   - Recommendation: Optional, document as best practice for disaster recovery

5. **Cross-region replication**: Support for multi-region buckets?
   - Recommendation: Document but don't build specific support

## Appendix: IAM Policy Template

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "HishtoryS3Access",
            "Effect": "Allow",
            "Action": [
                "s3:GetObject",
                "s3:PutObject",
                "s3:DeleteObject",
                "s3:ListBucket"
            ],
            "Resource": [
                "arn:aws:s3:::YOUR-BUCKET-NAME",
                "arn:aws:s3:::YOUR-BUCKET-NAME/*"
            ]
        }
    ]
}
```

## Appendix: Terraform Example

```hcl
resource "aws_s3_bucket" "hishtory" {
  bucket = "my-hishtory-sync"
}

resource "aws_s3_bucket_versioning" "hishtory" {
  bucket = aws_s3_bucket.hishtory.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "hishtory" {
  bucket = aws_s3_bucket.hishtory.id

  rule {
    id     = "cleanup-old-inbox"
    status = "Enabled"

    filter {
      prefix = "inbox/"
    }

    expiration {
      days = 30
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "hishtory" {
  bucket = aws_s3_bucket.hishtory.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}
```
