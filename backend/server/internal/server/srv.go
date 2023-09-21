package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/backend/server/internal/database"
	"github.com/ddworken/hishtory/shared"
	"github.com/rodaine/table"
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
)

type Server struct {
	db     *database.DB
	statsd *statsd.Client

	isProductionEnvironment bool
	isTestEnvironment       bool
	trackUsageData          bool
	releaseVersion          string
	cronFn                  CronFn
	updateInfo              shared.UpdateInfo
}

type CronFn func(ctx context.Context, db *database.DB, stats *statsd.Client) error
type Option func(*Server)

func WithStatsd(statsd *statsd.Client) Option {
	return func(s *Server) {
		s.statsd = statsd
	}
}

func WithReleaseVersion(releaseVersion string) Option {
	return func(s *Server) {
		s.releaseVersion = releaseVersion
	}
}

func WithCron(cronFn CronFn) Option {
	return func(s *Server) {
		s.cronFn = cronFn
	}
}

func WithUpdateInfo(updateInfo shared.UpdateInfo) Option {
	return func(s *Server) {
		s.updateInfo = updateInfo
	}
}

func IsProductionEnvironment(v bool) Option {
	return func(s *Server) {
		s.isProductionEnvironment = v
	}
}

func IsTestEnvironment(v bool) Option {
	return func(s *Server) {
		s.isTestEnvironment = v
	}
}

func TrackUsageData(v bool) Option {
	return func(s *Server) {
		s.trackUsageData = v
	}
}

func NewServer(db *database.DB, options ...Option) *Server {
	srv := Server{db: db}
	for _, option := range options {
		option(&srv)
	}
	if srv.isProductionEnvironment && srv.isTestEnvironment {
		panic(fmt.Errorf("cannot create a server that is both a prod environment and a test environment: %#v", srv))
	}
	return &srv
}

func (s *Server) Run(ctx context.Context, addr string) error {
	mux := httptrace.NewServeMux()

	if s.isProductionEnvironment {
		defer configureObservability(mux, s.releaseVersion)()
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
	if s.isTestEnvironment {
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

func (s *Server) UpdateReleaseVersion(v string, updateInfo shared.UpdateInfo) {
	s.releaseVersion = v
	s.updateInfo = updateInfo
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
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %w", err))
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

func (s *Server) triggerCronHandler(w http.ResponseWriter, r *http.Request) {
	err := s.cronFn(r.Context(), s.db, s.statsd)
	if err != nil {
		panic(err)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) slsaStatusHandler(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if s.isProductionEnvironment {
		encHistoryEntryCount, err := s.db.CountApproximateHistoryEntries(r.Context())
		checkGormError(err)
		if encHistoryEntryCount < 1000 {
			panic("Suspiciously few enc history entries!")
		}

		deviceCount, err := s.db.CountAllDevices(r.Context())
		checkGormError(err)
		if deviceCount < 100 {
			panic("Suspiciously few devices!")
		}
		// Check that we can write to the DB. This entry will get written and then eventually cleaned by the cron.
		err = s.db.AddHistoryEntries(r.Context(), &shared.EncHistoryEntry{
			EncryptedData: []byte("data"),
			Nonce:         []byte("nonce"),
			DeviceId:      "healthcheck_device_id",
			UserId:        "healthcheck_user_id",
			Date:          time.Now(),
			EncryptedId:   "healthcheck_enc_id",
			ReadCount:     10000,
		})
		checkGormError(err)
	} else {
		err := s.db.Ping()
		if err != nil {
			panic(fmt.Errorf("failed to ping DB: %w", err))
		}
	}
	w.Write([]byte("OK"))
}

func (s *Server) usageStatsHandler(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	numDevices, err := s.db.CountAllDevices(r.Context())
	checkGormError(err)

	numEntriesProcessed, err := s.db.UsageDataTotal(r.Context())
	checkGormError(err)

	numDbEntries, err := s.db.CountApproximateHistoryEntries(r.Context())
	checkGormError(err)

	oneWeek := time.Hour * 24 * 7
	weeklyActiveInstalls, err := s.db.CountActiveInstalls(r.Context(), oneWeek)
	checkGormError(err)

	weeklyQueryUsers, err := s.db.CountQueryUsers(r.Context(), oneWeek)
	checkGormError(err)

	lastRegistration, err := s.db.DateOfLastRegistration(r.Context())
	checkGormError(err)

	_, _ = fmt.Fprintf(w, "Num devices: %d\n", numDevices)
	_, _ = fmt.Fprintf(w, "Num history entries processed: %d\n", numEntriesProcessed)
	_, _ = fmt.Fprintf(w, "Num DB entries: %d\n", numDbEntries)
	_, _ = fmt.Fprintf(w, "Weekly active installs: %d\n", weeklyActiveInstalls)
	_, _ = fmt.Fprintf(w, "Weekly active queries: %d\n", weeklyQueryUsers)
	_, _ = fmt.Fprintf(w, "Last registration: %s\n", lastRegistration)
}

func (s *Server) wipeDbEntriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Host == "api.hishtory.dev" || s.isProductionEnvironment {
		panic("refusing to wipe the DB for prod")
	}
	if !s.isTestEnvironment {
		panic("refusing to wipe the DB non-test environment")
	}

	err := s.db.Unsafe_DeleteAllHistoryEntries(r.Context())
	checkGormError(err)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) getNumConnectionsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintf(w, "%#v", stats.OpenConnections)
}

func (s *Server) handleNonCriticalError(err error) {
	if err != nil {
		if s.isProductionEnvironment {
			fmt.Printf("Unexpected non-critical error: %v", err)
		} else {
			panic(fmt.Errorf("unexpected non-critical error: %w", err))
		}
	}
}

func (s *Server) updateUsageData(ctx context.Context, version string, remoteAddr string, userId, deviceId string, numEntriesHandled int, isQuery bool) error {
	if !s.trackUsageData {
		return nil
	}
	var usageData []shared.UsageData
	usageData, err := s.db.UsageDataFindByUserAndDevice(ctx, userId, deviceId)
	if err != nil {
		return fmt.Errorf("db.UsageDataFindByUserAndDevice: %w", err)
	}
	if len(usageData) == 0 {
		err := s.db.CreateUsageData(
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

		if err := s.db.UpdateUsageData(ctx, userId, deviceId, time.Now(), remoteAddr); err != nil {
			return fmt.Errorf("db.UsageDataUpdate: %w", err)
		}
		if numEntriesHandled > 0 {
			if err := s.db.UpdateUsageDataForNumEntriesHandled(ctx, userId, deviceId, numEntriesHandled); err != nil {
				return fmt.Errorf("db.UsageDataUpdateNumEntriesHandled: %w", err)
			}
		}
		if usage.Version != version {
			if err := s.db.UpdateUsageDataClientVersion(ctx, userId, deviceId, version); err != nil {
				return fmt.Errorf("db.UsageDataUpdateVersion: %w", err)
			}
		}
	}
	if isQuery {
		if err := s.db.UpdateUsageDataNumberQueries(ctx, userId, deviceId); err != nil {
			return fmt.Errorf("db.UsageDataUpdateNumQueries: %w", err)
		}
	}

	return nil
}
