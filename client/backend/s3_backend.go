package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// readCountLimit is the number of times an entry can be read before it is deleted.
// This matches the HTTP backend behavior (see backend/server/internal/server/api_handlers.go).
const readCountLimit = 5

// s3API defines the S3 operations used by S3Backend.
// This interface allows for dependency injection of mock clients in tests.
type s3API interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// S3Backend implements SyncBackend by storing data directly in an S3 bucket.
// This allows users to self-host their history sync without running a server.
type S3Backend struct {
	client s3API
	bucket string
	prefix string // optional path prefix within bucket
	userId string // derived from user secret, used as folder name
}

// NewS3Backend creates a new S3 backend with the given configuration.
func NewS3Backend(ctx context.Context, cfg *S3Config, userId string) (*S3Backend, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid S3 config: %w", err)
	}

	// Build AWS config options
	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(cfg.Region))

	// Use static credentials if provided
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Build S3 client options for custom endpoints
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

// Type returns "s3" to identify this backend type.
func (b *S3Backend) Type() string {
	return string(BackendTypeS3)
}

// key builds an S3 key path from parts, including the prefix and userId.
func (b *S3Backend) key(parts ...string) string {
	allParts := []string{}
	if b.prefix != "" {
		allParts = append(allParts, b.prefix)
	}
	allParts = append(allParts, b.userId)
	allParts = append(allParts, parts...)
	return path.Join(allParts...)
}

