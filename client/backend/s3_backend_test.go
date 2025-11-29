package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockS3Client implements the s3API interface for testing.
type MockS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte // key -> data

	// For tracking calls and simulating errors
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

func (m *MockS3Client) DeleteObjects(ctx context.Context, input *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, obj := range input.Delete.Objects {
		key := aws.ToString(obj.Key)
		delete(m.objects, key)
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (m *MockS3Client) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	m.headBucketCalled = true
	if m.headBucketErr != nil {
		return nil, m.headBucketErr
	}
	return &s3.HeadBucketOutput{}, nil
}

func (m *MockS3Client) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prefix := aws.ToString(input.Prefix)
	var objects []types.Object
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, types.Object{Key: aws.String(key)})
		}
	}
	return &s3.ListObjectsV2Output{
		Contents:    objects,
		IsTruncated: aws.Bool(false),
	}, nil
}

// NewTestableS3Backend creates an S3Backend with a mock client for testing.
// This uses the real S3Backend implementation with dependency-injected mock client.
func NewTestableS3Backend(userId, prefix string) *S3Backend {
	mock := NewMockS3Client()
	return &S3Backend{
		client: mock,
		bucket: "test-bucket",
		prefix: strings.TrimSuffix(prefix, "/"),
		userId: userId,
	}
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
	testutils.MarkTestForSharding(t, 18)
	tests := []struct {
		name     string
		config   S3Config
		wantErr  bool
		errMsg   string
		clearEnv bool // if true, temporarily unset HISHTORY_S3_SECRET_ACCESS_KEY
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
				AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
			},
			wantErr:  true,
			errMsg:   "secret access key is missing",
			clearEnv: true,
		},
		{
			name: "access key with secret",
			config: S3Config{
				Bucket:          "my-bucket",
				Region:          "us-east-1",
				AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
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
			if tt.clearEnv {
				oldVal := os.Getenv("HISHTORY_S3_SECRET_ACCESS_KEY")
				os.Unsetenv("HISHTORY_S3_SECRET_ACCESS_KEY")
				defer os.Setenv("HISHTORY_S3_SECRET_ACCESS_KEY", oldVal)
			}
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
		assert.Equal(t, "user123", devices.Devices[0].UserId)
	})

	t.Run("second device creates dump request", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register first device
		err := b.RegisterDevice(ctx, "user123", "device1")
		require.NoError(t, err)

		// Register second device
		err = b.RegisterDevice(ctx, "user123", "device2")
		require.NoError(t, err)

		// Verify dump request was created
		dumpRequests, err := b.getDumpRequests(ctx, "device1")
		require.NoError(t, err)
		assert.Len(t, dumpRequests, 1)
		assert.Equal(t, "device2", dumpRequests[0].RequestingDeviceId)
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

		// Submit entry from device1
		entries := []*shared.EncHistoryEntry{
			{
				EncryptedId: "entry1",
				DeviceId:    "device1",
				Date:        time.Now(),
			},
		}

		resp, err := b.SubmitEntries(ctx, entries, "device1")
		require.NoError(t, err)
		assert.NotNil(t, resp)

		// Verify entry is in device2's inbox (not device1's)
		device2Entries, err := b.QueryEntries(ctx, "device2", "user123", "test")
		require.NoError(t, err)
		found := false
		for _, e := range device2Entries {
			if e.EncryptedId == "entry1" {
				found = true
			}
		}
		assert.True(t, found, "Entry should be in device2's inbox")

		// Device1 should NOT have the entry in its inbox
		device1Entries, err := b.QueryEntries(ctx, "device1", "user123", "test")
		require.NoError(t, err)
		for _, e := range device1Entries {
			assert.NotEqual(t, "entry1", e.EncryptedId, "Source device should not receive its own entry")
		}
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no devices registered")
	})

	t.Run("returns pending dump requests", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register device1
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))
		// Register device2 - this creates a dump request for device2
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device2"))

		// Submit from device1 - should return the pending dump request
		entries := []*shared.EncHistoryEntry{
			{EncryptedId: "entry1", Date: time.Now(), DeviceId: "device1"},
		}

		resp, err := b.SubmitEntries(ctx, entries, "device1")
		require.NoError(t, err)
		assert.Len(t, resp.DumpRequests, 1)
		assert.Equal(t, "device2", resp.DumpRequests[0].RequestingDeviceId)
	})
}

