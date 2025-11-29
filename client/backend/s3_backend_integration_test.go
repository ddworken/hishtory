package backend

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/testutils"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain starts MinIO for S3 integration tests
func TestMain(m *testing.M) {
	// Start MinIO for S3 backend tests
	cleanup := testutils.RunMinioServer()
	defer cleanup()

	// Skip S3 integration tests on macOS in GitHub Actions if MinIO isn't available
	// (Docker/colima is flaky on macOS runners)
	if testutils.IsGithubAction() && runtime.GOOS == "darwin" && !testutils.IsMinioRunning() {
		fmt.Println("Skipping S3 integration tests: MinIO not available on macOS in GitHub Actions")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// newTestS3Config returns an S3Config for connecting to the test MinIO server
func newTestS3Config() *S3Config {
	return &S3Config{
		Bucket:          testutils.MinioBucket,
		Region:          testutils.MinioRegion,
		Endpoint:        testutils.MinioEndpoint,
		AccessKeyID:     testutils.MinioAccessKeyID,
		SecretAccessKey: testutils.MinioSecretAccessKey,
	}
}

// newTestBackend creates a new S3Backend for testing with a unique user ID
func newTestBackend(t *testing.T, userId string) *S3Backend {
	ctx := context.Background()
	cfg := newTestS3Config()
	backend, err := NewS3Backend(ctx, cfg, userId)
	require.NoError(t, err, "Failed to create S3 backend")
	return backend
}

// TestS3Integration_NewBackendAndPing tests creating an S3 backend and verifying bucket access
func TestS3Integration_NewBackendAndPing(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-" + uuid.New().String()[:8]

	// Create backend
	backend := newTestBackend(t, userId)

	// Verify type
	assert.Equal(t, "s3", backend.Type(), "Backend type should be s3")

	// Ping should succeed
	err := backend.Ping(ctx)
	require.NoError(t, err, "Ping should succeed with valid MinIO connection")
}

func TestS3Integration_PingFailsWithInvalidBucket(t *testing.T) {
	ctx := context.Background()
	cfg := newTestS3Config()
	cfg.Bucket = "nonexistent-bucket-" + uuid.New().String()

	backend, err := NewS3Backend(ctx, cfg, "test-user")
	require.NoError(t, err, "Backend creation should succeed even with invalid bucket")

	// Ping should fail with invalid bucket
	err = backend.Ping(ctx)
	assert.Error(t, err, "Ping should fail with nonexistent bucket")
}

// TestS3Integration_RegisterDevice tests device registration functionality
func TestS3Integration_RegisterDevice(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-" + uuid.New().String()[:8]

	t.Run("first device has no dump request", func(t *testing.T) {
		backend := newTestBackend(t, userId+"-first")

		err := backend.RegisterDevice(ctx, userId, "device1")
		require.NoError(t, err, "Registering first device should succeed")

		// Verify device is registered by checking devices list
		devices, err := backend.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 1)
		assert.Equal(t, "device1", devices.Devices[0].DeviceId)

		// No dump requests should exist (first device)
		dumpReqs, err := backend.getDumpRequests(ctx, "device1")
		require.NoError(t, err)
		assert.Empty(t, dumpReqs, "First device should have no dump requests")
	})

	t.Run("second device creates dump request", func(t *testing.T) {
		backend := newTestBackend(t, userId+"-second")

		// Register first device
		err := backend.RegisterDevice(ctx, userId, "device1")
		require.NoError(t, err)

		// Register second device
		err = backend.RegisterDevice(ctx, userId, "device2")
		require.NoError(t, err)

		// Verify both devices are registered
		devices, err := backend.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 2)

		// Dump request should exist for device2 (visible to device1)
		dumpReqs, err := backend.getDumpRequests(ctx, "device1")
		require.NoError(t, err)
		require.Len(t, dumpReqs, 1, "Should have one dump request for device2")
		assert.Equal(t, "device2", dumpReqs[0].RequestingDeviceId)
	})

	t.Run("re-registering same device is idempotent", func(t *testing.T) {
		backend := newTestBackend(t, userId+"-idempotent")

		err := backend.RegisterDevice(ctx, userId, "device1")
		require.NoError(t, err)

		// Re-register same device
		err = backend.RegisterDevice(ctx, userId, "device1")
		require.NoError(t, err)

		devices, err := backend.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 1, "Should still have only one device")
	})
}

