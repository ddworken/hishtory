package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"time"

	"github.com/ddworken/hishtory/backend/server/internal/database"
	"github.com/ddworken/hishtory/shared"
	"github.com/ddworken/hishtory/shared/ai"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func (s *Server) apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	var entries []*shared.EncHistoryEntry
	err := json.NewDecoder(r.Body).Decode(&entries)
	if err != nil {
		panic(fmt.Errorf("failed to decode: %w", err))
	}
	fmt.Printf("apiSubmitHandler: received request containg %d EncHistoryEntry\n", len(entries))
	if len(entries) == 0 {
		return
	}
	userId := entries[0].UserId

	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)
	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, entries[0].UserId, entries[0].DeviceId, len(entries), false))

	devices, err := s.db.DevicesForUser(r.Context(), entries[0].UserId)
	checkGormError(err)

	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId))
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))

	sourceDeviceId := getOptionalQueryParam(r, "source_device_id", s.isTestEnvironment)
	err = s.db.AddHistoryEntriesForAllDevices(r.Context(), sourceDeviceId, devices, entries)
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add entries to DB: %w", err))
	}
	if s.statsd != nil {
		s.statsd.Count("hishtory.submit", int64(len(devices)), []string{}, 1.0)
	}

	resp := shared.SubmitResponse{}

	if sourceDeviceId != "" {
		hv, err := shared.ParseVersionString(version)
		if err != nil || hv.GreaterThan(shared.ParsedVersion{MinorVersion: 0, MajorVersion: 221}) {
			// Note that if we fail to parse the version string, we do return dump and deletion requests. This is necessary
			// since tests run with v0.Unknown which obviously fails to parse.
			dumpRequests, err := s.db.DumpRequestForUserAndDevice(r.Context(), userId, sourceDeviceId)
			checkGormError(err)
			resp.DumpRequests = dumpRequests

			deletionRequests, err := s.db.DeletionRequestsForUserAndDevice(r.Context(), userId, sourceDeviceId)
			checkGormError(err)
			resp.DeletionRequests = deletionRequests

			checkGormError(s.db.DeletionRequestInc(r.Context(), userId, sourceDeviceId))
		}
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(err)
	}
}

func (s *Server) apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
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
	queryReason := getOptionalQueryParam(r, "queryReason", s.isTestEnvironment)
	isBackgroundQuery := queryReason == "preload" || queryReason == "newclient"
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	if !isBackgroundQuery {
		s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, true))
	}

	// Delete any entries that match a pending deletion request
	deletionRequests, err := s.db.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err)
	_, err = s.db.ApplyDeletionRequestsToBackend(r.Context(), deletionRequests)
	checkGormError(err)

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
			span, backgroundCtx := tracer.StartSpanFromContext(context.Background(), "apiQueryHandler.incrementReadCount")
			err := s.db.IncrementEntryReadCountsForDevice(backgroundCtx, deviceId)
			if err != nil {
				fmt.Printf("failed to increment read counts: %v\n", err)
			}
			span.Finish(tracer.WithError(err))
		}()
	} else {
		err := s.db.IncrementEntryReadCountsForDevice(ctx, deviceId)
		if err != nil {
			panic("failed to increment read counts")
		}
	}

	if s.statsd != nil {
		s.statsd.Incr("hishtory.query", []string{"query_reason:" + queryReason}, 1.0)
	}
}

func (s *Server) apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	srcDeviceId := getRequiredQueryParam(r, "source_device_id")
	requestingDeviceId := getRequiredQueryParam(r, "requesting_device_id")
	isChunk := getOptionalQueryParam(r, "is_chunk", s.isTestEnvironment) == "true"
	var entries []*shared.EncHistoryEntry
	err := json.NewDecoder(r.Body).Decode(&entries)
	if err != nil {
		panic(fmt.Errorf("failed to decode: %w", err))
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
	if !isChunk {
		err = s.db.DumpRequestDeleteForUserAndDevice(r.Context(), userId, requestingDeviceId)
		checkGormError(err)
	}

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
	dumpRequests, err := s.db.DumpRequestForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err)

	if err := json.NewEncoder(w).Encode(dumpRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshal the dump requests: %w", err))
	}
}

func (s *Server) apiDownloadHandler(w http.ResponseWriter, r *http.Request) {
	err := json.NewEncoder(w).Encode(s.updateInfo)
	if err != nil {
		panic(fmt.Errorf("failed to JSON marshal the update info: %w", err))
	}
}

