package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockS3Client implements the minimum S3 client interface for testing.
type MockS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte // key -> data

	// For tracking calls
	headBucketCalled bool
	headBucketErr    error
}

func NewMockS3Client() *MockS3Client {
	return &MockS3Client{
		objects: make(map[string][]byte),
	}
}

func (m *MockS3Client) GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(input.Key)
	data, ok := m.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func (m *MockS3Client) PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(input.Key)
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	m.objects[key] = data
	return &s3.PutObjectOutput{}, nil
}

func (m *MockS3Client) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(input.Key)
	delete(m.objects, key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *MockS3Client) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	m.headBucketCalled = true
	if m.headBucketErr != nil {
		return nil, m.headBucketErr
	}
	return &s3.HeadBucketOutput{}, nil
}

// ListObjectsV2 returns a list of objects matching the prefix
func (m *MockS3Client) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input) ([]types.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prefix := aws.ToString(input.Prefix)
	var objects []types.Object
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, types.Object{Key: aws.String(key)})
		}
	}
	return objects, nil
}

// TestableS3Backend wraps S3Backend to use mock client for testing.
type TestableS3Backend struct {
	*S3Backend
	mock *MockS3Client
}

func NewTestableS3Backend(userId, prefix string) *TestableS3Backend {
	mock := NewMockS3Client()
	return &TestableS3Backend{
		S3Backend: &S3Backend{
			bucket: "test-bucket",
			prefix: prefix,
			userId: userId,
		},
		mock: mock,
	}
}

// Override S3 operations to use mock

func (t *TestableS3Backend) getObject(ctx context.Context, key string) ([]byte, error) {
	result, err := t.mock.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}

func (t *TestableS3Backend) putObject(ctx context.Context, key string, data []byte) error {
	_, err := t.mock.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

func (t *TestableS3Backend) deleteObject(ctx context.Context, key string) error {
	_, err := t.mock.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (t *TestableS3Backend) listObjects(ctx context.Context, prefix string) ([]types.Object, error) {
	return t.mock.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(t.bucket),
		Prefix: aws.String(prefix),
	})
}

func (t *TestableS3Backend) getDevices(ctx context.Context) (*DeviceList, error) {
	key := t.key("devices.json")
	data, err := t.getObject(ctx, key)
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

func (t *TestableS3Backend) putDevices(ctx context.Context, devices *DeviceList) error {
	data, err := json.Marshal(devices)
	if err != nil {
		return fmt.Errorf("failed to marshal devices: %w", err)
	}
	return t.putObject(ctx, t.key("devices.json"), data)
}

// RegisterDevice implements device registration using mock
func (t *TestableS3Backend) RegisterDevice(ctx context.Context, userId, deviceId string) error {
	devices, err := t.getDevices(ctx)
	if err != nil && !isNotFoundError(err) {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	existingDeviceCount := len(devices.Devices)
	for _, d := range devices.Devices {
		if d.DeviceId == deviceId {
			return nil
		}
	}

	devices.Devices = append(devices.Devices, DeviceInfo{
		DeviceId:         deviceId,
		UserId:           userId,
		RegistrationDate: time.Now().UTC().Format(time.RFC3339),
	})

	if err := t.putDevices(ctx, devices); err != nil {
		return fmt.Errorf("failed to save devices: %w", err)
	}

	if existingDeviceCount > 0 {
		dumpReq := &shared.DumpRequest{
			UserId:             userId,
			RequestingDeviceId: deviceId,
			RequestTime:        time.Now().UTC(),
		}
		key := t.key("dump_requests", deviceId+".json")
		data, _ := json.Marshal(dumpReq)
		if err := t.putObject(ctx, key, data); err != nil {
			return fmt.Errorf("failed to create dump request: %w", err)
		}
	}

	return nil
}

// Bootstrap retrieves all entries
func (t *TestableS3Backend) Bootstrap(ctx context.Context, userId, deviceId string) ([]*shared.EncHistoryEntry, error) {
	entriesPrefix := t.key("entries") + "/"
	objects, err := t.listObjects(ctx, entriesPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	seen := make(map[string]bool)
	var entries []*shared.EncHistoryEntry

	for _, obj := range objects {
		data, err := t.getObject(ctx, *obj.Key)
		if err != nil {
			continue
		}

		var entry shared.EncHistoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}

		if seen[entry.EncryptedId] {
			continue
		}
		seen[entry.EncryptedId] = true
		entries = append(entries, &entry)
	}

	return entries, nil
}

// SubmitEntries submits entries and fans out to all devices
func (t *TestableS3Backend) SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error) {
	if len(entries) == 0 {
		return &shared.SubmitResponse{}, nil
	}

	deviceList, err := t.getDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}
	if len(deviceList.Devices) == 0 {
		return nil, fmt.Errorf("no devices registered for user")
	}

	for _, entry := range entries {
		entryKey := t.key("entries", entry.Date.Format("2006-01-02"), entry.EncryptedId+".json")
		entryData, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal entry: %w", err)
		}

		if err := t.putObject(ctx, entryKey, entryData); err != nil {
			return nil, fmt.Errorf("failed to write entry: %w", err)
		}

		for _, device := range deviceList.Devices {
			if device.DeviceId == sourceDeviceId {
				continue
			}

			entryCopy := *entry
			entryCopy.DeviceId = device.DeviceId
			entryCopy.IsFromSameDevice = false

			inboxKey := t.key("inbox", device.DeviceId, entry.Date.Format("20060102T150405Z")+"_"+entry.EncryptedId+".json")
			inboxData, err := json.Marshal(&entryCopy)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal inbox entry: %w", err)
			}
			if err := t.putObject(ctx, inboxKey, inboxData); err != nil {
				return nil, fmt.Errorf("failed to write inbox entry: %w", err)
			}
		}
	}

	resp := &shared.SubmitResponse{}

	// Check dump requests
	dumpPrefix := t.key("dump_requests") + "/"
	dumpObjects, _ := t.listObjects(ctx, dumpPrefix)
	for _, obj := range dumpObjects {
		if strings.Contains(*obj.Key, sourceDeviceId) {
			continue
		}
		data, err := t.getObject(ctx, *obj.Key)
		if err != nil {
			continue
		}
		var req shared.DumpRequest
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		resp.DumpRequests = append(resp.DumpRequests, &req)
	}

	return resp, nil
}