// TestS3Integration_SubmitEntries tests entry submission with fan-out to devices
func TestS3Integration_SubmitEntries(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	// Register two devices
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)
	err = backend.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	t.Run("entries are stored and fanned out", func(t *testing.T) {
		entryId := uuid.New().String()[:8]
		entries := []*shared.EncHistoryEntry{
			{
				EncryptedData: []byte("encrypted-data-1"),
				Nonce:         []byte("nonce-1"),
				DeviceId:      "device1",
				UserId:        userId,
				Date:          time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				EncryptedId:   "entry-" + entryId,
			},
		}

		resp, err := backend.SubmitEntries(ctx, entries, "device1")
		require.NoError(t, err)
		assert.NotNil(t, resp)

		// Verify entry is stored in main entries store via Bootstrap
		allEntries, err := backend.Bootstrap(ctx, userId, "device1")
		require.NoError(t, err)
		found := false
		for _, e := range allEntries {
			if e.EncryptedId == "entry-"+entryId {
				found = true
				break
			}
		}
		assert.True(t, found, "Entry should be in main entries store")

		// Verify entry is in device2's inbox (not device1's)
		device2Entries, err := backend.QueryEntries(ctx, "device2", userId, "test")
		require.NoError(t, err)
		found = false
		for _, e := range device2Entries {
			if e.EncryptedId == "entry-"+entryId {
				found = true
				assert.Equal(t, 1, e.ReadCount, "ReadCount should be incremented")
				break
			}
		}
		assert.True(t, found, "Entry should be in device2's inbox")

		// Device1 (source) should NOT have the entry in its inbox
		device1Entries, err := backend.QueryEntries(ctx, "device1", userId, "test")
		require.NoError(t, err)
		for _, e := range device1Entries {
			assert.NotEqual(t, "entry-"+entryId, e.EncryptedId, "Source device should not receive its own entry")
		}
	})

	t.Run("empty entries returns early", func(t *testing.T) {
		resp, err := backend.SubmitEntries(ctx, []*shared.EncHistoryEntry{}, "device1")
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns pending dump requests", func(t *testing.T) {
		// Register a third device to create a dump request
		err := backend.RegisterDevice(ctx, userId, "device3")
		require.NoError(t, err)

		// Submit entries from device1
		entries := []*shared.EncHistoryEntry{
			{EncryptedId: "entry-" + uuid.New().String()[:8], Date: time.Now(), DeviceId: "device1", UserId: userId},
		}

		resp, err := backend.SubmitEntries(ctx, entries, "device1")
		require.NoError(t, err)

		// Should see device3's dump request (and possibly device2's if not yet handled)
		foundDevice3 := false
		for _, req := range resp.DumpRequests {
			if req.RequestingDeviceId == "device3" {
				foundDevice3 = true
			}
		}
		assert.True(t, foundDevice3, "Should see device3's dump request")
	})
}

// TestS3Integration_SubmitEntriesNoDevices tests that SubmitEntries fails when no devices are registered
func TestS3Integration_SubmitEntriesNoDevices(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-nodevices-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "entry-1", Date: time.Now()},
	}

	_, err := backend.SubmitEntries(ctx, entries, "device1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no devices registered")
}