// RegisterDevice registers a new device for the user.
//
// Known limitation: This function uses a read-modify-write pattern on devices.json
// which is not atomic. If two devices register simultaneously, one registration could
// be lost. In practice, this is rare since device registration typically happens once
// during initial setup. If a device fails to register, re-running the setup will
// succeed. A proper fix would require S3 conditional writes (ETags) or an external
// locking mechanism (e.g., DynamoDB), which adds significant complexity.
func (b *S3Backend) RegisterDevice(ctx context.Context, userId, deviceId string) error {
	// Get current devices list
	devices, err := b.getDevices(ctx)
	if err != nil && !isNotFoundError(err) {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	// Check if device already exists
	existingDeviceCount := len(devices.Devices)
	for _, d := range devices.Devices {
		if d.DeviceId == deviceId {
			// Device already registered
			return nil
		}
	}

	// Add new device
	devices.Devices = append(devices.Devices, DeviceInfo{
		DeviceId:         deviceId,
		UserId:           userId,
		RegistrationDate: time.Now().UTC().Format(time.RFC3339),
	})

	// Save updated devices list
	if err := b.putDevices(ctx, devices); err != nil {
		return fmt.Errorf("failed to save devices: %w", err)
	}

	// If there are existing devices, create a dump request so they send history to the new device
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

	return nil
}

// Bootstrap returns all history entries for a user.
func (b *S3Backend) Bootstrap(ctx context.Context, _, _ string) ([]*shared.EncHistoryEntry, error) {
	entriesPrefix := b.key("entries") + "/"
	objects, err := b.listObjects(ctx, entriesPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	// Download and parse each entry, deduplicating by EncryptedId
	seen := make(map[string]bool)
	var entries []*shared.EncHistoryEntry

	for _, obj := range objects {
		data, err := b.getObject(ctx, *obj.Key)
		if err != nil {
			hctx.GetLogger().Warnf("S3Backend.Bootstrap: failed to read entry %s: %v", *obj.Key, err)
			continue
		}

		var entry shared.EncHistoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			hctx.GetLogger().Warnf("S3Backend.Bootstrap: failed to unmarshal entry %s: %v", *obj.Key, err)
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

// SubmitEntries submits new encrypted history entries.
func (b *S3Backend) SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error) {
	if len(entries) == 0 {
		return &shared.SubmitResponse{}, nil
	}

	// Get list of all devices
	deviceList, err := b.getDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}
	if len(deviceList.Devices) == 0 {
		return nil, fmt.Errorf("no devices registered for user")
	}

	// For each entry, write to main entries store and each device's inbox
	for _, entry := range entries {
		// Write to entries/ (master copy)
		entryKey := b.key("entries", entry.Date.Format("2006-01-02"), entry.EncryptedId+".json")
		entryData, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal entry: %w", err)
		}

		if err := b.putObject(ctx, entryKey, entryData); err != nil {
			return nil, fmt.Errorf("failed to write entry: %w", err)
		}

		// Write to each device's inbox (except source device)
		for _, device := range deviceList.Devices {
			if device.DeviceId == sourceDeviceId {
				continue // Don't send to the device that created the entry
			}

			entryCopy := *entry
			entryCopy.DeviceId = device.DeviceId
			entryCopy.IsFromSameDevice = false

			inboxKey := b.key("inbox", device.DeviceId, entry.Date.Format("20060102T150405Z")+"_"+entry.EncryptedId+".json")
			inboxData, err := json.Marshal(&entryCopy)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal inbox entry: %w", err)
			}
			if err := b.putObject(ctx, inboxKey, inboxData); err != nil {
				return nil, fmt.Errorf("failed to write inbox entry: %w", err)
			}
		}
	}

	// Check for pending dump requests and deletion requests for source device
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

// SubmitDump handles bulk transfer of entries to a requesting device.
func (b *S3Backend) SubmitDump(ctx context.Context, entries []*shared.EncHistoryEntry, _, requestingDeviceId, sourceDeviceId string) error {
	// Write all entries to requesting device's inbox
	for _, entry := range entries {
		entryCopy := *entry
		entryCopy.DeviceId = requestingDeviceId

		inboxKey := b.key("inbox", requestingDeviceId, entry.Date.Format("20060102T150405Z")+"_"+entry.EncryptedId+".json")
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

// QueryEntries retrieves new entries for a device.
// Entries are kept until they have been read readCountLimit times, matching
// the HTTP backend behavior. This prevents data loss if the client crashes
// after receiving entries but before persisting them locally.
func (b *S3Backend) QueryEntries(ctx context.Context, deviceId, _, _ string) ([]*shared.EncHistoryEntry, error) {
	inboxPrefix := b.key("inbox", deviceId) + "/"
	objects, err := b.listObjects(ctx, inboxPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list inbox: %w", err)
	}

	var entries []*shared.EncHistoryEntry

	for _, obj := range objects {
		data, err := b.getObject(ctx, *obj.Key)
		if err != nil {
			hctx.GetLogger().Warnf("S3Backend.QueryEntries: failed to read entry %s: %v", *obj.Key, err)
			continue
		}

		var entry shared.EncHistoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			hctx.GetLogger().Warnf("S3Backend.QueryEntries: failed to unmarshal entry %s: %v", *obj.Key, err)
			continue
		}

		// Skip entries that have already been read enough times
		if entry.ReadCount >= readCountLimit {
			// Clean up: delete entries that have exceeded the read count
			_ = b.deleteObject(ctx, *obj.Key)
			continue
		}

		// Increment read count and include in results
		entry.ReadCount++
		entries = append(entries, &entry)

		// Update the entry in S3 with incremented read count, or delete if limit reached
		if entry.ReadCount >= readCountLimit {
			_ = b.deleteObject(ctx, *obj.Key)
		} else {
			// Write back with updated read count
			updatedData, err := json.Marshal(&entry)
			if err == nil {
				_ = b.putObject(ctx, *obj.Key, updatedData)
			}
		}
	}

	return entries, nil
}

// GetDeletionRequests returns pending deletion requests for a device.
// Deletion requests are kept until they have been read readCountLimit times,
// matching the HTTP backend behavior. This prevents data loss if the client
// crashes after receiving requests but before processing them locally.
func (b *S3Backend) GetDeletionRequests(ctx context.Context, _, deviceId string) ([]*shared.DeletionRequest, error) {
	prefix := b.key("deletions", deviceId) + "/"
	objects, err := b.listObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}

	var requests []*shared.DeletionRequest
	for _, obj := range objects {
		data, err := b.getObject(ctx, *obj.Key)
		if err != nil {
			hctx.GetLogger().Warnf("S3Backend.GetDeletionRequests: failed to read request %s: %v", *obj.Key, err)
			continue
		}

		var req shared.DeletionRequest
		if err := json.Unmarshal(data, &req); err != nil {
			hctx.GetLogger().Warnf("S3Backend.GetDeletionRequests: failed to unmarshal request %s: %v", *obj.Key, err)
			continue
		}

		// Skip requests that have already been read enough times
		if req.ReadCount >= readCountLimit {
			// Clean up: delete requests that have exceeded the read count
			_ = b.deleteObject(ctx, *obj.Key)
			continue
		}

		// Increment read count and include in results
		req.ReadCount++
		requests = append(requests, &req)

		// Update the request in S3 with incremented read count, or delete if limit reached
		if req.ReadCount >= readCountLimit {
			_ = b.deleteObject(ctx, *obj.Key)
		} else {
			// Write back with updated read count
			updatedData, err := json.Marshal(&req)
			if err == nil {
				_ = b.putObject(ctx, *obj.Key, updatedData)
			}
		}
	}

	return requests, nil
}

// AddDeletionRequest adds a deletion request to be propagated to all devices.
func (b *S3Backend) AddDeletionRequest(ctx context.Context, request shared.DeletionRequest) error {
	// Get all devices to fan out the deletion request
	deviceList, err := b.getDevices(ctx)
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	// Create deletion request for each device
	for _, device := range deviceList.Devices {
		reqCopy := request
		reqCopy.DestinationDeviceId = device.DeviceId
		reqCopy.ReadCount = 0

		entryId := ""
		if len(request.Messages.Ids) > 0 {
			entryId = request.Messages.Ids[0].EntryId
		}
		key := b.key("deletions", device.DeviceId, fmt.Sprintf("%d_%s.json", time.Now().UnixNano(), entryId))
		data, err := json.Marshal(&reqCopy)
		if err != nil {
			return fmt.Errorf("failed to marshal deletion request: %w", err)
		}
		if err := b.putObject(ctx, key, data); err != nil {
			return fmt.Errorf("failed to write deletion request: %w", err)
		}
	}

	// Also delete the entries from the main entries store
	// Build set of entry IDs to delete
	idsToDelete := make(map[string]bool)
	for _, msg := range request.Messages.Ids {
		if msg.EntryId != "" {
			idsToDelete[msg.EntryId] = true
		}
	}

	if len(idsToDelete) > 0 {
		// List all entries once
		entriesPrefix := b.key("entries") + "/"
		objects, err := b.listObjects(ctx, entriesPrefix)
		if err != nil {
			return fmt.Errorf("failed to list entries for deletion: %w", err)
		}

		// Single pass: find all matching keys
		// Key format is: [prefix]/[userId]/entries/[date]/[entryId].json
		// We match by checking if the key ends with [entryId].json
		var keysToDelete []string
		for _, obj := range objects {
			for id := range idsToDelete {
				if strings.HasSuffix(*obj.Key, "/"+id+".json") {
					keysToDelete = append(keysToDelete, *obj.Key)
					break
				}
			}
		}

		// Batch delete
		if len(keysToDelete) > 0 {
			if err := b.deleteObjects(ctx, keysToDelete); err != nil {
				return fmt.Errorf("failed to delete entries: %w", err)
			}
		}
	}

	return nil
}

// Uninstall removes a device and its pending data.
func (b *S3Backend) Uninstall(ctx context.Context, _, deviceId string) error {
	// Remove device from devices list
	deviceList, err := b.getDevices(ctx)
	if err != nil {
		return err
	}

	newDevices := make([]DeviceInfo, 0, len(deviceList.Devices))
	for _, d := range deviceList.Devices {
		if d.DeviceId != deviceId {
			newDevices = append(newDevices, d)
		}
	}
	deviceList.Devices = newDevices

	if err := b.putDevices(ctx, deviceList); err != nil {
		return err
	}

	// Clean up inbox
	inboxPrefix := b.key("inbox", deviceId) + "/"
	objects, _ := b.listObjects(ctx, inboxPrefix)
	for _, obj := range objects {
		_ = b.deleteObject(ctx, *obj.Key)
	}

	// Clean up deletion requests
	delPrefix := b.key("deletions", deviceId) + "/"
	objects, _ = b.listObjects(ctx, delPrefix)
	for _, obj := range objects {
		_ = b.deleteObject(ctx, *obj.Key)
	}

	// Clean up dump requests
	dumpPrefix := b.key("dump_requests") + "/"
	objects, _ = b.listObjects(ctx, dumpPrefix)
	for _, obj := range objects {
		if strings.Contains(*obj.Key, deviceId) {
			_ = b.deleteObject(ctx, *obj.Key)
		}
	}

	return nil
}

// Ping checks if the S3 bucket is accessible.
func (b *S3Backend) Ping(ctx context.Context) error {
	_, err := b.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(b.bucket),
	})
	return err
}