func (s *Server) apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	isIntegrationTestDevice := getOptionalQueryParam(r, "is_integration_test_device", false) == "true"

	if getMaximumNumberOfAllowedUsers() < math.MaxInt {
		userAlreadyExist, err := s.db.UserAlreadyExist(r.Context(), userId)
		if err != nil {
			panic(fmt.Errorf("db.UserAlreadyExist: %w", err))
		}

		if !userAlreadyExist {
			numDistinctUsers, err := s.db.DistinctUsers(r.Context())
			if err != nil {
				panic(fmt.Errorf("db.DistinctUsers: %w", err))
			}
			if numDistinctUsers >= int64(getMaximumNumberOfAllowedUsers()) {
				panic(fmt.Sprintf("Refusing to allow registration of new device since there are currently %d users and this server allows a max of %d users", numDistinctUsers, getMaximumNumberOfAllowedUsers()))
			}
		}
	}

	existingDevicesCount, err := s.db.CountDevicesForUser(r.Context(), userId)
	checkGormError(err)
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	if err := s.db.CreateDevice(r.Context(), &database.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now(), IsIntegrationTestDevice: isIntegrationTestDevice}); err != nil {
		checkGormError(err)
	}

	if existingDevicesCount > 0 {
		err := s.db.DumpRequestCreate(r.Context(), &shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()})
		checkGormError(err)
	}

	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)
	s.handleNonCriticalError(s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, false))

	if s.statsd != nil && !isIntegrationTestDevice {
		s.statsd.Incr("hishtory.register", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) getDeletionRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// Increment the ReadCount
	err := s.db.DeletionRequestInc(r.Context(), userId, deviceId)
	checkGormError(err)

	// Return all the deletion requests
	deletionRequests, err := s.db.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err)
	if err := json.NewEncoder(w).Encode(deletionRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshal the dump requests: %w", err))
	}
}

func (s *Server) addDeletionRequestHandler(w http.ResponseWriter, r *http.Request) {
	var request shared.DeletionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		panic(fmt.Errorf("failed to decode: %w", err))
	}
	request.ReadCount = 0
	fmt.Printf("addDeletionRequestHandler: received request containg %d messages to be deleted\n", len(request.Messages.Ids))

	err := s.db.DeletionRequestCreate(r.Context(), &request)
	checkGormError(err)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) slsaStatusHandler(w http.ResponseWriter, r *http.Request) {
	// returns "OK" unless there is a current SLSA bug
	v := getHishtoryVersion(r)
	pv, err := shared.ParseVersionString(v)
	if err != nil {
		w.Write([]byte("OK"))
		return
	}
	if pv.LessThan(shared.ParsedVersion{MajorVersion: 0, MinorVersion: 159}) {
		w.Write([]byte("Sigstore deployed a broken change. See https://github.com/slsa-framework/slsa-github-generator/issues/1163"))
		return
	}
	if pv.LessThan(shared.ParsedVersion{MajorVersion: 0, MinorVersion: 286}) {
		w.Write([]byte("Sigstore deployed a broken change. See https://github.com/slsa-framework/slsa-github-generator/issues/1163"))
		return
	}
	if pv.LessThan(shared.ParsedVersion{MajorVersion: 0, MinorVersion: 329}) {
		w.Write([]byte("SLSA made a non-backwards compatible change. See https://github.com/ddworken/hishtory/issues/294"))
		return
	}
	w.Write([]byte("OK"))
}

func (s *Server) feedbackHandler(w http.ResponseWriter, r *http.Request) {
	var feedback shared.Feedback
	err := json.NewDecoder(r.Body).Decode(&feedback)
	if err != nil {
		panic(fmt.Errorf("failed to decode: %w", err))
	}
	fmt.Printf("feedbackHandler: received request containg feedback %#v\n", feedback)
	err = s.db.FeedbackCreate(r.Context(), &feedback)
	checkGormError(err)

	if s.statsd != nil {
		s.statsd.Incr("hishtory.uninstall", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) aiSuggestionHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req ai.AiSuggestionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		panic(fmt.Errorf("failed to decode AiSuggestionRequest: %w", err))
	}
	if req.NumberCompletions > 10 {
		panic(fmt.Errorf("request for %d completions is greater than max allowed", req.NumberCompletions))
	}
	numDevices, err := s.db.CountDevicesForUser(ctx, req.UserId)
	if err != nil {
		panic(fmt.Errorf("failed to count devices for user: %w", err))
	}
	if numDevices == 0 {
		panic(fmt.Errorf("rejecting OpenAI request for user_id=%#v since it does not exist", req.UserId))
	}
	suggestions, usage, err := ai.GetAiSuggestionsViaOpenAiApi(ai.DefaultOpenAiEndpoint, req.Query, req.ShellName, req.OsName, req.Model, req.NumberCompletions)
	if err != nil {
		panic(fmt.Errorf("failed to query OpenAI API: %w", err))
	}
	s.statsd.Incr("hishtory.openai.query", []string{}, float64(req.NumberCompletions))
	s.statsd.Incr("hishtory.openai.tokens", []string{}, float64(usage.TotalTokens))
	var resp ai.AiSuggestionResponse
	resp.Suggestions = suggestions
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(fmt.Errorf("failed to JSON marshal the API response: %w", err))
	}
}

func (s *Server) testOnlyOverrideAiSuggestions(w http.ResponseWriter, r *http.Request) {
	var req ai.TestOnlyOverrideAiSuggestionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		panic(fmt.Errorf("failed to decode TestOnlyOverrideAiSuggestionRequest: %w", err))
	}
	ai.TestOnlyOverrideAiSuggestions[req.Query] = req.Suggestions
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

func (s *Server) apiUninstallHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	numDeleted, err := s.db.UninstallDevice(r.Context(), userId, deviceId)
	if err != nil {
		panic(fmt.Errorf("failed to UninstallDevice(user_id=%s, device_id=%s): %w", userId, deviceId, err))
	}
	fmt.Printf("apiUninstallHandler: Deleted %d items from the DB\n", numDeleted)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}