// TestS3Integration_QueryEntries tests querying entries from device inbox
func TestS3Integration_QueryEntries(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-query-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	// Register devices and submit entries
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)
	err = backend.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	entryId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{
			EncryptedId:   "query-entry-" + entryId,
			EncryptedData: []byte("data"),
			Date:          time.Now(),
			DeviceId:      "device1",
			UserId:        userId,
		},
	}

	_, err = backend.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	t.Run("returns entries and increments ReadCount", func(t *testing.T) {
		result, err := backend.QueryEntries(ctx, "device2", userId, "test")
		require.NoError(t, err)

		found := false
		for _, e := range result {
			if e.EncryptedId == "query-entry-"+entryId {
				found = true
				assert.Equal(t, 1, e.ReadCount)
			}
		}
		assert.True(t, found, "Should find the entry in query results")
	})

	t.Run("entries persist until read count limit is reached", func(t *testing.T) {
		// Read count is already 1 from previous test, so we need 4 more reads to hit the limit
		// Reads 2, 3, 4 should still return the entry
		for i := 2; i < 5; i++ {
			result, err := backend.QueryEntries(ctx, "device2", userId, "test")
			require.NoError(t, err)

			found := false
			for _, e := range result {
				if e.EncryptedId == "query-entry-"+entryId {
					found = true
					assert.Equal(t, i, e.ReadCount, "Read %d should have ReadCount=%d", i, i)
				}
			}
			assert.True(t, found, "Read %d should still find the entry", i)
		}

		// Read 5 (the limit) - entry is returned but then deleted
		result, err := backend.QueryEntries(ctx, "device2", userId, "test")
		require.NoError(t, err)
		found := false
		for _, e := range result {
			if e.EncryptedId == "query-entry-"+entryId {
				found = true
				assert.Equal(t, 5, e.ReadCount)
			}
		}
		assert.True(t, found, "Read 5 should still return the entry")

		// Read 6 - entry should be gone now
		result, err = backend.QueryEntries(ctx, "device2", userId, "test")
		require.NoError(t, err)
		for _, e := range result {
			assert.NotEqual(t, "query-entry-"+entryId, e.EncryptedId, "Entry should be deleted after reaching read count limit")
		}
	})

	t.Run("empty inbox returns empty slice", func(t *testing.T) {
		emptyBackend := newTestBackend(t, "empty-user-"+uuid.New().String()[:8])
		result, err := emptyBackend.QueryEntries(ctx, "nonexistent-device", "empty-user", "test")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

// TestS3Integration_Bootstrap tests bootstrapping all entries for a user
func TestS3Integration_Bootstrap(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-bootstrap-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	// Register device and submit multiple entries
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)

	baseId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "bootstrap-1-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data1")},
		{EncryptedId: "bootstrap-2-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data2")},
		{EncryptedId: "bootstrap-3-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data3")},
	}

	_, err = backend.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	t.Run("returns all entries", func(t *testing.T) {
		result, err := backend.Bootstrap(ctx, userId, "device1")
		require.NoError(t, err)

		foundIds := make(map[string]bool)
		for _, e := range result {
			foundIds[e.EncryptedId] = true
		}

		assert.True(t, foundIds["bootstrap-1-"+baseId], "Should have entry 1")
		assert.True(t, foundIds["bootstrap-2-"+baseId], "Should have entry 2")
		assert.True(t, foundIds["bootstrap-3-"+baseId], "Should have entry 3")
	})

	t.Run("deduplicates entries with same EncryptedId", func(t *testing.T) {
		// Submit duplicate entry
		duplicateEntry := []*shared.EncHistoryEntry{
			{EncryptedId: "bootstrap-1-" + baseId, Date: time.Now().Add(time.Hour), DeviceId: "device1", UserId: userId, EncryptedData: []byte("duplicate")},
		}
		_, err := backend.SubmitEntries(ctx, duplicateEntry, "device1")
		require.NoError(t, err)

		result, err := backend.Bootstrap(ctx, userId, "device1")
		require.NoError(t, err)

		// Count occurrences of the duplicated entry
		count := 0
		for _, e := range result {
			if e.EncryptedId == "bootstrap-1-"+baseId {
				count++
			}
		}
		assert.Equal(t, 1, count, "Should deduplicate entries with same EncryptedId")
	})
}

