package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ddworken/hishtory/backend/server/internal/database"
	"github.com/ddworken/hishtory/shared"

	"github.com/DataDog/datadog-go/statsd"
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

type (
	CronFn func(ctx context.Context, db *database.DB, stats *statsd.Client) error
	Option func(*Server)
)

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
				fmt.Println("failed to deep clean: %w", err)
			}
		}()
	}
	middlewares := mergeMiddlewares(
		withPanicGuard(s.statsd),
		withLogging(s.statsd, os.Stdout),
	)

	mux.Handle("/api/v1/submit", middlewares(http.HandlerFunc(s.apiSubmitHandler)))
	mux.Handle("/api/v1/get-dump-requests", middlewares(http.HandlerFunc(s.apiGetPendingDumpRequestsHandler)))
	mux.Handle("/api/v1/submit-dump", middlewares(http.HandlerFunc(s.apiSubmitDumpHandler)))
	mux.Handle("/api/v1/query", middlewares(http.HandlerFunc(s.apiQueryHandler)))
	mux.Handle("/api/v1/bootstrap", middlewares(http.HandlerFunc(s.apiBootstrapHandler)))
	mux.Handle("/api/v1/register", middlewares(http.HandlerFunc(s.apiRegisterHandler)))
	mux.Handle("/api/v1/banner", middlewares(http.HandlerFunc(s.apiBannerHandler)))
	mux.Handle("/api/v1/download", middlewares(http.HandlerFunc(s.apiDownloadHandler)))
	mux.Handle("/api/v1/trigger-cron", middlewares(http.HandlerFunc(s.triggerCronHandler)))
	mux.Handle("/api/v1/get-deletion-requests", middlewares(http.HandlerFunc(s.getDeletionRequestsHandler)))
	mux.Handle("/api/v1/add-deletion-request", middlewares(http.HandlerFunc(s.addDeletionRequestHandler)))
	mux.Handle("/api/v1/slsa-status", middlewares(http.HandlerFunc(s.slsaStatusHandler)))
	mux.Handle("/api/v1/feedback", middlewares(http.HandlerFunc(s.feedbackHandler)))
	mux.Handle("/api/v1/uninstall", middlewares(http.HandlerFunc(s.apiUninstallHandler)))
	mux.Handle("/api/v1/ai-suggest", middlewares(http.HandlerFunc(s.aiSuggestionHandler)))
	mux.Handle("/api/v1/ping", middlewares(http.HandlerFunc(s.pingHandler)))
	mux.Handle("/healthcheck", middlewares(http.HandlerFunc(s.healthCheckHandler)))
	mux.Handle("/internal/api/v1/usage-stats", middlewares(http.HandlerFunc(s.usageStatsHandler)))
	mux.Handle("/internal/api/v1/stats", middlewares(http.HandlerFunc(s.statsHandler)))
	if s.isTestEnvironment {
		mux.Handle("/api/v1/ai-suggest-override", middlewares(http.HandlerFunc(s.testOnlyOverrideAiSuggestions)))
		mux.Handle("/api/v1/wipe-db-entries", middlewares(http.HandlerFunc(s.wipeDbEntriesHandler)))
		mux.Handle("/api/v1/get-num-connections", middlewares(http.HandlerFunc(s.getNumConnectionsHandler)))
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

func (s *Server) handleNonCriticalError(err error) {
	if err != nil {
		if s.isProductionEnvironment {
			fmt.Printf("Unexpected non-critical error: %v", err)
		} else {
			panic(fmt.Errorf("unexpected non-critical error: %w", err))
		}
	}
}

func (s *Server) updateUsageData(ctx context.Context, version, remoteAddr, userId, deviceId string, numEntriesHandled int, isQuery bool) error {
	if !s.trackUsageData {
		return nil
	}
	var usageData []database.UsageData
	usageData, err := s.db.UsageDataFindByUserAndDevice(ctx, userId, deviceId)
	if err != nil && !strings.Contains(err.Error(), "record not found") {
		return fmt.Errorf("db.UsageDataFindByUserAndDevice: %w", err)
	}
	if len(usageData) == 0 {
		err := s.db.CreateUsageData(
			ctx,
			&database.UsageData{
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