// Helper methods for S3 operations

func (b *S3Backend) getDevices(ctx context.Context) (*DeviceList, error) {
	key := b.key("devices.json")
	data, err := b.getObject(ctx, key)
	if err != nil {
		if isNotFoundError(err) {
			return &DeviceList{}, nil
		}
		return nil, err
	}

	var devices DeviceList
	if err := json.Unmarshal(data, &devices); err != nil {
		return nil, fmt.Errorf("failed to unmarshal devices: %w", err)
	}
	return &devices, nil
}

func (b *S3Backend) putDevices(ctx context.Context, devices *DeviceList) error {
	data, err := json.Marshal(devices)
	if err != nil {
		return fmt.Errorf("failed to marshal devices: %w", err)
	}
	return b.putObject(ctx, b.key("devices.json"), data)
}

func (b *S3Backend) createDumpRequest(ctx context.Context, req *shared.DumpRequest) error {
	key := b.key("dump_requests", req.RequestingDeviceId+".json")
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return b.putObject(ctx, key, data)
}

func (b *S3Backend) getDumpRequests(ctx context.Context, sourceDeviceId string) ([]*shared.DumpRequest, error) {
	prefix := b.key("dump_requests") + "/"
	objects, err := b.listObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}

	var requests []*shared.DumpRequest
	for _, obj := range objects {
		// Skip dump requests from the source device itself
		if strings.Contains(*obj.Key, sourceDeviceId) {
			continue
		}

		data, err := b.getObject(ctx, *obj.Key)
		if err != nil {
			continue
		}

		var req shared.DumpRequest
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		requests = append(requests, &req)
	}

	return requests, nil
}

