package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	pprofhttp "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/internal/database"
	"github.com/ddworken/hishtory/shared"
	"github.com/rodaine/table"
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"gopkg.in/DataDog/dd-trace-go.v1/profiler"
)

type Srv struct {
	db     *database.DB
	statsd *statsd.Client
}

type ServerOption func(*Srv)

func WithStatsd(statsd *statsd.Client) ServerOption {
	return func(s *Srv) {
		s.statsd = statsd
	}
}

func NewServer(db *database.DB, options ...ServerOption) *Srv {
	srv := Srv{db: db}
	for _, option := range options {
		option(&srv)
	}
	return &srv
}

func (s *Srv) Run(ctx context.Context, addr string) error {
	mux := httptrace.NewServeMux()

	if isProductionEnvironment() {
		defer configureObservability(mux)()
		go func() {
			if err := s.db.DeepClean(ctx); err != nil {
				panic(err)
			}
		}()
	}
	loggerMiddleware := withLogging(s.statsd)

	mux.Handle("/api/v1/submit", loggerMiddleware(s.apiSubmitHandler))
	mux.Handle("/api/v1/get-dump-requests", loggerMiddleware(s.apiGetPendingDumpRequestsHandler))
	mux.Handle("/api/v1/submit-dump", loggerMiddleware(s.apiSubmitDumpHandler))
	mux.Handle("/api/v1/query", loggerMiddleware(s.apiQueryHandler))
	mux.Handle("/api/v1/bootstrap", loggerMiddleware(s.apiBootstrapHandler))
	mux.Handle("/api/v1/register", loggerMiddleware(s.apiRegisterHandler))
	mux.Handle("/api/v1/banner", loggerMiddleware(s.apiBannerHandler))
	mux.Handle("/api/v1/download", loggerMiddleware(s.apiDownloadHandler))
	mux.Handle("/api/v1/trigger-cron", loggerMiddleware(s.triggerCronHandler))
	mux.Handle("/api/v1/get-deletion-requests", loggerMiddleware(s.getDeletionRequestsHandler))
	mux.Handle("/api/v1/add-deletion-request", loggerMiddleware(s.addDeletionRequestHandler))
	mux.Handle("/api/v1/slsa-status", loggerMiddleware(s.slsaStatusHandler))
	mux.Handle("/api/v1/feedback", loggerMiddleware(s.feedbackHandler))
	mux.Handle("/healthcheck", loggerMiddleware(s.healthCheckHandler))
	mux.Handle("/internal/api/v1/usage-stats", loggerMiddleware(s.usageStatsHandler))
	mux.Handle("/internal/api/v1/stats", loggerMiddleware(s.statsHandler))
	if isTestEnvironment() {
		mux.Handle("/api/v1/wipe-db-entries", loggerMiddleware(s.wipeDbEntriesHandler))
		mux.Handle("/api/v1/get-num-connections", loggerMiddleware(s.getNumConnectionsHandler))
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	fmt.Printf("Listening on %s\n", addr)
	if err := httpServer.ListenAndServe(); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http.ListenAndServe: %w", err)
		}
	}

	return nil
}

func (s *Srv) apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
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

	if err := s.updateUsageData(r.Context(), version, remoteIPAddr, entries[0].UserId, entries[0].DeviceId, len(entries), false); err != nil {
		fmt.Printf("updateUsageData: %v\n", err)
	}

	devices, err := s.db.DevicesForUser(r.Context(), entries[0].UserId)
	checkGormError(err, 0)

	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId))
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))

	err = s.db.DeviceEntriesCreateChunk(r.Context(), devices, entries, 1000)
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add entries to DB: %w", err))
	}
	if s.statsd != nil {
		s.statsd.Count("hishtory.submit", int64(len(devices)), []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Srv) apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	if err := s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, false); err != nil {
		fmt.Printf("updateUsageData: %v\n", err)
	}
	historyEntries, err := s.db.EncHistoryEntriesForUser(r.Context(), userId)
	checkGormError(err, 1)
	fmt.Printf("apiBootstrapHandler: Found %d entries\n", len(historyEntries))
	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}
}

