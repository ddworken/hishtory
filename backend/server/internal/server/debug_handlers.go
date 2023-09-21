package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ddworken/hishtory/shared"

	"github.com/rodaine/table"
)

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

func (s *Server) triggerCronHandler(w http.ResponseWriter, r *http.Request) {
	err := s.cronFn(r.Context(), s.db, s.statsd)
	if err != nil {
		panic(err)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
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
