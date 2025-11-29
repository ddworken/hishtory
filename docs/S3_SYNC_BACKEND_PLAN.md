# S3 Sync Backend Implementation Plan

## Overview

This document outlines a detailed plan for implementing an alternate syncing backend built on top of Amazon S3 (or S3-compatible storage like MinIO, Backblaze B2, Wasabi, etc.). This would allow users to self-host their history sync without running the hishtory server, using only an S3 bucket.

**Key Design Principle**: The interface is defined on the **client side**. The client will have two implementations:
1. `HTTPBackend` - wraps the existing HTTP API calls to the hishtory server
2. `S3Backend` - implements sync directly against an S3 bucket

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
- **Read tracking**: Server tracks which entries have been read by each device (`read_count` field)
- **Deletion propagation**: Deletion requests are distributed to all devices

### Client API Usage (from code review)

| Endpoint | Client Usage Location | Purpose |
|----------|----------------------|---------|
| `/api/v1/register` | `client/cmd/install.go:656` | Register new device |
| `/api/v1/bootstrap` | `client/cmd/install.go:661` | Get all entries for new device |
| `/api/v1/submit` | `client/cmd/saveHistoryEntry.go:108,187,219` | Submit new entries |
| `/api/v1/submit-dump` | `client/cmd/saveHistoryEntry.go:332` | Bulk transfer to requesting device |
| `/api/v1/query` | `client/lib/lib.go:682` | Get new entries for device |
| `/api/v1/get-deletion-requests` | `client/lib/lib.go:709` | Get pending deletions |
| `/api/v1/add-deletion-request` | `client/lib/lib.go:1152` | Add deletion request |
| `/api/v1/uninstall` | `client/cmd/install.go:137`, `client/cmd/syncing.go:75` | Unregister device |
| `/api/v1/ping` | `client/lib/lib.go:540` | Health check |

### Endpoints NOT part of sync (separate concerns)
- `/api/v1/banner` - Server messaging (not needed for S3)
- `/api/v1/download` - Update info (not needed for S3)
- `/api/v1/ai-suggest` - AI completion (separate service)
- `/api/v1/slsa-status` - Update verification (separate service)
- `/api/v1/feedback` - Analytics (optional, can be no-op for S3)

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

// SyncBackend defines the interface for syncing history entries.
// This interface is implemented by:
//   - HTTPBackend: wraps existing API calls to api.hishtory.dev
//   - S3Backend: syncs directly to an S3 bucket
type SyncBackend interface {
    // RegisterDevice registers a new device for the user.
    // For HTTP: POST /api/v1/register?user_id=...&device_id=...
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
```

#### 1.2 HTTP Backend Implementation (`client/backend/http_backend.go`)

Wrap existing `ApiGet`/`ApiPost` functions into the new interface. This is largely a refactoring of existing code from `client/lib/lib.go`:

```go
package backend

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "time"

    "github.com/ddworken/hishtory/client/hctx"
    "github.com/ddworken/hishtory/shared"
)

type HTTPBackend struct {
    serverURL string
    client    *http.Client
}

func NewHTTPBackend(serverURL string) *HTTPBackend {
    if serverURL == "" {
        serverURL = getServerHostname()
    }
    return &HTTPBackend{
        serverURL: serverURL,
        client:    &http.Client{Timeout: 30 * time.Second},
    }
}

func getServerHostname() string {
    if server := os.Getenv("HISHTORY_SERVER"); server != "" {
        return server
    }
    return "https://api.hishtory.dev"
}

func (b *HTTPBackend) Type() string {
    return "http"
}

func (b *HTTPBackend) RegisterDevice(ctx context.Context, userId, deviceId string) error {
    path := "/api/v1/register?user_id=" + userId + "&device_id=" + deviceId
    _, err := b.apiGet(ctx, path)
    return err
}

func (b *HTTPBackend) Bootstrap(ctx context.Context, userId, deviceId string) ([]*shared.EncHistoryEntry, error) {
    path := "/api/v1/bootstrap?user_id=" + userId + "&device_id=" + deviceId
    respBody, err := b.apiGet(ctx, path)
    if err != nil {
        return nil, err
    }
    var entries []*shared.EncHistoryEntry
    if err := json.Unmarshal(respBody, &entries); err != nil {
        return nil, fmt.Errorf("failed to unmarshal bootstrap response: %w", err)
    }
    return entries, nil
}