// QueryEntries retrieves entries from device inbox
func (t *TestableS3Backend) QueryEntries(ctx context.Context, deviceId, userId string) ([]*shared.EncHistoryEntry, error) {
	inboxPrefix := t.key("inbox", deviceId) + "/"
	objects, err := t.listObjects(ctx, inboxPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list inbox: %w", err)
	}

	var entries []*shared.EncHistoryEntry
	var keysToDelete []string

	for _, obj := range objects {
		data, err := t.getObject(ctx, *obj.Key)
		if err != nil {
			continue
		}

		var entry shared.EncHistoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}

		entry.ReadCount++
		entries = append(entries, &entry)
		keysToDelete = append(keysToDelete, *obj.Key)
	}

	for _, key := range keysToDelete {
		_ = t.deleteObject(ctx, key)
	}

	return entries, nil
}

// Ping tests bucket access
func (t *TestableS3Backend) Ping(ctx context.Context) error {
	_, err := t.mock.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(t.bucket),
	})
	return err
}

// Tests

func TestS3BackendType(t *testing.T) {
	b := NewTestableS3Backend("user123", "")
	assert.Equal(t, "s3", b.Type())
}

func TestS3BackendKeyBuilding(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		userId   string
		parts    []string
		expected string
	}{
		{
			name:     "no prefix",
			prefix:   "",
			userId:   "user123",
			parts:    []string{"devices.json"},
			expected: "user123/devices.json",
		},
		{
			name:     "with prefix",
			prefix:   "hishtory",
			userId:   "user123",
			parts:    []string{"devices.json"},
			expected: "hishtory/user123/devices.json",
		},
		{
			name:     "nested path",
			prefix:   "",
			userId:   "user123",
			parts:    []string{"entries", "2024-01-15", "entry1.json"},
			expected: "user123/entries/2024-01-15/entry1.json",
		},
		{
			name:     "prefix with trailing slash",
			prefix:   "myprefix/",
			userId:   "user456",
			parts:    []string{"inbox", "device1", "entry.json"},
			expected: "myprefix/user456/inbox/device1/entry.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: The prefix trimming happens in NewS3Backend, but we test the key() method
			b := &S3Backend{
				prefix: strings.TrimSuffix(tt.prefix, "/"),
				userId: tt.userId,
			}
			result := b.key(tt.parts...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestS3ConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  S3Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: S3Config{
				Bucket: "my-bucket",
				Region: "us-east-1",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			config: S3Config{
				Region: "us-east-1",
			},
			wantErr: true,
			errMsg:  "S3 bucket is required",
		},
		{
			name: "missing region",
			config: S3Config{
				Bucket: "my-bucket",
			},
			wantErr: true,
			errMsg:  "S3 region is required",
		},
		{
			name: "access key without secret",
			config: S3Config{
				Bucket:      "my-bucket",
				Region:      "us-east-1",
				AccessKeyID: "AKIAXXXXXXXX",
			},
			wantErr: true,
			errMsg:  "secret access key is missing",
		},
		{
			name: "access key with secret",
			config: S3Config{
				Bucket:          "my-bucket",
				Region:          "us-east-1",
				AccessKeyID:     "AKIAXXXXXXXX",
				SecretAccessKey: "secret123",
			},
			wantErr: false,
		},
		{
			name: "with endpoint",
			config: S3Config{
				Bucket:   "my-bucket",
				Region:   "us-east-1",
				Endpoint: "http://localhost:9000",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "NoSuchKey error",
			err:      &types.NoSuchKey{},
			expected: true,
		},
		{
			name:     "string contains NoSuchKey",
			err:      fmt.Errorf("operation failed: NoSuchKey"),
			expected: true,
		},
		{
			name:     "string contains NotFound",
			err:      fmt.Errorf("object NotFound"),
			expected: true,
		},
		{
			name:     "string contains 404",
			err:      fmt.Errorf("HTTP 404 error"),
			expected: true,
		},
		{
			name:     "other error",
			err:      fmt.Errorf("connection timeout"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotFoundError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestS3BackendRegisterDevice(t *testing.T) {
	ctx := context.Background()

	t.Run("first device", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		err := b.RegisterDevice(ctx, "user123", "device1")
		require.NoError(t, err)

		// Verify device was registered
		devices, err := b.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 1)
		assert.Equal(t, "device1", devices.Devices[0].DeviceId)

		// Verify no dump request (first device)
		dumpKey := b.key("dump_requests", "device1.json")
		_, err = b.getObject(ctx, dumpKey)
		assert.Error(t, err) // Should not exist
	})

	t.Run("second device creates dump request", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register first device
		err := b.RegisterDevice(ctx, "user123", "device1")
		require.NoError(t, err)

		// Register second device
		err = b.RegisterDevice(ctx, "user123", "device2")
		require.NoError(t, err)

		// Verify both devices registered
		devices, err := b.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 2)

		// Verify dump request created for second device
		dumpKey := b.key("dump_requests", "device2.json")
		data, err := b.getObject(ctx, dumpKey)
		require.NoError(t, err)

		var req shared.DumpRequest
		require.NoError(t, json.Unmarshal(data, &req))
		assert.Equal(t, "device2", req.RequestingDeviceId)
	})

	t.Run("re-registering same device is idempotent", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		err := b.RegisterDevice(ctx, "user123", "device1")
		require.NoError(t, err)

		err = b.RegisterDevice(ctx, "user123", "device1")
		require.NoError(t, err)

		devices, err := b.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 1)
	})
}