func (s *Srv) apiQueryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	if err := s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, true); err != nil {
		fmt.Printf("updateUsageData: %v\n", err)
	}

	// Delete any entries that match a pending deletion request
	deletionRequests, err := s.db.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err, 0)
	for _, request := range deletionRequests {
		_, err := s.db.ApplyDeletionRequestsToBackend(r.Context(), request)
		checkGormError(err, 0)
	}

	// Then retrieve
	historyEntries, err := s.db.EncHistoryEntriesForDevice(r.Context(), deviceId, 5)
	checkGormError(err, 0)
	fmt.Printf("apiQueryHandler: Found %d entries for %s\n", len(historyEntries), r.URL)
	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}

	// And finally, kick off a background goroutine that will increment the read count. Doing it in the background avoids
	// blocking the entire response. This does have a potential race condition, but that is fine.
	if isProductionEnvironment() {
		go func() {
			span, ctx := tracer.StartSpanFromContext(ctx, "apiQueryHandler.incrementReadCount")
			err := s.db.DeviceIncrementReadCounts(ctx, deviceId)
			span.Finish(tracer.WithError(err))
		}()
	} else {
		err := s.db.DeviceIncrementReadCounts(ctx, deviceId)
		if err != nil {
			panic("failed to increment read counts")
		}
	}

	if s.statsd != nil {
		s.statsd.Incr("hishtory.query", []string{}, 1.0)
	}
}

func (s *Srv) apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
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

	err = s.db.EncHistoryCreateMulti(r.Context(), entries...)
	checkGormError(err, 0)
	err = s.db.DumpRequestDeleteForUserAndDevice(r.Context(), userId, requestingDeviceId)
	checkGormError(err, 0)

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	if err := s.updateUsageData(r.Context(), version, remoteIPAddr, userId, srcDeviceId, len(entries), false); err != nil {
		fmt.Printf("updateUsageData: %v\n", err)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Srv) apiBannerHandler(w http.ResponseWriter, r *http.Request) {
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

func (s *Srv) apiGetPendingDumpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	dumpRequests, err := s.db.DumpRequestForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err, 0)

	if err := json.NewEncoder(w).Encode(dumpRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %w", err))
	}
}

func (s *Srv) getDeletionRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// Increment the ReadCount
	err := s.db.DeletionRequestInc(r.Context(), userId, deviceId)
	checkGormError(err, 0)

	// Return all the deletion requests
	deletionRequests, err := s.db.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err, 0)
	if err := json.NewEncoder(w).Encode(deletionRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %w", err))
	}
}