func (b *HTTPBackend) SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error) {
    jsonValue, err := json.Marshal(entries)
    if err != nil {
        return nil, err
    }
    path := "/api/v1/submit?source_device_id=" + sourceDeviceId
    respBody, err := b.apiPost(ctx, path, "application/json", jsonValue)
    if err != nil {
        return nil, err
    }
    var resp shared.SubmitResponse
    if err := json.Unmarshal(respBody, &resp); err != nil {
        return nil, fmt.Errorf("failed to unmarshal submit response: %w", err)
    }
    return &resp, nil
}

func (b *HTTPBackend) SubmitDump(ctx context.Context, entries []*shared.EncHistoryEntry, userId, requestingDeviceId, sourceDeviceId string) error {
    jsonValue, err := json.Marshal(entries)
    if err != nil {
        return err
    }
    path := "/api/v1/submit-dump?user_id=" + userId + "&requesting_device_id=" + requestingDeviceId + "&source_device_id=" + sourceDeviceId
    _, err = b.apiPost(ctx, path, "application/json", jsonValue)
    return err
}

func (b *HTTPBackend) QueryEntries(ctx context.Context, deviceId, userId string) ([]*shared.EncHistoryEntry, error) {
    path := "/api/v1/query?device_id=" + deviceId + "&user_id=" + userId
    respBody, err := b.apiGet(ctx, path)
    if err != nil {
        return nil, err
    }
    var entries []*shared.EncHistoryEntry
    if err := json.Unmarshal(respBody, &entries); err != nil {
        return nil, fmt.Errorf("failed to unmarshal query response: %w", err)
    }
    return entries, nil
}

func (b *HTTPBackend) GetDeletionRequests(ctx context.Context, userId, deviceId string) ([]*shared.DeletionRequest, error) {
    path := "/api/v1/get-deletion-requests?user_id=" + userId + "&device_id=" + deviceId
    respBody, err := b.apiGet(ctx, path)
    if err != nil {
        return nil, err
    }
    var requests []*shared.DeletionRequest
    if err := json.Unmarshal(respBody, &requests); err != nil {
        return nil, fmt.Errorf("failed to unmarshal deletion requests: %w", err)
    }
    return requests, nil
}

func (b *HTTPBackend) AddDeletionRequest(ctx context.Context, request shared.DeletionRequest) error {
    jsonValue, err := json.Marshal(request)
    if err != nil {
        return err
    }
    _, err = b.apiPost(ctx, "/api/v1/add-deletion-request", "application/json", jsonValue)
    return err
}

func (b *HTTPBackend) Uninstall(ctx context.Context, userId, deviceId string) error {
    path := "/api/v1/uninstall?user_id=" + userId + "&device_id=" + deviceId
    _, err := b.apiPost(ctx, path, "application/json", []byte{})
    return err
}

func (b *HTTPBackend) Ping(ctx context.Context) error {
    _, err := b.apiGet(ctx, "/api/v1/ping")
    return err
}

// apiGet and apiPost are internal helpers (similar to existing lib.ApiGet/ApiPost)
func (b *HTTPBackend) apiGet(ctx context.Context, path string) ([]byte, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", b.serverURL+path, nil)
    if err != nil {
        return nil, err
    }
    b.setHeaders(ctx, req)
    resp, err := b.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("HTTP %d from GET %s", resp.StatusCode, path)
    }
    return io.ReadAll(resp.Body)
}

func (b *HTTPBackend) apiPost(ctx context.Context, path, contentType string, body []byte) ([]byte, error) {
    req, err := http.NewRequestWithContext(ctx, "POST", b.serverURL+path, bytes.NewBuffer(body))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Content-Type", contentType)
    b.setHeaders(ctx, req)
    resp, err := b.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("HTTP %d from POST %s", resp.StatusCode, path)
    }
    return io.ReadAll(resp.Body)
}

func (b *HTTPBackend) setHeaders(ctx context.Context, req *http.Request) {
    // These headers are used for logging/analytics on the server side
    // They can be obtained from context if needed
    req.Header.Set("X-Hishtory-Version", "v0.TODO")
    // Device ID and User ID could be passed via context or method params
}
```

#### 1.3 Refactor Client Code to Use Backend Interface

The following files need modification to use the `SyncBackend` interface:

**`client/lib/lib.go`** - Core sync functions:
```go
// Before: Direct API calls
func RetrieveAdditionalEntriesFromRemote(ctx context.Context, queryReason string) error {
    respBody, err := ApiGet(ctx, "/api/v1/query?device_id="+config.DeviceId+"...")
    // ...
}