// TestS3Integration_DumpRequestFlow tests the complete dump request workflow
func TestS3Integration_DumpRequestFlow(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-dump-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	// Register first device
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)

	// Submit some entries from device1
	baseId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "dump-entry-1-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data1")},
		{EncryptedId: "dump-entry-2-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data2")},
	}
	_, err = backend.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	// Register second device (creates dump request)
	err = backend.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	t.Run("dump request is created for new device", func(t *testing.T) {
		// Device1 should see device2's dump request
		dumpReqs, err := backend.getDumpRequests(ctx, "device1")
		require.NoError(t, err)
		require.Len(t, dumpReqs, 1)
		assert.Equal(t, "device2", dumpReqs[0].RequestingDeviceId)
	})

	t.Run("SubmitDump transfers entries to requesting device", func(t *testing.T) {
		// Get all entries for dumping
		allEntries, err := backend.Bootstrap(ctx, userId, "device1")
		require.NoError(t, err)

		// Submit dump to device2
		err = backend.SubmitDump(ctx, allEntries, userId, "device2", "device1")
		require.NoError(t, err)

		// Device2 should now have entries in its inbox
		device2Entries, err := backend.QueryEntries(ctx, "device2", userId, "test")
		require.NoError(t, err)

		foundIds := make(map[string]bool)
		for _, e := range device2Entries {
			foundIds[e.EncryptedId] = true
		}
		assert.True(t, foundIds["dump-entry-1-"+baseId], "Device2 should have dump entry 1")
		assert.True(t, foundIds["dump-entry-2-"+baseId], "Device2 should have dump entry 2")
	})

	t.Run("dump request is cleared after SubmitDump", func(t *testing.T) {
		dumpReqs, err := backend.getDumpRequests(ctx, "device1")
		require.NoError(t, err)

		for _, req := range dumpReqs {
			assert.NotEqual(t, "device2", req.RequestingDeviceId, "Device2's dump request should be cleared")
		}
	})
}

// TestS3Integration_DeletionRequests tests deletion request creation and retrieval
func TestS3Integration_DeletionRequests(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-del-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	// Register devices
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)
	err = backend.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	// Clear any pending entries from device2's inbox (from the dump request)
	_, _ = backend.QueryEntries(ctx, "device2", userId, "test")

	// Submit entries
	entryId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "del-entry-" + entryId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data")},
	}
	_, err = backend.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	// Clear device2's inbox again
	_, _ = backend.QueryEntries(ctx, "device2", userId, "test")

	t.Run("AddDeletionRequest fans out to devices", func(t *testing.T) {
		delReq := shared.DeletionRequest{
			UserId: userId,
			Messages: shared.MessageIdentifiers{
				Ids: []shared.MessageIdentifier{
					{EntryId: "del-entry-" + entryId},
				},
			},
		}

		err := backend.AddDeletionRequest(ctx, delReq)
		require.NoError(t, err)

		// Both devices should have the deletion request
		dev1DelReqs, err := backend.GetDeletionRequests(ctx, userId, "device1")
		require.NoError(t, err)
		assert.Len(t, dev1DelReqs, 1, "Device1 should have deletion request")

		dev2DelReqs, err := backend.GetDeletionRequests(ctx, userId, "device2")
		require.NoError(t, err)
		assert.Len(t, dev2DelReqs, 1, "Device2 should have deletion request")
	})

	t.Run("deletion requests persist until read count limit is reached", func(t *testing.T) {
		// Read count is already 1 from previous test, need 4 more reads to hit limit of 5
		for i := 2; i <= 5; i++ {
			dev1DelReqs, err := backend.GetDeletionRequests(ctx, userId, "device1")
			require.NoError(t, err)
			if i < 5 {
				assert.Len(t, dev1DelReqs, 1, "Read %d should still have deletion request", i)
				assert.Equal(t, i, dev1DelReqs[0].ReadCount, "ReadCount should be %d", i)
			} else {
				// On read 5, we get the request but it gets deleted after
				assert.Len(t, dev1DelReqs, 1, "Read 5 should still return deletion request")
			}
		}

		// After 5 reads, deletion requests should be gone
		dev1DelReqs, err := backend.GetDeletionRequests(ctx, userId, "device1")
		require.NoError(t, err)
		assert.Empty(t, dev1DelReqs, "Deletion requests should be removed after reaching read count limit")
	})

	t.Run("entry is deleted from main entries store", func(t *testing.T) {
		allEntries, err := backend.Bootstrap(ctx, userId, "device1")
		require.NoError(t, err)

		for _, e := range allEntries {
			assert.NotEqual(t, "del-entry-"+entryId, e.EncryptedId, "Deleted entry should not be in entries store")
		}
	})
}

