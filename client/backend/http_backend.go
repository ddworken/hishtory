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

	"github.com/ddworken/hishtory/shared"
)

const DefaultServerHostname = "https://api.hishtory.dev"

// HTTPBackend implements SyncBackend by making HTTP requests to the hishtory server.
type HTTPBackend struct {
	serverURL  string
	client     *http.Client
	version    string
	getHeaders func() (deviceId, userId string) // callback to get auth headers
}

// HTTPBackendOption is a functional option for configuring HTTPBackend
type HTTPBackendOption func(*HTTPBackend)

// WithServerURL sets a custom server URL
func WithServerURL(url string) HTTPBackendOption {
	return func(b *HTTPBackend) {
		b.serverURL = url
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) HTTPBackendOption {
	return func(b *HTTPBackend) {
		b.client = client
	}
}

// WithVersion sets the client version for headers
func WithVersion(version string) HTTPBackendOption {
	return func(b *HTTPBackend) {
		b.version = version
	}
}

// WithHeadersCallback sets a callback to get deviceId and userId for request headers
func WithHeadersCallback(fn func() (deviceId, userId string)) HTTPBackendOption {
	return func(b *HTTPBackend) {
		b.getHeaders = fn
	}
}

// NewHTTPBackend creates a new HTTP backend with the given options.
func NewHTTPBackend(opts ...HTTPBackendOption) *HTTPBackend {
	b := &HTTPBackend{
		serverURL: getServerHostname(),
		client:    &http.Client{Timeout: 30 * time.Second},
		version:   "Unknown",
	}

	for _, opt := range opts {
		opt(b)
	}

	return b
}

func getServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return DefaultServerHostname
}

// Type returns "http" to identify this backend type.
func (b *HTTPBackend) Type() string {
	return string(BackendTypeHTTP)
}

// RegisterDevice registers a new device with the hishtory server.
func (b *HTTPBackend) RegisterDevice(ctx context.Context, userId, deviceId string) error {
	path := "/api/v1/register?user_id=" + userId + "&device_id=" + deviceId
	_, err := b.apiGet(ctx, path)
	return err
}

// Bootstrap retrieves all history entries for a user during initial device setup.
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

// SubmitEntries submits new encrypted history entries to the server.
func (b *HTTPBackend) SubmitEntries(ctx context.Context, entries []*shared.EncHistoryEntry, sourceDeviceId string) (*shared.SubmitResponse, error) {
	if len(entries) == 0 {
		return &shared.SubmitResponse{}, nil
	}

	jsonValue, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entries: %w", err)
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

// SubmitDump handles bulk transfer of entries to a requesting device.
func (b *HTTPBackend) SubmitDump(ctx context.Context, entries []*shared.EncHistoryEntry, userId, requestingDeviceId, sourceDeviceId string) error {
	jsonValue, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal entries: %w", err)
	}

	path := "/api/v1/submit-dump?user_id=" + userId +
		"&requesting_device_id=" + requestingDeviceId +
		"&source_device_id=" + sourceDeviceId
	_, err = b.apiPost(ctx, path, "application/json", jsonValue)
	return err
}

// QueryEntries retrieves new entries for a specific device.
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

// GetDeletionRequests retrieves pending deletion requests for a device.
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

// AddDeletionRequest adds a deletion request to be propagated to all devices.
func (b *HTTPBackend) AddDeletionRequest(ctx context.Context, request shared.DeletionRequest) error {
	jsonValue, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal deletion request: %w", err)
	}
	_, err = b.apiPost(ctx, "/api/v1/add-deletion-request", "application/json", jsonValue)
	return err
}

// Uninstall removes a device from the server.
func (b *HTTPBackend) Uninstall(ctx context.Context, userId, deviceId string) error {
	path := "/api/v1/uninstall?user_id=" + userId + "&device_id=" + deviceId
	_, err := b.apiPost(ctx, path, "application/json", []byte{})
	return err
}

// Ping checks if the server is reachable.
func (b *HTTPBackend) Ping(ctx context.Context) error {
	_, err := b.apiGet(ctx, "/api/v1/ping")
	return err
}

// apiGet performs a GET request to the server.
func (b *HTTPBackend) apiGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.serverURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %w", err)
	}

	b.setHeaders(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to GET %s%s: %w", b.serverURL, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to GET %s%s: status_code=%d", b.serverURL, path, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// apiPost performs a POST request to the server.
func (b *HTTPBackend) apiPost(ctx context.Context, path, contentType string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", b.serverURL+path, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	b.setHeaders(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to POST %s%s: %w", b.serverURL, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to POST %s%s: status_code=%d", b.serverURL, path, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// setHeaders sets common headers on the request.
func (b *HTTPBackend) setHeaders(req *http.Request) {
	req.Header.Set("X-Hishtory-Version", "v0."+b.version)

	if b.getHeaders != nil {
		deviceId, userId := b.getHeaders()
		if deviceId != "" {
			req.Header.Set("X-Hishtory-Device-Id", deviceId)
		}
		if userId != "" {
			req.Header.Set("X-Hishtory-User-Id", userId)
		}
	}
}