// After: Use backend interface
func RetrieveAdditionalEntriesFromRemote(ctx context.Context, backend SyncBackend, queryReason string) error {
    entries, err := backend.QueryEntries(ctx, config.DeviceId, data.UserId(config.UserSecret))
    // ...
}
```

**`client/cmd/saveHistoryEntry.go`** - Entry submission:
```go
// Before (line 108, 187, 219):
_, err = lib.ApiPost(ctx, "/api/v1/submit?source_device_id="+config.DeviceId, ...)

// After:
backend := hctx.GetBackend(ctx)
resp, err := backend.SubmitEntries(ctx, encEntries, config.DeviceId)
```

**`client/cmd/install.go`** - Registration and bootstrap:
```go
// Before (line 656):
_, err := lib.ApiGet(ctx, registerPath)
respBody, err := lib.ApiGet(ctx, "/api/v1/bootstrap?...")

// After:
backend := hctx.GetBackend(ctx)
err := backend.RegisterDevice(ctx, userId, deviceId)
entries, err := backend.Bootstrap(ctx, userId, deviceId)
```

**`client/hctx/hctx.go`** - Add backend to context:
```go
type backendCtxKeyType string
const BackendCtxKey backendCtxKeyType = "backend"

func GetBackend(ctx context.Context) backend.SyncBackend {
    v := ctx.Value(BackendCtxKey)
    if v != nil {
        return v.(backend.SyncBackend)
    }
    // Default to HTTP backend
    return backend.NewHTTPBackend("")
}

func MakeContext() context.Context {
    // ... existing code ...

    // Initialize backend based on config
    var syncBackend backend.SyncBackend
    if config.BackendType == "s3" && config.S3Config != nil {
        syncBackend = backend.NewS3Backend(config.S3Config)
    } else {
        syncBackend = backend.NewHTTPBackend("")
    }
    ctx = context.WithValue(ctx, BackendCtxKey, syncBackend)
    return ctx
}
```

**Files requiring changes (summary):**

| File | Changes Required |
|------|-----------------|
| `client/lib/lib.go` | Replace `ApiGet`/`ApiPost` with backend calls in: `RetrieveAdditionalEntriesFromRemote`, `ProcessDeletionRequests`, `SendDeletionRequest`, `Reupload`, `CanReachHishtoryServer` |
| `client/cmd/saveHistoryEntry.go` | Replace submit calls (lines 108, 187, 219, 332) |
| `client/cmd/install.go` | Replace register/bootstrap (lines 656, 661) |
| `client/cmd/syncing.go` | Replace uninstall call (line 75) |
| `client/hctx/hctx.go` | Add `BackendType`, `S3Config` to `ClientConfig`, add `GetBackend()` |
| `client/tui/tui.go` | Replace deletion request call (line 839) |

### Phase 2: Implement S3 Backend

#### 2.1 S3 Backend Core (`client/backend/s3_backend.go`)

```go
package backend

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "path"
    "strings"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/ddworken/hishtory/shared"
)

type S3Backend struct {
    client *s3.Client
    bucket string
    prefix string // optional path prefix within bucket (e.g., "hishtory/")
    userId string // derived from user secret, used as folder name
}

type S3Config struct {
    Bucket          string `json:"bucket"`
    Region          string `json:"region"`
    Endpoint        string `json:"endpoint,omitempty"`         // for S3-compatible services
    AccessKeyID     string `json:"access_key_id,omitempty"`    // optional if using IAM/env
    SecretAccessKey string `json:"-"`                          // from env var HISHTORY_S3_SECRET_ACCESS_KEY
    Prefix          string `json:"prefix,omitempty"`           // optional path prefix
}

func NewS3Backend(cfg *S3Config, userId string) (*S3Backend, error) {
    // Build AWS config
    var opts []func(*config.LoadOptions) error
    opts = append(opts, config.WithRegion(cfg.Region))

    // Custom credentials if provided
    if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
        opts = append(opts, config.WithCredentialsProvider(
            credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
        ))
    }

    awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
    if err != nil {
        return nil, fmt.Errorf("failed to load AWS config: %w", err)
    }

    // Build S3 client options
    var s3Opts []func(*s3.Options)
    if cfg.Endpoint != "" {
        s3Opts = append(s3Opts, func(o *s3.Options) {
            o.BaseEndpoint = aws.String(cfg.Endpoint)
            o.UsePathStyle = true // Required for MinIO and most S3-compatible services
        })
    }

    client := s3.NewFromConfig(awsCfg, s3Opts...)

    return &S3Backend{
        client: client,
        bucket: cfg.Bucket,
        prefix: strings.TrimSuffix(cfg.Prefix, "/"),
        userId: userId,
    }, nil
}