// TestS3Integration_Uninstall tests device uninstallation and cleanup
func TestS3Integration_Uninstall(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-uninstall-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	// Register devices
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)
	err = backend.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	// Submit entries (this puts entries in device2's inbox)
	entryId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "uninstall-entry-" + entryId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data")},
	}
	_, err = backend.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	// Add a deletion request for device2
	delReq := shared.DeletionRequest{
		UserId: userId,
		Messages: shared.MessageIdentifiers{
			Ids: []shared.MessageIdentifier{{EntryId: "some-entry"}},
		},
	}
	err = backend.AddDeletionRequest(ctx, delReq)
	require.NoError(t, err)

	t.Run("uninstall removes device and cleans up", func(t *testing.T) {
		err := backend.Uninstall(ctx, userId, "device2")
		require.NoError(t, err)

		// Verify device is removed from list
		devices, err := backend.getDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices.Devices, 1)
		assert.Equal(t, "device1", devices.Devices[0].DeviceId)

		// Verify inbox is cleaned up (QueryEntries should return empty for device2)
		entries, err := backend.QueryEntries(ctx, "device2", userId, "test")
		require.NoError(t, err)
		assert.Empty(t, entries, "Device2's inbox should be cleaned up")

		// Verify deletion requests are cleaned up
		delReqs, err := backend.GetDeletionRequests(ctx, userId, "device2")
		require.NoError(t, err)
		assert.Empty(t, delReqs, "Device2's deletion requests should be cleaned up")
	})
}