func TestS3BackendQueryEntries(t *testing.T) {
	ctx := context.Background()

	t.Run("returns entries and keeps them until read count limit", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Manually add entry to device inbox
		entry := &shared.EncHistoryEntry{
			EncryptedId: "entry1",
			DeviceId:    "device1",
			UserId:      "user123",
			Date:        time.Now(),
			ReadCount:   0,
		}
		entryData, _ := json.Marshal(entry)
		inboxKey := b.key("inbox", "device1", "20240115T103000Z_entry1.json")
		require.NoError(t, b.putObject(ctx, inboxKey, entryData))

		// First query should return the entry with ReadCount=1
		entries, err := b.QueryEntries(ctx, "device1", "user123", "test")
		require.NoError(t, err)
		assert.Len(t, entries, 1)
		assert.Equal(t, "entry1", entries[0].EncryptedId)
		assert.Equal(t, 1, entries[0].ReadCount)

		// Entry should still exist in inbox (not deleted yet)
		data, err := b.getObject(ctx, inboxKey)
		require.NoError(t, err)
		var storedEntry shared.EncHistoryEntry
		require.NoError(t, json.Unmarshal(data, &storedEntry))
		assert.Equal(t, 1, storedEntry.ReadCount)

		// Query 4 more times (reads 2-5)
		for i := 2; i <= readCountLimit; i++ {
			entries, err = b.QueryEntries(ctx, "device1", "user123", "test")
			require.NoError(t, err)
			if i < readCountLimit {
				assert.Len(t, entries, 1, "Read %d should still return entry", i)
				assert.Equal(t, i, entries[0].ReadCount)
			}
		}

		// After 5 reads, entry should be deleted
		_, err = b.getObject(ctx, inboxKey)
		assert.Error(t, err, "Entry should be deleted after reaching read count limit")

		// Subsequent queries should return empty
		entries, err = b.QueryEntries(ctx, "device1", "user123", "test")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("empty inbox returns empty slice", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		entries, err := b.QueryEntries(ctx, "device1", "user123", "test")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("skips entries that already exceeded read count", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Add entry that has already been read 5 times
		entry := &shared.EncHistoryEntry{
			EncryptedId: "entry1",
			DeviceId:    "device1",
			UserId:      "user123",
			Date:        time.Now(),
			ReadCount:   readCountLimit, // Already at limit
		}
		entryData, _ := json.Marshal(entry)
		inboxKey := b.key("inbox", "device1", "20240115T103000Z_entry1.json")
		require.NoError(t, b.putObject(ctx, inboxKey, entryData))

		// Query should not return the entry (it's at limit)
		entries, err := b.QueryEntries(ctx, "device1", "user123", "test")
		require.NoError(t, err)
		assert.Empty(t, entries)

		// Entry should be deleted
		_, err = b.getObject(ctx, inboxKey)
		assert.Error(t, err)
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

		// Add same entry twice (different paths)
		entry := &shared.EncHistoryEntry{EncryptedId: "entry1", DeviceId: "device1", Date: time.Now()}
		data, _ := json.Marshal(entry)

		key1 := b.key("entries", "2024-01-15", "entry1.json")
		key2 := b.key("entries", "2024-01-16", "entry1.json")
		require.NoError(t, b.putObject(ctx, key1, data))
		require.NoError(t, b.putObject(ctx, key2, data))

		result, err := b.Bootstrap(ctx, "user123", "device1")
		require.NoError(t, err)
		assert.Len(t, result, 1) // Should deduplicate
	})
}

func TestS3BackendPing(t *testing.T) {
	ctx := context.Background()

	t.Run("successful ping", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		err := b.Ping(ctx)
		require.NoError(t, err)
	})

	t.Run("failed ping", func(t *testing.T) {
		mock := NewMockS3Client()
		mock.headBucketErr = &types.NotFound{}
		b := &S3Backend{
			client: mock,
			bucket: "test-bucket",
			userId: "user123",
		}

		err := b.Ping(ctx)
		require.Error(t, err)
	})
}