func (b *S3Backend) Type() string {
    return "s3"
}

// Helper to build S3 key paths
func (b *S3Backend) key(parts ...string) string {
    allParts := []string{}
    if b.prefix != "" {
        allParts = append(allParts, b.prefix)
    }
    allParts = append(allParts, b.userId)
    allParts = append(allParts, parts...)
    return path.Join(allParts...)
}
```

#### 2.2 S3 Backend Operations

**Device Registration:**
```go
func (b *S3Backend) RegisterDevice(ctx context.Context, userId, deviceId string) error {
    // 1. Get current devices list (with optimistic locking via ETag)
    devices, etag, err := b.getDevices(ctx)
    if err != nil && !isNotFoundError(err) {
        return fmt.Errorf("failed to get devices: %w", err)
    }

    // 2. Check if this is a new device
    isNewDevice := true
    existingDeviceCount := len(devices)
    for _, d := range devices {
        if d.DeviceId == deviceId {
            isNewDevice = false
            break
        }
    }

    if isNewDevice {
        // 3. Add new device
        devices = append(devices, DeviceInfo{
            DeviceId:         deviceId,
            UserId:           userId,
            RegistrationDate: time.Now().UTC(),
        })

        // 4. Save updated devices list (with conditional write)
        if err := b.putDevices(ctx, devices, etag); err != nil {
            return fmt.Errorf("failed to save devices: %w", err)
        }

        // 5. If there are existing devices, create a dump request
        if existingDeviceCount > 0 {
            dumpReq := &shared.DumpRequest{
                UserId:             userId,
                RequestingDeviceId: deviceId,
                RequestTime:        time.Now().UTC(),
            }
            if err := b.createDumpRequest(ctx, dumpReq); err != nil {
                return fmt.Errorf("failed to create dump request: %w", err)
            }
        }
    }

    return nil
}
```

**Submit Entries:**
```go
func (b *S3Backend) SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error) {
    if len(entries) == 0 {
        return &shared.SubmitResponse{}, nil
    }

    // 1. Get list of all devices
    devices, _, err := b.getDevices(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to get devices: %w", err)
    }
    if len(devices) == 0 {
        return nil, fmt.Errorf("no devices registered for user")
    }

    // 2. For each entry, write to main entries store and each device's inbox
    for _, entry := range entries {
        entryKey := b.key("entries", entry.Date.Format("2006-01-02"), entry.EncryptedId+".json")
        entryData, err := json.Marshal(entry)
        if err != nil {
            return nil, fmt.Errorf("failed to marshal entry: %w", err)
        }

        // Write to entries/ (master copy)
        if err := b.putObject(ctx, entryKey, entryData); err != nil {
            return nil, fmt.Errorf("failed to write entry: %w", err)
        }

        // Write to each device's inbox (except source device)
        for _, device := range devices {
            entryCopy := *entry
            entryCopy.DeviceId = device.DeviceId
            entryCopy.IsFromSameDevice = (device.DeviceId == sourceDeviceId)

            // Skip writing to inbox if from same device (optimization)
            // The server stores these for bootstrap, but inbox is for sync
            if entryCopy.IsFromSameDevice {
                continue
            }

            inboxKey := b.key("inbox", device.DeviceId, entry.Date.Format("20060102T150405")+"_"+entry.EncryptedId+".json")
            inboxData, err := json.Marshal(&entryCopy)
            if err != nil {
                return nil, fmt.Errorf("failed to marshal inbox entry: %w", err)
            }
            if err := b.putObject(ctx, inboxKey, inboxData); err != nil {
                return nil, fmt.Errorf("failed to write inbox entry: %w", err)
            }
        }
    }

    // 3. Check for pending dump requests and deletion requests for source device
    resp := &shared.SubmitResponse{}

    dumpReqs, err := b.getDumpRequests(ctx, sourceDeviceId)
    if err == nil {
        resp.DumpRequests = dumpReqs
    }

    delReqs, err := b.GetDeletionRequests(ctx, b.userId, sourceDeviceId)
    if err == nil {
        resp.DeletionRequests = delReqs
    }

    return resp, nil
}
```

**Query Entries:**
```go
func (b *S3Backend) QueryEntries(ctx context.Context, deviceId, userId string) ([]*shared.EncHistoryEntry, error) {
    // 1. List objects in inbox/{device_id}/
    inboxPrefix := b.key("inbox", deviceId) + "/"
    objects, err := b.listObjects(ctx, inboxPrefix)
    if err != nil {
        return nil, fmt.Errorf("failed to list inbox: %w", err)
    }

    // 2. Download and parse each entry (with read count tracking)
    var entries []*shared.EncHistoryEntry
    var keysToDelete []string

    for _, obj := range objects {
        data, err := b.getObject(ctx, obj.Key)
        if err != nil {
            continue // Skip entries we can't read
        }

        var entry shared.EncHistoryEntry
        if err := json.Unmarshal(data, &entry); err != nil {
            continue
        }

        // Skip entries from same device (shouldn't be in inbox, but defensive)
        if entry.IsFromSameDevice {
            keysToDelete = append(keysToDelete, obj.Key)
            continue
        }

        entry.ReadCount++
        entries = append(entries, &entry)

        // Mark for deletion after successful read (read_count equivalent)
        // In S3 model, we delete from inbox after reading
        keysToDelete = append(keysToDelete, obj.Key)
    }

    // 3. Delete processed entries from inbox
    for _, key := range keysToDelete {
        _ = b.deleteObject(ctx, key) // Best effort deletion
    }

    return entries, nil
}
```

**Bootstrap:**
```go
func (b *S3Backend) Bootstrap(ctx context.Context, userId, deviceId string) ([]*shared.EncHistoryEntry, error) {
    // 1. List all objects in entries/
    entriesPrefix := b.key("entries") + "/"
    objects, err := b.listObjects(ctx, entriesPrefix)
    if err != nil {
        return nil, fmt.Errorf("failed to list entries: %w", err)
    }

    // 2. Download and parse each entry (deduplicate by EncryptedId)
    seen := make(map[string]bool)
    var entries []*shared.EncHistoryEntry

    for _, obj := range objects {
        data, err := b.getObject(ctx, obj.Key)
        if err != nil {
            continue
        }

        var entry shared.EncHistoryEntry
        if err := json.Unmarshal(data, &entry); err != nil {
            continue
        }

        // Deduplicate (same logic as server's AllHistoryEntriesForUser)
        if seen[entry.EncryptedId] {
            continue
        }
        seen[entry.EncryptedId] = true
        entries = append(entries, &entry)
    }

    return entries, nil
}
```

**Submit Dump (respond to DumpRequest):**
```go
func (b *S3Backend) SubmitDump(ctx context.Context, entries []*shared.EncHistoryEntry, userId, requestingDeviceId, sourceDeviceId string) error {
    // Write all entries to requesting device's inbox
    for _, entry := range entries {
        entryCopy := *entry
        entryCopy.DeviceId = requestingDeviceId

        inboxKey := b.key("inbox", requestingDeviceId, entry.Date.Format("20060102T150405")+"_"+entry.EncryptedId+".json")
        data, err := json.Marshal(&entryCopy)
        if err != nil {
            return fmt.Errorf("failed to marshal entry: %w", err)
        }
        if err := b.putObject(ctx, inboxKey, data); err != nil {
            return fmt.Errorf("failed to write inbox entry: %w", err)
        }
    }

    // Clear the dump request
    return b.deleteDumpRequest(ctx, requestingDeviceId)
}
```

**Deletion Requests:**
```go
func (b *S3Backend) AddDeletionRequest(ctx context.Context, request shared.DeletionRequest) error {
    // Get all devices to fan out the deletion request
    devices, _, err := b.getDevices(ctx)
    if err != nil {
        return fmt.Errorf("failed to get devices: %w", err)
    }

    // Create deletion request for each device
    for _, device := range devices {
        reqCopy := request
        reqCopy.DestinationDeviceId = device.DeviceId
        reqCopy.ReadCount = 0

        key := b.key("deletions", device.DeviceId, fmt.Sprintf("%d_%s.json", time.Now().UnixNano(), request.Messages.Ids[0].EntryId))
        data, err := json.Marshal(&reqCopy)
        if err != nil {
            return fmt.Errorf("failed to marshal deletion request: %w", err)
        }
        if err := b.putObject(ctx, key, data); err != nil {
            return fmt.Errorf("failed to write deletion request: %w", err)
        }
    }

    // Also delete the entries from the main entries store
    for _, msg := range request.Messages.Ids {
        // Find and delete matching entries (by date or entry ID)
        entriesPrefix := b.key("entries") + "/"
        objects, _ := b.listObjects(ctx, entriesPrefix)
        for _, obj := range objects {
            if strings.Contains(obj.Key, msg.EntryId) {
                _ = b.deleteObject(ctx, obj.Key)
            }
        }
    }

    return nil
}

