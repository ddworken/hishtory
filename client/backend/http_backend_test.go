package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPBackendType(t *testing.T) {
	b := NewHTTPBackend()
	assert.Equal(t, "http", b.Type())
}

func TestHTTPBackendRegisterDevice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/register", r.URL.Path)
		assert.Equal(t, "user123", r.URL.Query().Get("user_id"))
		assert.Equal(t, "device456", r.URL.Query().Get("device_id"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	err := b.RegisterDevice(context.Background(), "user123", "device456")
	require.NoError(t, err)
}

func TestHTTPBackendBootstrap(t *testing.T) {
	entries := []*shared.EncHistoryEntry{
		{
			EncryptedData: []byte("encrypted1"),
			Nonce:         []byte("nonce1"),
			DeviceId:      "device1",
			UserId:        "user1",
			Date:          time.Now(),
			EncryptedId:   "id1",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/bootstrap", r.URL.Path)
		assert.Equal(t, "user123", r.URL.Query().Get("user_id"))
		assert.Equal(t, "device456", r.URL.Query().Get("device_id"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	result, err := b.Bootstrap(context.Background(), "user123", "device456")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "id1", result[0].EncryptedId)
}

func TestHTTPBackendSubmitEntries(t *testing.T) {
	entries := []*shared.EncHistoryEntry{
		{
			EncryptedData: []byte("encrypted1"),
			Nonce:         []byte("nonce1"),
			DeviceId:      "device1",
			UserId:        "user1",
			Date:          time.Now(),
			EncryptedId:   "id1",
		},
	}

	expectedResp := shared.SubmitResponse{
		DumpRequests: []*shared.DumpRequest{
			{UserId: "user1", RequestingDeviceId: "device2"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/submit", r.URL.Path)
		assert.Equal(t, "device1", r.URL.Query().Get("source_device_id"))
		assert.Equal(t, "POST", r.Method)

		var received []*shared.EncHistoryEntry
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		assert.Len(t, received, 1)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedResp)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	resp, err := b.SubmitEntries(context.Background(), entries, "device1")
	require.NoError(t, err)
	assert.Len(t, resp.DumpRequests, 1)
	assert.Equal(t, "device2", resp.DumpRequests[0].RequestingDeviceId)
}

func TestHTTPBackendSubmitEntriesEmpty(t *testing.T) {
	b := NewHTTPBackend()
	resp, err := b.SubmitEntries(context.Background(), []*shared.EncHistoryEntry{}, "device1")
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.DumpRequests)
	assert.Empty(t, resp.DeletionRequests)
}

func TestHTTPBackendSubmitDump(t *testing.T) {
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "id1"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/submit-dump", r.URL.Path)
		assert.Equal(t, "user1", r.URL.Query().Get("user_id"))
		assert.Equal(t, "device2", r.URL.Query().Get("requesting_device_id"))
		assert.Equal(t, "device1", r.URL.Query().Get("source_device_id"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	err := b.SubmitDump(context.Background(), entries, "user1", "device2", "device1")
	require.NoError(t, err)
}

func TestHTTPBackendQueryEntries(t *testing.T) {
	entries := []*shared.EncHistoryEntry{
		{EncryptedId: "id1"},
		{EncryptedId: "id2"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query", r.URL.Path)
		assert.Equal(t, "device1", r.URL.Query().Get("device_id"))
		assert.Equal(t, "user1", r.URL.Query().Get("user_id"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	result, err := b.QueryEntries(context.Background(), "device1", "user1")
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestHTTPBackendGetDeletionRequests(t *testing.T) {
	requests := []*shared.DeletionRequest{
		{UserId: "user1", DestinationDeviceId: "device1"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/get-deletion-requests", r.URL.Path)
		assert.Equal(t, "user1", r.URL.Query().Get("user_id"))
		assert.Equal(t, "device1", r.URL.Query().Get("device_id"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(requests)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	result, err := b.GetDeletionRequests(context.Background(), "user1", "device1")
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestHTTPBackendAddDeletionRequest(t *testing.T) {
	request := shared.DeletionRequest{
		UserId: "user1",
		Messages: shared.MessageIdentifiers{
			Ids: []shared.MessageIdentifier{{EntryId: "entry1"}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/add-deletion-request", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var received shared.DeletionRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		assert.Equal(t, "user1", received.UserId)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	err := b.AddDeletionRequest(context.Background(), request)
	require.NoError(t, err)
}

func TestHTTPBackendUninstall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/uninstall", r.URL.Path)
		assert.Equal(t, "user1", r.URL.Query().Get("user_id"))
		assert.Equal(t, "device1", r.URL.Query().Get("device_id"))
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	err := b.Uninstall(context.Background(), "user1", "device1")
	require.NoError(t, err)
}

func TestHTTPBackendPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/ping", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))
	err := b.Ping(context.Background())
	require.NoError(t, err)
}

func TestHTTPBackendErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	b := NewHTTPBackend(WithServerURL(server.URL))

	t.Run("RegisterDevice error", func(t *testing.T) {
		err := b.RegisterDevice(context.Background(), "user1", "device1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status_code=500")
	})

	t.Run("Bootstrap error", func(t *testing.T) {
		_, err := b.Bootstrap(context.Background(), "user1", "device1")
		assert.Error(t, err)
	})

	t.Run("QueryEntries error", func(t *testing.T) {
		_, err := b.QueryEntries(context.Background(), "device1", "user1")
		assert.Error(t, err)
	})

	t.Run("Ping error", func(t *testing.T) {
		err := b.Ping(context.Background())
		assert.Error(t, err)
	})
}

func TestHTTPBackendHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := NewHTTPBackend(
		WithServerURL(server.URL),
		WithVersion("123"),
		WithHeadersCallback(func() (string, string) {
			return "test-device", "test-user"
		}),
	)

	err := b.Ping(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "v0.123", receivedHeaders.Get("X-Hishtory-Version"))
	assert.Equal(t, "test-device", receivedHeaders.Get("X-Hishtory-Device-Id"))
	assert.Equal(t, "test-user", receivedHeaders.Get("X-Hishtory-User-Id"))
}

func TestHTTPBackendWithCustomClient(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	b := NewHTTPBackend(WithHTTPClient(customClient))
	assert.Equal(t, customClient, b.client)
}

func TestGetServerHostname(t *testing.T) {
	// Default hostname
	hostname := getServerHostname()
	assert.Equal(t, DefaultServerHostname, hostname)

	// Custom hostname via env
	t.Setenv("HISHTORY_SERVER", "http://custom.server")
	hostname = getServerHostname()
	assert.Equal(t, "http://custom.server", hostname)
}