func (s *Srv) addDeletionRequestHandler(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var request shared.DeletionRequest

	if err := json.Unmarshal(data, &request); err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	request.ReadCount = 0
	fmt.Printf("addDeletionRequestHandler: received request containg %d messages to be deleted\n", len(request.Messages.Ids))

	err = s.db.DeletionRequestCreate(r.Context(), &request)
	checkGormError(err, 0)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (_ *Srv) apiDownloadHandler(w http.ResponseWriter, r *http.Request) {
	updateInfo := buildUpdateInfo(ReleaseVersion)
	resp, err := json.Marshal(updateInfo)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func (s *Srv) apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
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

	existingDevicesCount, err := s.db.DevicesCountForUser(r.Context(), userId)
	checkGormError(err, 0)
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	if err := s.db.DeviceCreate(r.Context(), &shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now()}); err != nil {
		checkGormError(err, 0)
	}

	if existingDevicesCount > 0 {
		err := s.db.DumpRequestCreate(r.Context(), &shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()})
		checkGormError(err, 0)
	}

	// TODO: add these to the context in a middleware
	version := getHishtoryVersion(r)
	remoteIPAddr := getRemoteAddr(r)

	if err := s.updateUsageData(r.Context(), version, remoteIPAddr, userId, deviceId, 0, false); err != nil {
		fmt.Printf("updateUsageData: %v\n", err)
	}

	if s.statsd != nil {
		s.statsd.Incr("hishtory.register", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Srv) triggerCronHandler(w http.ResponseWriter, r *http.Request) {
	err := cron(r.Context())
	if err != nil {
		panic(err)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Srv) slsaStatusHandler(w http.ResponseWriter, r *http.Request) {
	// returns "OK" unless there is a current SLSA bug
	v := getHishtoryVersion(r)
	if !strings.Contains(v, "v0.") {
		w.Write([]byte("OK"))
		return
	}
	vNum, err := strconv.Atoi(strings.Split(v, ".")[1])
	if err != nil {
		w.Write([]byte("OK"))
		return
	}
	if vNum < 159 {
		w.Write([]byte("Sigstore deployed a broken change. See https://github.com/slsa-framework/slsa-github-generator/issues/1163"))
		return
	}
	w.Write([]byte("OK"))
}

func (s *Srv) feedbackHandler(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var feedback shared.Feedback
	err = json.Unmarshal(data, &feedback)
	if err != nil {
		panic(fmt.Sprintf("feedbackHandler: body=%#v, err=%v", data, err))
	}
	fmt.Printf("feedbackHandler: received request containg feedback %#v\n", feedback)
	err = s.db.FeedbackCreate(r.Context(), &feedback)
	checkGormError(err, 0)

	if s.statsd != nil {
		s.statsd.Incr("hishtory.uninstall", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Srv) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if isProductionEnvironment() {
		// Check that we have a reasonable looking set of devices/entries in the DB
		//rows, err := s.db.Raw("SELECT true FROM enc_history_entries LIMIT 1 OFFSET 1000").Rows()
		//if err != nil {
		//	panic(fmt.Sprintf("failed to count entries in DB: %v", err))
		//}
		//defer rows.Close()
		//if !rows.Next() {
		//	panic("Suspiciously few enc history entries!")
		//}
		encHistoryEntryCount, err := s.db.EncHistoryEntryCount(r.Context())
		checkGormError(err, 0)
		if encHistoryEntryCount < 1000 {
			panic("Suspiciously few enc history entries!")
		}

		deviceCount, err := s.db.DevicesCount(r.Context())
		checkGormError(err, 0)
		if deviceCount < 100 {
			panic("Suspiciously few devices!")
		}
		// Check that we can write to the DB. This entry will get written and then eventually cleaned by the cron.
		err = s.db.EncHistoryCreate(r.Context(), &shared.EncHistoryEntry{
			EncryptedData: []byte("data"),
			Nonce:         []byte("nonce"),
			DeviceId:      "healthcheck_device_id",
			UserId:        "healthcheck_user_id",
			Date:          time.Now(),
			EncryptedId:   "healthcheck_enc_id",
			ReadCount:     10000,
		})
		checkGormError(err, 0)
	} else {
		err := s.db.Ping()
		if err != nil {
			panic(fmt.Errorf("failed to ping DB: %w", err))
		}
	}
	w.Write([]byte("OK"))
}

func (s *Srv) usageStatsHandler(w http.ResponseWriter, r *http.Request) {
	usageData, err := s.db.UsageDataStats(r.Context())
	if err != nil {
		panic(fmt.Errorf("db.UsageDataStats: %w", err))
	}

	tbl := table.New("Registration Date", "Num Devices", "Num Entries", "Num Queries", "Last Active", "Last Query", "Versions", "IPs")
	tbl.WithWriter(w)
	for _, data := range usageData {
		versions := strings.ReplaceAll(strings.ReplaceAll(data.Versions, "Unknown", ""), ", ", "")
		lastQueryStr := strings.ReplaceAll(data.LastQueried.Format(shared.DateOnly), "1970-01-01", "")
		tbl.AddRow(
			data.RegistrationDate.Format(shared.DateOnly),
			data.NumDevices,
			data.NumEntries,
			data.NumQueries,
			data.LastUsedDate.Format(shared.DateOnly),
			lastQueryStr,
			versions,
			data.IpAddresses,
		)
	}
	tbl.Print()
}

func (s *Srv) statsHandler(w http.ResponseWriter, r *http.Request) {
	numDevices, err := s.db.DevicesCount(r.Context())
	checkGormError(err, 0)

	numEntriesProcessed, err := s.db.UsageDataTotal(r.Context())
	checkGormError(err, 0)

	numDbEntries, err := s.db.EncHistoryEntryCount(r.Context())
	checkGormError(err, 0)

	oneWeek := time.Hour * 24 * 7
	weeklyActiveInstalls, err := s.db.WeeklyActiveInstalls(r.Context(), oneWeek)
	checkGormError(err, 0)

	weeklyQueryUsers, err := s.db.WeeklyQueryUsers(r.Context(), oneWeek)
	checkGormError(err, 0)

	lastRegistration, err := s.db.LastRegistration(r.Context())
	checkGormError(err, 0)

	_, _ = fmt.Fprintf(w, "Num devices: %d\n", numDevices)
	_, _ = fmt.Fprintf(w, "Num history entries processed: %d\n", numEntriesProcessed)
	_, _ = fmt.Fprintf(w, "Num DB entries: %d\n", numDbEntries)
	_, _ = fmt.Fprintf(w, "Weekly active installs: %d\n", weeklyActiveInstalls)
	_, _ = fmt.Fprintf(w, "Weekly active queries: %d\n", weeklyQueryUsers)
	_, _ = fmt.Fprintf(w, "Last registration: %s\n", lastRegistration)
}

func (s *Srv) wipeDbEntriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Host == "api.hishtory.dev" || isProductionEnvironment() {
		panic("refusing to wipe the DB for prod")
	}
	if !isTestEnvironment() {
		panic("refusing to wipe the DB non-test environment")
	}

	err := s.db.EncHistoryClear(r.Context())
	checkGormError(err, 0)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Srv) getNumConnectionsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintf(w, "%#v", stats.OpenConnections)
}

func (s *Srv) updateUsageData(ctx context.Context, version string, remoteAddr string, userId, deviceId string, numEntriesHandled int, isQuery bool) error {
	var usageData []shared.UsageData
	usageData, err := s.db.UsageDataFindByUserAndDevice(ctx, userId, deviceId)
	if err != nil {
		return fmt.Errorf("db.UsageDataFindByUserAndDevice: %w", err)
	}
	if len(usageData) == 0 {
		err := s.db.UsageDataCreate(
			ctx,
			&shared.UsageData{
				UserId:            userId,
				DeviceId:          deviceId,
				LastUsed:          time.Now(),
				NumEntriesHandled: numEntriesHandled,
				Version:           version,
			},
		)
		if err != nil {
			return fmt.Errorf("db.UsageDataCreate: %w", err)
		}
	} else {
		usage := usageData[0]

		if err := s.db.UsageDataUpdate(ctx, userId, deviceId, time.Now(), remoteAddr); err != nil {
			return fmt.Errorf("db.UsageDataUpdate: %w", err)
		}
		if numEntriesHandled > 0 {
			if err := s.db.UsageDataUpdateNumEntriesHandled(ctx, userId, deviceId, numEntriesHandled); err != nil {
				return fmt.Errorf("db.UsageDataUpdateNumEntriesHandled: %w", err)
			}
		}
		if usage.Version != version {
			if err := s.db.UsageDataUpdateVersion(ctx, userId, deviceId, version); err != nil {
				return fmt.Errorf("db.UsageDataUpdateVersion: %w", err)
			}
		}
	}
	if isQuery {
		if err := s.db.UsageDataUpdateNumQueries(ctx, userId, deviceId); err != nil {
			return fmt.Errorf("db.UsageDataUpdateNumQueries: %w", err)
		}
	}

	return nil
}

func configureObservability(mux *httptrace.ServeMux) func() {
	// Profiler
	err := profiler.Start(
		profiler.WithService("hishtory-api"),
		profiler.WithVersion(ReleaseVersion),
		profiler.WithAPIKey(os.Getenv("DD_API_KEY")),
		profiler.WithUDS("/var/run/datadog/apm.socket"),
		profiler.WithProfileTypes(
			profiler.CPUProfile,
			profiler.HeapProfile,
		),
	)
	if err != nil {
		fmt.Printf("Failed to start DataDog profiler: %v\n", err)
	}
	// Tracer
	tracer.Start(
		tracer.WithRuntimeMetrics(),
		tracer.WithService("hishtory-api"),
		tracer.WithUDS("/var/run/datadog/apm.socket"),
	)
	// TODO: should this be here?
	defer tracer.Stop()

	// Pprof
	mux.HandleFunc("/debug/pprof/", pprofhttp.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprofhttp.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprofhttp.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprofhttp.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprofhttp.Trace)

	// Func to stop all of the above
	return func() {
		profiler.Stop()
		tracer.Stop()
	}
}