func (b *S3Backend) deleteDumpRequest(ctx context.Context, deviceId string) error {
	key := b.key("dump_requests", deviceId+".json")
	return b.deleteObject(ctx, key)
}

func (b *S3Backend) getObject(ctx context.Context, key string) ([]byte, error) {
	result, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}

func (b *S3Backend) putObject(ctx context.Context, key string, data []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	return err
}

func (b *S3Backend) deleteObject(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	return err
}

// deleteObjects deletes multiple objects in batches of up to 1000 (S3 limit).
func (b *S3Backend) deleteObjects(ctx context.Context, keys []string) error {
	const maxBatchSize = 1000

	for i := 0; i < len(keys); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(keys) {
			end = len(keys)
		}

		batch := keys[i:end]
		objects := make([]types.ObjectIdentifier, len(batch))
		for j, key := range batch {
			objects[j] = types.ObjectIdentifier{Key: aws.String(key)}
		}

		_, err := b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(b.bucket),
			Delete: &types.Delete{Objects: objects},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *S3Backend) listObjects(ctx context.Context, prefix string) ([]types.Object, error) {
	var objects []types.Object
	var continuationToken *string

	for {
		output, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(b.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}
		objects = append(objects, output.Contents...)

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	return objects, nil
}

// isNotFoundError checks if the error is an S3 NoSuchKey error.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	// Also check for NotFound error message (some S3-compatible services)
	return strings.Contains(err.Error(), "NoSuchKey") ||
		strings.Contains(err.Error(), "NotFound") ||
		strings.Contains(err.Error(), "404")
}