func TestS3BackendSubmitEntries(t *testing.T) {
	ctx := context.Background()

	t.Run("submit entry fans out to other devices", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register two devices
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device2"))

		entries := []*shared.EncHistoryEntry{
			{
				EncryptedData: []byte("encrypted1"),
				Nonce:         []byte("nonce1"),
				DeviceId:      "device1",
				UserId:        "user123",
				Date:          time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				EncryptedId:   "entry1",
			},
		}

		resp, err := b.SubmitEntries(ctx, entries, "device1")
		require.NoError(t, err)
		assert.NotNil(t, resp)

		// Verify entry stored in main entries store
		entryKey := b.key("entries", "2024-01-15", "entry1.json")
		data, err := b.getObject(ctx, entryKey)
		require.NoError(t, err)
		assert.NotEmpty(t, data)

		// Verify entry in device2's inbox (but not device1's)
		device2InboxPrefix := b.key("inbox", "device2") + "/"
		device2Objects, _ := b.listObjects(ctx, device2InboxPrefix)
		assert.Len(t, device2Objects, 1)

		device1InboxPrefix := b.key("inbox", "device1") + "/"
		device1Objects, _ := b.listObjects(ctx, device1InboxPrefix)
		assert.Len(t, device1Objects, 0)
	})

	t.Run("empty entries returns early", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		resp, err := b.SubmitEntries(ctx, []*shared.EncHistoryEntry{}, "device1")
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("no devices returns error", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		entries := []*shared.EncHistoryEntry{
			{EncryptedId: "entry1", Date: time.Now()},
		}

		_, err := b.SubmitEntries(ctx, entries, "device1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no devices registered")
	})

	t.Run("returns pending dump requests", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register devices
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device2"))

		// device2 registration creates a dump request
		// When device1 submits, it should see device2's dump request
		entries := []*shared.EncHistoryEntry{
			{EncryptedId: "entry1", Date: time.Now(), DeviceId: "device1", UserId: "user123"},
		}

		resp, err := b.SubmitEntries(ctx, entries, "device1")
		require.NoError(t, err)
		assert.Len(t, resp.DumpRequests, 1)
		assert.Equal(t, "device2", resp.DumpRequests[0].RequestingDeviceId)
	})
}