// TestS3Integration_FullSyncWorkflow tests a complete sync workflow across multiple devices
func TestS3Integration_FullSyncWorkflow(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-fullsync-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	baseId := uuid.New().String()[:8]

	// Step 1: Device 1 registers and submits commands
	t.Log("Step 1: Device 1 registers")
	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)

	t.Log("Step 1: Device 1 submits entries")
	entries1 := []*shared.EncHistoryEntry{
		{EncryptedId: "sync-1-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("echo hello")},
		{EncryptedId: "sync-2-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("ls -la")},
	}
	_, err = backend.SubmitEntries(ctx, entries1, "device1")
	require.NoError(t, err)

	// Step 2: Device 2 registers (creates dump request)
	t.Log("Step 2: Device 2 registers")
	err = backend.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	// Step 3: Device 1 sees dump request and sends dump
	t.Log("Step 3: Device 1 submits more entries and sees dump request")
	entries2 := []*shared.EncHistoryEntry{
		{EncryptedId: "sync-3-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("pwd")},
	}
	resp, err := backend.SubmitEntries(ctx, entries2, "device1")
	require.NoError(t, err)
	require.Len(t, resp.DumpRequests, 1, "Should see device2's dump request")

	// Device 1 performs dump
	t.Log("Step 3: Device 1 performs dump to device 2")
	allEntries, err := backend.Bootstrap(ctx, userId, "device1")
	require.NoError(t, err)
	err = backend.SubmitDump(ctx, allEntries, userId, "device2", "device1")
	require.NoError(t, err)

	// Step 4: Device 2 queries and gets all history
	t.Log("Step 4: Device 2 queries entries")
	device2Entries, err := backend.QueryEntries(ctx, "device2", userId, "test")
	require.NoError(t, err)

	foundIds := make(map[string]bool)
	for _, e := range device2Entries {
		foundIds[e.EncryptedId] = true
	}
	assert.True(t, foundIds["sync-1-"+baseId], "Device 2 should have sync-1")
	assert.True(t, foundIds["sync-2-"+baseId], "Device 2 should have sync-2")
	assert.True(t, foundIds["sync-3-"+baseId], "Device 2 should have sync-3")

	// Step 5: Device 2 submits its own commands
	t.Log("Step 5: Device 2 submits entries")
	entries3 := []*shared.EncHistoryEntry{
		{EncryptedId: "sync-4-" + baseId, Date: time.Now(), DeviceId: "device2", UserId: userId, EncryptedData: []byte("git status")},
	}
	_, err = backend.SubmitEntries(ctx, entries3, "device2")
	require.NoError(t, err)

	// Step 6: Device 1 queries and gets device 2's command
	t.Log("Step 6: Device 1 queries entries")
	device1Entries, err := backend.QueryEntries(ctx, "device1", userId, "test")
	require.NoError(t, err)

	found := false
	for _, e := range device1Entries {
		if e.EncryptedId == "sync-4-"+baseId {
			found = true
			break
		}
	}
	assert.True(t, found, "Device 1 should receive device 2's entry")

	// Step 7: Bootstrap shows all entries
	t.Log("Step 7: Bootstrap shows all entries")
	allEntriesFinal, err := backend.Bootstrap(ctx, userId, "device1")
	require.NoError(t, err)

	foundIds = make(map[string]bool)
	for _, e := range allEntriesFinal {
		foundIds[e.EncryptedId] = true
	}
	assert.True(t, foundIds["sync-1-"+baseId], "Bootstrap should have sync-1")
	assert.True(t, foundIds["sync-2-"+baseId], "Bootstrap should have sync-2")
	assert.True(t, foundIds["sync-3-"+baseId], "Bootstrap should have sync-3")
	assert.True(t, foundIds["sync-4-"+baseId], "Bootstrap should have sync-4")
}

// TestS3Integration_ConcurrentDevices tests concurrent operations from multiple "backends"
func TestS3Integration_ConcurrentDevices(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-concurrent-" + uuid.New().String()[:8]

	// Create two backends simulating two devices
	backend1 := newTestBackend(t, userId)
	backend2 := newTestBackend(t, userId)

	// Both backends share the same userId, so they share the same S3 prefix

	// Register device 1
	err := backend1.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)

	// Register device 2 using backend2
	err = backend2.RegisterDevice(ctx, userId, "device2")
	require.NoError(t, err)

	// Both backends should see both devices
	devices1, err := backend1.getDevices(ctx)
	require.NoError(t, err)
	assert.Len(t, devices1.Devices, 2)

	devices2, err := backend2.getDevices(ctx)
	require.NoError(t, err)
	assert.Len(t, devices2.Devices, 2)

	// Submit entries from backend1
	baseId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "concurrent-" + baseId, Date: time.Now(), DeviceId: "device1", UserId: userId, EncryptedData: []byte("data")},
	}
	_, err = backend1.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	// Backend2 should be able to query the entry from device2's inbox
	result, err := backend2.QueryEntries(ctx, "device2", userId, "test")
	require.NoError(t, err)

	found := false
	for _, e := range result {
		if e.EncryptedId == "concurrent-"+baseId {
			found = true
			break
		}
	}
	assert.True(t, found, "Backend2 should see entry submitted by backend1")
}

// TestS3Integration_MultipleEntriesSameTimestamp tests handling of multiple entries with similar timestamps
func TestS3Integration_MultipleEntriesSameTimestamp(t *testing.T) {
	ctx := context.Background()
	userId := "test-user-timestamp-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)

	// Submit multiple entries with the same timestamp
	timestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	baseId := uuid.New().String()[:8]
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "ts-1-" + baseId, Date: timestamp, DeviceId: "device1", UserId: userId, EncryptedData: []byte("cmd1")},
		{EncryptedId: "ts-2-" + baseId, Date: timestamp, DeviceId: "device1", UserId: userId, EncryptedData: []byte("cmd2")},
		{EncryptedId: "ts-3-" + baseId, Date: timestamp, DeviceId: "device1", UserId: userId, EncryptedData: []byte("cmd3")},
	}

	_, err = backend.SubmitEntries(ctx, entries, "device1")
	require.NoError(t, err)

	// All entries should be stored
	allEntries, err := backend.Bootstrap(ctx, userId, "device1")
	require.NoError(t, err)

	foundIds := make(map[string]bool)
	for _, e := range allEntries {
		foundIds[e.EncryptedId] = true
	}

	assert.True(t, foundIds["ts-1-"+baseId], "Should have ts-1")
	assert.True(t, foundIds["ts-2-"+baseId], "Should have ts-2")
	assert.True(t, foundIds["ts-3-"+baseId], "Should have ts-3")
}

