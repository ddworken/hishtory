package server

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/ddworken/hishtory/shared"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func (s *Server) apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var entries []*shared.EncHistoryEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiSubmitHandler: received request containg %d EncHistoryEntry\n", len(entries))
	if len(entries) == 0 {
		return
	}

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, entries[0].UserId, entries[0].DeviceId, len(entries), false))

	devices, err := s.db.DevicesForUser(r.Context(), entries[0].UserId)
	checkGormError(err)

	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId))
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))

	err = s.db.AddHistoryEntriesForAllDevices(r.Context(), devices, entries)
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add entries to DB: %w", err))
	}
	if s.statsd != nil {
		s.statsd.Count("hishtory.submit", int64(len(devices)), []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, false))
	historyEntries, err := s.db.AllHistoryEntriesForUser(r.Context(), userId)
	checkGormError(err)
	fmt.Printf("apiBootstrapHandler: Found %d entries\n", len(historyEntries))
	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}
}

func (s *Server) apiQueryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, true))

	// Delete any entries that match a pending deletion request
	deletionRequests, err := s.db.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err)
	for _, request := range deletionRequests {
		_, err := s.db.ApplyDeletionRequestsToBackend(r.Context(), request)
		checkGormError(err)
	}

	// Then retrieve
	historyEntries, err := s.db.HistoryEntriesForDevice(r.Context(), deviceId, 5)
	checkGormError(err)
	fmt.Printf("apiQueryHandler: Found %d entries for %s\n", len(historyEntries), r.URL)
	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}

	// And finally, kick off a background goroutine that will increment the read count. Doing it in the background avoids
	// blocking the entire response. This does have a potential race condition, but that is fine.
	if s.isProductionEnvironment {
		go func() {
			span, ctx := tracer.StartSpanFromContext(ctx, "apiQueryHandler.incrementReadCount")
			err := s.db.IncrementEntryReadCountsForDevice(ctx, deviceId)
			span.Finish(tracer.WithError(err))
		}()
	} else {
		err := s.db.IncrementEntryReadCountsForDevice(ctx, deviceId)
		if err != nil {
			panic("failed to increment read counts")
		}
	}

	if s.statsd != nil {
		s.statsd.Incr("hishtory.query", []string{}, 1.0)
	}
}

func (s *Server) apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	srcDeviceId := getRequiredQueryParam(r, "source_device_id")
	requestingDeviceId := getRequiredQueryParam(r, "requesting_device_id")
	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var entries []*shared.EncHistoryEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiSubmitDumpHandler: received request containg %d EncHistoryEntry\n", len(entries))

	// sanity check
	for _, entry := range entries {
		entry.DeviceId = requestingDeviceId
		if entry.UserId != userId {
			panic(fmt.Errorf("batch contains an entry with UserId=%#v, when the query param contained the user_id=%#v", entry.UserId, userId))
		}
	}

	err = s.db.AddHistoryEntries(r.Context(), entries...)
	checkGormError(err)
	err = s.db.DumpRequestDeleteForUserAndDevice(r.Context(), userId, requestingDeviceId)
	checkGormError(err)

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, userId, srcDeviceId, len(entries), false))

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiBannerHandler(w http.ResponseWriter, r *http.Request) {
	commitHash := getRequiredQueryParam(r, "commit_hash")
	deviceId := getRequiredQueryParam(r, "device_id")
	forcedBanner := r.URL.Query().Get("forced_banner")
	fmt.Printf("apiBannerHandler: commit_hash=%#v, device_id=%#v, forced_banner=%#v\n", commitHash, deviceId, forcedBanner)
	if getHishtoryVersion(r) == "v0.160" {
		w.Write([]byte("Warning: hiSHtory v0.160 has a bug that slows down your shell! Please run `hishtory update` to upgrade hiSHtory."))
		return
	}
	w.Write([]byte(html.EscapeString(forcedBanner)))
}

func (s *Server) apiGetPendingDumpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	dumpRequests, err := s.db.DumpRequestForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err)

	if err := json.NewEncoder(w).Encode(dumpRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %w", err))
	}
}

func (s *Server) apiDownloadHandler(w http.ResponseWriter, r *http.Request) {
	err := json.NewEncoder(w).Encode(s.updateInfo)

	if err != nil {
		panic(fmt.Errorf("failed to JSON marshall the update info: %w", err))
	}
}

func (s *Server) apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
	if getMaximumNumberOfAllowedUsers() < math.MaxInt {
		numDistinctUsers, err := s.db.DistinctUsers(r.Context())
		if err != nil {
			panic(fmt.Errorf("db.DistinctUsers: %w", err))
		}
		if numDistinctUsers >= int64(getMaximumNumberOfAllowedUsers()) {
			panic(fmt.Sprintf("Refusing to allow registration of new device since there are currently %d users and this server allows a max of %d users", numDistinctUsers, getMaximumNumberOfAllowedUsers()))
		}
	}
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	existingDevicesCount, err := s.db.CountDevicesForUser(r.Context(), userId)
	checkGormError(err)
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	if err := s.db.CreateDevice(r.Context(), &shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now()}); err != nil {
		checkGormError(err)
	}

	if existingDevicesCount > 0 {
		err := s.db.DumpRequestCreate(r.Context(), &shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()})
		checkGormError(err)
	}

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, false))

	if s.statsd != nil {
		s.statsd.Incr("hishtory.register", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}