func (b *S3Backend) GetDeletionRequests(ctx context.Context, userId, deviceId string) ([]*shared.DeletionRequest, error) {
    prefix := b.key("deletions", deviceId) + "/"
    objects, err := b.listObjects(ctx, prefix)
    if err != nil {
        return nil, err
    }

    var requests []*shared.DeletionRequest
    for _, obj := range objects {
        data, err := b.getObject(ctx, obj.Key)
        if err != nil {
            continue
        }

        var req shared.DeletionRequest
        if err := json.Unmarshal(data, &req); err != nil {
            continue
        }

        req.ReadCount++
        requests = append(requests, &req)

        // Delete after reading (equivalent to incrementing read_count past threshold)
        _ = b.deleteObject(ctx, obj.Key)
    }

    return requests, nil
}
```

**Uninstall and Ping:**
```go
func (b *S3Backend) Uninstall(ctx context.Context, userId, deviceId string) error {
    // 1. Remove device from devices list
    devices, etag, err := b.getDevices(ctx)
    if err != nil {
        return err
    }

    newDevices := make([]DeviceInfo, 0, len(devices))
    for _, d := range devices {
        if d.DeviceId != deviceId {
            newDevices = append(newDevices, d)
        }
    }

    if err := b.putDevices(ctx, newDevices, etag); err != nil {
        return err
    }

    // 2. Clean up inbox
    inboxPrefix := b.key("inbox", deviceId) + "/"
    objects, _ := b.listObjects(ctx, inboxPrefix)
    for _, obj := range objects {
        _ = b.deleteObject(ctx, obj.Key)
    }

    // 3. Clean up deletion requests
    delPrefix := b.key("deletions", deviceId) + "/"
    objects, _ = b.listObjects(ctx, delPrefix)
    for _, obj := range objects {
        _ = b.deleteObject(ctx, obj.Key)
    }

    return nil
}