// TestS3Integration_BatchDeletionOver1000Entries tests batch deletion with more than 1000 entries
// to verify the deleteObjects batching logic works correctly with S3's 1000 object limit
func TestS3Integration_BatchDeletionOver1000Entries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large batch deletion test in short mode")
	}

	ctx := context.Background()
	userId := "test-user-batchdel-" + uuid.New().String()[:8]
	backend := newTestBackend(t, userId)

	err := backend.RegisterDevice(ctx, userId, "device1")
	require.NoError(t, err)

	// Create 1500 entries - more than the 1000 batch limit
	numEntries := 1500
	baseId := uuid.New().String()[:8]
	timestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	t.Logf("Creating %d entries...", numEntries)
	entries := make([]*shared.EncHistoryEntry, numEntries)
	for i := 0; i < numEntries; i++ {
		entries[i] = &shared.EncHistoryEntry{
			EncryptedId:   "batch-" + baseId + "-" + string(rune('A'+i/1000)) + "-" + itoa(i),
			Date:          timestamp,
			DeviceId:      "device1",
			UserId:        userId,
			EncryptedData: []byte("data"),
		}
	}

	// Submit entries in batches to avoid timeout
	batchSize := 100
	for i := 0; i < numEntries; i += batchSize {
		end := i + batchSize
		if end > numEntries {
			end = numEntries
		}
		_, err := backend.SubmitEntries(ctx, entries[i:end], "device1")
		require.NoError(t, err, "Failed to submit batch starting at %d", i)
	}

	// Verify all entries exist
	allEntries, err := backend.Bootstrap(ctx, userId, "device1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(allEntries), numEntries, "Should have at least %d entries", numEntries)

	// Delete half the entries (750) - this will require multiple batches internally
	idsToDelete := make([]shared.MessageIdentifier, 0, numEntries/2)
	for i := 0; i < numEntries; i += 2 {
		idsToDelete = append(idsToDelete, shared.MessageIdentifier{
			EntryId: "batch-" + baseId + "-" + string(rune('A'+i/1000)) + "-" + itoa(i),
		})
	}

	t.Logf("Deleting %d entries...", len(idsToDelete))
	delReq := shared.DeletionRequest{
		UserId:   userId,
		Messages: shared.MessageIdentifiers{Ids: idsToDelete},
	}

	err = backend.AddDeletionRequest(ctx, delReq)
	require.NoError(t, err, "AddDeletionRequest should succeed with >1000 entries")

	// Verify correct entries were deleted
	remainingEntries, err := backend.Bootstrap(ctx, userId, "device1")
	require.NoError(t, err)

	// Count entries that should remain (odd indices)
	remainingIds := make(map[string]bool)
	for _, e := range remainingEntries {
		remainingIds[e.EncryptedId] = true
	}

	// Verify deleted entries are gone
	deletedCount := 0
	remainingCount := 0
	for i := 0; i < numEntries; i++ {
		entryId := "batch-" + baseId + "-" + string(rune('A'+i/1000)) + "-" + itoa(i)
		if i%2 == 0 {
			// Should be deleted
			if !remainingIds[entryId] {
				deletedCount++
			}
		} else {
			// Should remain
			if remainingIds[entryId] {
				remainingCount++
			}
		}
	}

	t.Logf("Verified: %d entries deleted, %d entries remain", deletedCount, remainingCount)
	assert.Equal(t, numEntries/2, deletedCount, "Half the entries should be deleted")
	assert.Equal(t, numEntries/2, remainingCount, "Half the entries should remain")
}

// itoa is a simple int to string converter to avoid importing strconv
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var result []byte
	for i > 0 {
		result = append([]byte{byte('0' + i%10)}, result...)
		i /= 10
	}
	return string(result)
}