func TestS3BackendAddDeletionRequest(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes matching entries from main store", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register a device
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))

		// Add some entries to the main store
		entries := []struct {
			id   string
			date string
		}{
			{"entry1", "2024-01-15"},
			{"entry2", "2024-01-15"},
			{"entry3", "2024-01-16"},
		}

		for _, e := range entries {
			entry := &shared.EncHistoryEntry{EncryptedId: e.id, DeviceId: "device1", Date: time.Now()}
			data, _ := json.Marshal(entry)
			key := b.key("entries", e.date, e.id+".json")
			require.NoError(t, b.putObject(ctx, key, data))
		}

		// Delete entry1 and entry3
		delReq := shared.DeletionRequest{
			UserId:   "user123",
			Messages: shared.MessageIdentifiers{Ids: []shared.MessageIdentifier{{EntryId: "entry1"}, {EntryId: "entry3"}}},
		}

		err := b.AddDeletionRequest(ctx, delReq)
		require.NoError(t, err)

		// Verify entry1 and entry3 are deleted, but entry2 remains
		_, err = b.getObject(ctx, b.key("entries", "2024-01-15", "entry1.json"))
		assert.Error(t, err, "entry1 should be deleted")

		_, err = b.getObject(ctx, b.key("entries", "2024-01-15", "entry2.json"))
		assert.NoError(t, err, "entry2 should still exist")

		_, err = b.getObject(ctx, b.key("entries", "2024-01-16", "entry3.json"))
		assert.Error(t, err, "entry3 should be deleted")
	})

	t.Run("fans out deletion request to all devices", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register two devices
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device2"))

		delReq := shared.DeletionRequest{
			UserId:   "user123",
			Messages: shared.MessageIdentifiers{Ids: []shared.MessageIdentifier{{EntryId: "entry1"}}},
		}

		err := b.AddDeletionRequest(ctx, delReq)
		require.NoError(t, err)

		// Both devices should have deletion requests
		reqs1, err := b.GetDeletionRequests(ctx, "user123", "device1")
		require.NoError(t, err)
		assert.Len(t, reqs1, 1)

		reqs2, err := b.GetDeletionRequests(ctx, "user123", "device2")
		require.NoError(t, err)
		assert.Len(t, reqs2, 1)
	})

	t.Run("handles batch deletion of many entries", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		// Register a device
		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))

		// Add 50 entries (smaller than 1000 for unit test speed, integration test covers >1000)
		numEntries := 50
		idsToDelete := make([]shared.MessageIdentifier, 0, numEntries/2)

		for i := 0; i < numEntries; i++ {
			entryId := fmt.Sprintf("entry%d", i)
			entry := &shared.EncHistoryEntry{EncryptedId: entryId, DeviceId: "device1", Date: time.Now()}
			data, _ := json.Marshal(entry)
			key := b.key("entries", "2024-01-15", entryId+".json")
			require.NoError(t, b.putObject(ctx, key, data))

			// Delete every other entry
			if i%2 == 0 {
				idsToDelete = append(idsToDelete, shared.MessageIdentifier{EntryId: entryId})
			}
		}

		delReq := shared.DeletionRequest{
			UserId:   "user123",
			Messages: shared.MessageIdentifiers{Ids: idsToDelete},
		}

		err := b.AddDeletionRequest(ctx, delReq)
		require.NoError(t, err)

		// Verify correct entries were deleted
		for i := 0; i < numEntries; i++ {
			entryId := fmt.Sprintf("entry%d", i)
			key := b.key("entries", "2024-01-15", entryId+".json")
			_, err := b.getObject(ctx, key)
			if i%2 == 0 {
				assert.Error(t, err, "entry%d should be deleted", i)
			} else {
				assert.NoError(t, err, "entry%d should still exist", i)
			}
		}
	})

	t.Run("handles empty deletion request", func(t *testing.T) {
		b := NewTestableS3Backend("user123", "")

		require.NoError(t, b.RegisterDevice(ctx, "user123", "device1"))

		delReq := shared.DeletionRequest{
			UserId:   "user123",
			Messages: shared.MessageIdentifiers{Ids: []shared.MessageIdentifier{}},
		}

		err := b.AddDeletionRequest(ctx, delReq)
		require.NoError(t, err)
	})
}