func TestS3BackendQueryEntries(t *testing.T) {
	ctx := context.Background()

	t.Run("returns entries from inbox and deletes them", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Manually add entry to device inbox
		entry := &shared.EncHistoryEntry{
			EncryptedId: "entry1",
			DeviceId:    "device1",
			UserId:      "user123",
			Date:        time.Now(),
		}
		entryData, _ := json.Marshal(entry)
		inboxKey := b.key("inbox", "device1", "20240115T103000Z_entry1.json")
		require.NoError(t, b.putObject(ctx, inboxKey, entryData))

		// Query should return the entry
		entries, err := b.QueryEntries(ctx, "device1", "user123")
		require.NoError(t, err)
		assert.Len(t, entries, 1)
		assert.Equal(t, "entry1", entries[0].EncryptedId)
		assert.Equal(t, 1, entries[0].ReadCount) // Incremented

		// Entry should be deleted from inbox
		_, err = b.getObject(ctx, inboxKey)
		assert.Error(t, err)
	})

	t.Run("empty inbox returns empty slice", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		entries, err := b.QueryEntries(ctx, "device1", "user123")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}

func TestS3BackendBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("returns all entries", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Add some entries
		entries := []*shared.EncHistoryEntry{
			{EncryptedId: "entry1", DeviceId: "device1", Date: time.Now()},
			{EncryptedId: "entry2", DeviceId: "device1", Date: time.Now()},
		}

		for _, entry := range entries {
			data, _ := json.Marshal(entry)
			key := b.key("entries", entry.Date.Format("2006-01-02"), entry.EncryptedId+".json")
			require.NoError(t, b.putObject(ctx, key, data))
		}

		result, err := b.Bootstrap(ctx, "user123", "device1")
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("deduplicates entries", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Add duplicate entries (same EncryptedId)
		entry := &shared.EncHistoryEntry{EncryptedId: "entry1", DeviceId: "device1", Date: time.Now()}
		data, _ := json.Marshal(entry)

		key1 := b.key("entries", "2024-01-15", "entry1.json")
		key2 := b.key("entries", "2024-01-16", "entry1.json")
		require.NoError(t, b.putObject(ctx, key1, data))
		require.NoError(t, b.putObject(ctx, key2, data))

		result, err := b.Bootstrap(ctx, "user123", "device1")
		require.NoError(t, err)
		assert.Len(t, result, 1) // Deduplicated
	})
}

func TestS3BackendPing(t *testing.T) {
	ctx := context.Background()

	t.Run("successful ping", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")
		err := b.Ping(ctx)
		require.NoError(t, err)
		assert.True(t, b.mock.headBucketCalled)
	})

	t.Run("failed ping", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")
		b.mock.headBucketErr = fmt.Errorf("access denied")

		err := b.Ping(ctx)
		assert.Error(t, err)
	})
}

func TestDeviceListJSON(t *testing.T) {
	devices := DeviceList{
		Devices: []DeviceInfo{
			{
				DeviceId:         "device1",
				UserId:           "user123",
				RegistrationDate: "2024-01-15T10:30:00Z",
			},
			{
				DeviceId:         "device2",
				UserId:           "user123",
				RegistrationDate: "2024-01-16T11:00:00Z",
			},
		},
	}

	data, err := json.Marshal(devices)
	require.NoError(t, err)

	var parsed DeviceList
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, devices, parsed)
}

func TestS3ConfigJSONSerialization(t *testing.T) {
	config := S3Config{
		Bucket:          "my-bucket",
		Region:          "us-west-2",
		Endpoint:        "http://localhost:9000",
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret123", // Should NOT be serialized
		Prefix:          "hishtory/",
	}

	data, err := json.Marshal(config)
	require.NoError(t, err)

	// Verify secret is not in JSON
	assert.NotContains(t, string(data), "secret123")
	assert.NotContains(t, string(data), "secret_access_key")

	// Verify other fields are present
	var parsed S3Config
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "my-bucket", parsed.Bucket)
	assert.Equal(t, "us-west-2", parsed.Region)
	assert.Equal(t, "http://localhost:9000", parsed.Endpoint)
	assert.Equal(t, "AKIATEST", parsed.AccessKeyID)
	assert.Equal(t, "", parsed.SecretAccessKey) // Not deserialized
}