func (b *S3Backend) Ping(ctx context.Context) error {
    _, err := b.client.HeadBucket(ctx, &s3.HeadBucketInput{
        Bucket: aws.String(b.bucket),
    })
    return err
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
| `client/backend/backend.go` | `SyncBackend` interface definition |
| `client/backend/http_backend.go` | HTTP backend implementation (wraps existing API calls) |
| `client/backend/s3_backend.go` | S3 backend implementation |
| `client/backend/s3_helpers.go` | S3 helper functions (putObject, getObject, listObjects, etc.) |
| `client/backend/s3_backend_test.go` | S3 backend unit tests |
| `client/cmd/configBackend.go` | CLI commands for backend configuration |
| `docs/S3_BACKEND.md` | User documentation for S3 setup |

### Files to Modify

| File | Changes |
|------|---------|
| `client/hctx/hctx.go` | Add `BackendType`, `S3Config` to `ClientConfig`; add `GetBackend()` function |
| `client/lib/lib.go` | Replace direct `ApiGet`/`ApiPost` calls with backend interface in sync functions |
| `client/cmd/saveHistoryEntry.go` | Use `backend.SubmitEntries()` instead of `ApiPost` (lines 108, 187, 219, 332) |
| `client/cmd/install.go` | Use `backend.RegisterDevice()` and `backend.Bootstrap()` (lines 656, 661) |
| `client/cmd/syncing.go` | Use `backend.Uninstall()` (line 75) |
| `client/tui/tui.go` | Use `backend.AddDeletionRequest()` (line 839) |
| `go.mod` | Add `github.com/aws/aws-sdk-go-v2` dependencies |
| `.github/workflows/go-test.yml` | Add MinIO service for S3 integration tests |

### Behavioral Changes

The refactoring maintains exact behavioral parity for HTTP backend users:
- All existing `ApiGet`/`ApiPost` calls map 1:1 to `HTTPBackend` methods
- Error handling and retry logic remains in the calling code
- `IsOfflineError()` continues to work for detecting network issues
- No changes to encryption/decryption (handled by calling code, not backend)

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
