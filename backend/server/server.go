package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ddworken/hishtory/database"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	pprofhttp "net/http/pprof"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"github.com/rodaine/table"
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"gopkg.in/DataDog/dd-trace-go.v1/profiler"
	"gorm.io/gorm"
)

var (
	GLOBAL_STATSD  *statsd.Client
	ReleaseVersion string = "UNKNOWN"
)

func getRequiredQueryParam(r *http.Request, queryParam string) (string, error) {
	val := r.URL.Query().Get(queryParam)
	if val == "" {
		return "", fmt.Errorf("request to %s is missing required query param=%#v", r.URL, queryParam)
	}
	return val, nil
}

func getHishtoryVersion(r *http.Request) string {
	return r.Header.Get("X-Hishtory-Version")
}

func updateUsageData(ctx context.Context, db *database.DB, r *http.Request, userId, deviceId string, numEntriesHandled int, isQuery bool) {
	usageData, err := db.GetUsageData(ctx, userId, deviceId)
	if err != nil {
		log.Printf("ERROR: failed to get usage data for user_id=%s, device_id=%s: %v", userId, deviceId, err)
		return
	}

	if len(usageData) == 0 {
		if err := db.CreateUsageData(ctx, userId, deviceId, getHishtoryVersion(r), numEntriesHandled); err != nil {
			log.Printf("ERROR: failed to create usage data for user_id=%s, device_id=%s: %v", userId, deviceId, err)
			return
		}
	} else {
		if err := db.UpdateUsageData(ctx, usageData, userId, deviceId, getHishtoryVersion(r), numEntriesHandled, getRemoteAddr(r)); err != nil {
			log.Printf("ERROR: failed to update usage data for user_id=%s, device_id=%s: %v", userId, deviceId, err)
			return
		}
	}

	if isQuery {
		if err := db.UpdateNumQueries(ctx, userId, deviceId); err != nil {
			log.Printf("ERROR: failed to update num_queries for user_id=%s, device_id=%s: %v", userId, deviceId, err)
			return
		}
	}
}

func (s *server) usageStatsHandler(w http.ResponseWriter, r *http.Request) {
	query := `
	SELECT 
		MIN(devices.registration_date) as registration_date, 
		COUNT(DISTINCT devices.device_id) as num_devices,
		SUM(usage_data.num_entries_handled) as num_history_entries,
		MAX(usage_data.last_used) as last_active,
		COALESCE(STRING_AGG(DISTINCT usage_data.last_ip, ', ') FILTER (WHERE usage_data.last_ip != 'Unknown' AND usage_data.last_ip != 'UnknownIp'), 'Unknown')  as ip_addresses,
		COALESCE(SUM(usage_data.num_queries), 0) as num_queries,
		COALESCE(MAX(usage_data.last_queried), 'January 1, 1970') as last_queried,
		STRING_AGG(DISTINCT usage_data.version, ', ') as versions
	FROM devices
	INNER JOIN usage_data ON devices.device_id = usage_data.device_id
	GROUP BY devices.user_id
	ORDER BY registration_date
	`
	rows, err := s.db.WithContext(r.Context()).Raw(query).Rows()
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	tbl := table.New("Registration Date", "Num Devices", "Num Entries", "Num Queries", "Last Active", "Last Query", "Versions", "IPs")
	tbl.WithWriter(w)
	for rows.Next() {
		var registrationDate time.Time
		var numDevices int
		var numEntries int
		var lastUsedDate time.Time
		var ipAddresses string
		var numQueries int
		var lastQueried time.Time
		var versions string
		err = rows.Scan(&registrationDate, &numDevices, &numEntries, &lastUsedDate, &ipAddresses, &numQueries, &lastQueried, &versions)
		if err != nil {
			panic(err)
		}
		versions = strings.ReplaceAll(strings.ReplaceAll(versions, "Unknown", ""), ", ", "")
		lastQueryStr := strings.ReplaceAll(lastQueried.Format("2006-01-02"), "1970-01-01", "")
		tbl.AddRow(registrationDate.Format("2006-01-02"), numDevices, numEntries, numQueries, lastUsedDate.Format("2006-01-02"), lastQueryStr, versions, ipAddresses)
	}
	tbl.Print()
}

func (s *server) statsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var numDevices int64 = 0
	checkGormResult(s.db.WithContext(ctx).Model(&shared.Device{}).Count(&numDevices))
	type numEntriesProcessed struct {
		Total int
	}
	nep := numEntriesProcessed{}
	checkGormResult(s.db.WithContext(ctx).Model(&database.UsageData{}).Select("SUM(num_entries_handled) as total").Find(&nep))
	var numDbEntries int64 = 0
	checkGormResult(s.db.WithContext(ctx).Model(&shared.EncHistoryEntry{}).Count(&numDbEntries))

	lastWeek := time.Now().AddDate(0, 0, -7)
	weeklyActiveInstalls, err := s.db.GetLastUsedSince(ctx, lastWeek)
	if err != nil {
		panic(fmt.Errorf("failed to get usage data since %s: %w", lastWeek, err))
	}
	weeklyQueryUsers, err := s.db.GetLastQueriedSince(ctx, lastWeek)
	if err != nil {
		panic(fmt.Errorf("failed to get query data since %s: %w", lastWeek, err))
	}

	var lastRegistration string
	row := s.db.WithContext(ctx).Raw("select to_char(max(registration_date), 'DD Month YYYY HH24:MI') from devices").Row()
	if err := row.Scan(&lastRegistration); err != nil {
		panic(err)
	}
	fmt.Fprintf(w, "Num devices: %d\n", numDevices)
	fmt.Fprintf(w, "Num history entries processed: %d\n", nep.Total)
	fmt.Fprintf(w, "Num DB entries: %d\n", numDbEntries)
	fmt.Fprintf(w, "Weekly active installs: %d\n", weeklyActiveInstalls)
	fmt.Fprintf(w, "Weekly active queries: %d\n", weeklyQueryUsers)
	fmt.Fprintf(w, "Last registration: %s\n", lastRegistration)
}

func (s *server) apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	defer r.Body.Close()
	var entries []*shared.EncHistoryEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Failed to decode request: %v", err)
		return
	}
	fmt.Printf("apiSubmitHandler: received request containg %d EncHistoryEntry\n", len(entries))
	if len(entries) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	updateUsageData(ctx, s.db, r, entries[0].UserId, entries[0].DeviceId, len(entries), false)

	devices, err := s.db.GetDevicesForUser(ctx, entries[0].UserId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		panic(fmt.Errorf("s.db.GetDevicesForUser: %w", err))
	}
	if len(devices) == 0 {
		err := fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%v", err)
		fmt.Println(err)
		return
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, device := range devices {
			for _, entry := range entries {
				entry.DeviceId = device.DeviceId
				if entry.Date.IsZero() {
					entry.Date = time.Now()
				}
			}
			// Chunk the inserts to prevent the `extended protocol limited to 65535 parameters` error
			for _, entriesChunk := range shared.Chunks(entries, 1000) {
				checkGormResult(tx.Create(&entriesChunk))
			}
		}
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add entries to DB: %w", err))
	}
	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Count("hishtory.submit", int64(len(devices)), []string{}, 1.0)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := getRequiredQueryParam(r, "user_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	deviceId, err := getRequiredQueryParam(r, "device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	updateUsageData(ctx, s.db, r, userId, deviceId, 0, false)

	historyEntries, err := s.db.GetHistoryEntriesForUser(ctx, userId)
	if err != nil {
		panic(fmt.Errorf("db.GetHistoryEntriesForUser: %w", err))
	}
	fmt.Printf("apiBootstrapHandler: Found %d entries\n", len(historyEntries))

	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}
}

func (s *server) apiQueryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := getRequiredQueryParam(r, "user_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	deviceId, err := getRequiredQueryParam(r, "device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	updateUsageData(ctx, s.db, r, userId, deviceId, 0, true)

	// Delete any entries that match a pending deletion request
	deletionRequests, err := s.db.GetDeletionRequests(ctx, deviceId, userId)
	if err != nil {
		panic(fmt.Errorf("db.GetDeletionRequests: %w", err))
	}
	for _, request := range deletionRequests {
		_, err := s.db.ApplyDeletionRequests(ctx, *request)
		if err != nil {
			panic(err)
		}
	}

	// Then retrieve
	historyEntries, err := s.db.GetHistoryEntriesForDevice(ctx, deviceId)
	if err != nil {
		panic(fmt.Errorf("db.GetHistoryEntriesForDevice: %w", err))
	}
	fmt.Printf("apiQueryHandler: Found %d entries for %s\n", len(historyEntries), r.URL)

	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}

	// And finally, kick off a background goroutine that will increment the read count. Doing it in the background avoids
	// blocking the entire response. This does have a potential race condition, but that is fine.
	if isProductionEnvironment() {
		go func() {
			span, ctx := tracer.StartSpanFromContext(ctx, "apiQueryHandler.incrementReadCount")
			err := s.db.IncrementReadCounts(ctx, deviceId)
			span.Finish(tracer.WithError(err))
		}()
	} else {
		err := s.db.IncrementReadCounts(ctx, deviceId)
		if err != nil {
			panic("failed to increment read counts")
		}
	}

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.query", []string{}, 1.0)
	}
}

func getRemoteAddr(r *http.Request) string {
	addr, ok := r.Header["X-Real-Ip"]
	if !ok || len(addr) == 0 {
		return "UnknownIp"
	}
	return addr[0]
}

func (s *server) apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if getMaximumNumberOfAllowedUsers() < math.MaxInt {
		numDistinctUsers, err := s.db.GetDistinctUserCount(ctx)
		if err != nil {
			panic(fmt.Errorf("db.GetDistinctUserCount: %w", err))
		}
		if numDistinctUsers >= int64(getMaximumNumberOfAllowedUsers()) {
			panic(fmt.Sprintf("Refusing to allow registration of new device since there are currently %d users and this server allows a max of %d users", numDistinctUsers, getMaximumNumberOfAllowedUsers()))
		}
	}
	userId, err := getRequiredQueryParam(r, "user_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	deviceId, err := getRequiredQueryParam(r, "device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	var existingDevicesCount int64 = -1
	checkGormResult(s.db.WithContext(ctx).Model(&shared.Device{}).Where("user_id = ?", userId).Count(&existingDevicesCount))
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	checkGormResult(s.db.WithContext(ctx).Create(&shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now()}))
	if existingDevicesCount > 0 {
		checkGormResult(s.db.WithContext(ctx).Create(&shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()}))
	}
	updateUsageData(ctx, s.db, r, userId, deviceId, 0, false)

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.register", []string{}, 1.0)
	}
}

func (s *server) apiGetPendingDumpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := getRequiredQueryParam(r, "user_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	deviceId, err := getRequiredQueryParam(r, "device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	dumpRequests, err := s.db.GetDumpRequests(ctx, userId, deviceId)
	if err != nil {
		panic(fmt.Errorf("db.GetDumpRequests: %w", err))
	}

	if err := json.NewEncoder(w).Encode(dumpRequests); err != nil {
		panic(fmt.Errorf("failed to JSON encode the dump requests: %w", err))
	}
}

func (s *server) apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := getRequiredQueryParam(r, "user_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	srcDeviceId, err := getRequiredQueryParam(r, "source_device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	requestingDeviceId, err := getRequiredQueryParam(r, "requesting_device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(fmt.Errorf("io.ReadAll: %w", err))
	}
	var entries []shared.EncHistoryEntry

	if err := json.Unmarshal(data, &entries); err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiSubmitDumpHandler: received request containg %d EncHistoryEntry\n", len(entries))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, entry := range entries {
			entry.DeviceId = requestingDeviceId
			if entry.UserId != userId {
				return fmt.Errorf("batch contains an entry with UserId=%#v, when the query param contained the user_id=%#v", entry.UserId, userId)
			}
			checkGormResult(tx.Create(&entry))
		}
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add dumped DB: %w", err))
	}
	checkGormResult(s.db.WithContext(ctx).Delete(&shared.DumpRequest{}, "user_id = ? AND requesting_device_id = ?", userId, requestingDeviceId))
	updateUsageData(ctx, s.db, r, userId, srcDeviceId, len(entries), false)
}

func (s *server) apiBannerHandler(w http.ResponseWriter, r *http.Request) {
	commitHash, err := getRequiredQueryParam(r, "commit_hash")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	deviceId, err := getRequiredQueryParam(r, "device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	forcedBanner := r.URL.Query().Get("forced_banner")
	fmt.Printf("apiBannerHandler: commit_hash=%#v, device_id=%#v, forced_banner=%#v\n", commitHash, deviceId, forcedBanner)
	if getHishtoryVersion(r) == "v0.160" {
		w.Write([]byte("Warning: hiSHtory v0.160 has a bug that slows down your shell! Please run `hishtory update` to upgrade hiSHtory."))
		return
	}
	w.Write([]byte(html.EscapeString(forcedBanner)))
}

func (s *server) getDeletionRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, err := getRequiredQueryParam(r, "user_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	deviceId, err := getRequiredQueryParam(r, "device_id")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	// Increment the ReadCount
	if err := s.db.IncrementReadCount(ctx, userId, deviceId); err != nil {
		panic(fmt.Errorf("db.IncrementReadCount: %w", err))
	}

	// Return all the deletion requests
	deletionRequests, err := s.db.GetDeletionRequests(ctx, userId, deviceId)
	if err != nil {
		panic(fmt.Errorf("db.GetDeletionRequests: %w", err))
	}
	if err := json.NewEncoder(w).Encode(deletionRequests); err != nil {
		panic(fmt.Errorf("failed to JSON encode the dump requests: %w", err))
	}
}

func (s *server) addDeletionRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var request shared.DeletionRequest
	err = json.Unmarshal(data, &request)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	request.ReadCount = 0
	fmt.Printf("addDeletionRequestHandler: received request containg %d messages to be deleted\n", len(request.Messages.Ids))

	// Store the deletion request so all the devices will get it
	devices, err := s.db.GetDevicesForUser(ctx, request.UserId)
	if err != nil {
		panic(fmt.Errorf("db.GetDevicesForUser: %w", err))
	}
	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", request.UserId))
	}
	fmt.Printf("addDeletionRequestHandler: Found %d devices\n", len(devices))
	for _, device := range devices {
		request.DestinationDeviceId = device.DeviceId
		checkGormResult(s.db.WithContext(ctx).Create(&request))
	}

	// Also delete anything currently in the DB matching it
	numDeleted, err := s.db.ApplyDeletionRequests(ctx, request)
	if err != nil {
		panic(fmt.Errorf("db.ApplyDeletionRequests: %w", err))
	}
	fmt.Printf("addDeletionRequestHandler: Deleted %d rows in the backend\n", numDeleted)
}

func (s *server) wipeDbEntriesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Host == "api.hishtory.dev" || isProductionEnvironment() {
		panic("refusing to wipe the DB for prod")
	}
	if !isTestEnvironment() {
		panic("refusing to wipe the DB non-test environment")
	}
	checkGormResult(s.db.WithContext(ctx).Exec("DELETE FROM enc_history_entries"))
}

func (s *server) getNumConnectionsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		panic(fmt.Errorf("db.Stats: %w", err))
	}
	fmt.Fprintf(w, "%#v", stats.OpenConnections)
}

func isTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func isProductionEnvironment() bool {
	return os.Getenv("HISHTORY_ENV") == "prod"
}

func init() {
	if ReleaseVersion == "UNKNOWN" && !isTestEnvironment() {
		panic("server.go was built without a ReleaseVersion!")
	}
}

func cron(ctx context.Context, db *database.DB) error {
	if err := updateReleaseVersion(); err != nil {
		panic(err)
	}

	if err := db.Clean(ctx); err != nil {
		panic(err)
	}

	if GLOBAL_STATSD != nil {
		err := GLOBAL_STATSD.Flush()
		if err != nil {
			panic(err)
		}
	}

	return nil
}

func runBackgroundJobs(ctx context.Context, db *database.DB) {
	time.Sleep(5 * time.Second)
	for {
		err := cron(ctx, db)
		if err != nil {
			fmt.Printf("Cron failure: %v", err)
		}
		time.Sleep(10 * time.Minute)
	}
}

func (s *server) triggerCronHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := cron(ctx, s.db)
	if err != nil {
		panic(err)
	}
}

type releaseInfo struct {
	Name string `json:"name"`
}

func updateReleaseVersion() error {
	resp, err := http.Get("https://api.github.com/repos/ddworken/hishtory/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to get latest release version: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read github API response body: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden && strings.Contains(string(respBody), "API rate limit exceeded for ") {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to call github API, status_code=%d, body=%#v", resp.StatusCode, string(respBody))
	}

	var info releaseInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return fmt.Errorf("failed to parse github API response: %w", err)
	}

	latestVersionTag := info.Name
	ReleaseVersion = decrementVersionIfInvalid(latestVersionTag)
	return nil
}

func decrementVersionIfInvalid(initialVersion string) string {
	// Decrements the version up to 5 times if the version doesn't have valid binaries yet.
	version := initialVersion
	for i := 0; i < 5; i++ {
		updateInfo := buildUpdateInfo(version)
		err := assertValidUpdate(updateInfo)
		if err == nil {
			fmt.Printf("Found a valid version: %v\n", version)
			return version
		}
		fmt.Printf("Found %s to be an invalid version: %v\n", version, err)
		version, err = decrementVersion(version)
		if err != nil {
			fmt.Printf("Failed to decrement version after finding the latest version was invalid: %v\n", err)
			return initialVersion
		}
	}
	fmt.Printf("Decremented the version 5 times and failed to find a valid version version number, initial version number: %v, last checked version number: %v\n", initialVersion, version)
	return initialVersion
}

func assertValidUpdate(updateInfo shared.UpdateInfo) error {
	urls := []string{
		updateInfo.LinuxAmd64Url,
		updateInfo.LinuxAmd64AttestationUrl,
		updateInfo.LinuxArm64Url,
		updateInfo.LinuxArm64AttestationUrl,
		updateInfo.LinuxArm7Url,
		updateInfo.LinuxArm7AttestationUrl,
		updateInfo.DarwinAmd64Url,
		updateInfo.DarwinAmd64UnsignedUrl,
		updateInfo.DarwinAmd64AttestationUrl,
		updateInfo.DarwinArm64Url,
		updateInfo.DarwinArm64UnsignedUrl,
		updateInfo.DarwinArm64AttestationUrl,
	}
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to retrieve URL %#v: %v", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("URL %#v returned 404", url)
		}
	}
	return nil
}

func InitDB() (*database.DB, error) {
	db, err := database.OpenDB(isTestEnvironment())
	if err != nil {
		return nil, fmt.Errorf("OpenDB: %w", err)
	}
	sqlDb, err := db.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("db.SqlDB: %w", err)
	}

	if err := sqlDb.Ping(); err != nil {
		return nil, fmt.Errorf("sqlDb.Ping: %w", err)
	}
	if isProductionEnvironment() {
		sqlDb.SetMaxIdleConns(10)
	}
	if isTestEnvironment() {
		sqlDb.SetMaxIdleConns(1)
	}

	return db, nil
}

func decrementVersion(version string) (string, error) {
	if version == "UNKNOWN" {
		return "", fmt.Errorf("cannot decrement UNKNOWN")
	}
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid version: %s", version)
	}
	versionNumber, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid version: %s", version)
	}
	return parts[0] + "." + strconv.Itoa(versionNumber-1), nil
}

func buildUpdateInfo(version string) shared.UpdateInfo {
	const urlPrefix = "https://github.com/ddworken/hishtory/releases/download"
	return shared.UpdateInfo{
		LinuxAmd64Url:             fmt.Sprintf("%s/%s/hishtory-linux-amd64", urlPrefix, version),
		LinuxAmd64AttestationUrl:  fmt.Sprintf("%s/%s/hishtory-linux-amd64.intoto.jsonl", urlPrefix, version),
		LinuxArm64Url:             fmt.Sprintf("%s/%s/hishtory-linux-arm64", urlPrefix, version),
		LinuxArm64AttestationUrl:  fmt.Sprintf("%s/%s/hishtory-linux-arm64.intoto.jsonl", urlPrefix, version),
		LinuxArm7Url:              fmt.Sprintf("%s/%s/hishtory-linux-arm", urlPrefix, version),
		LinuxArm7AttestationUrl:   fmt.Sprintf("%s/%s/hishtory-linux-arm.intoto.jsonl", urlPrefix, version),
		DarwinAmd64Url:            fmt.Sprintf("%s/%s/hishtory-darwin-amd64", urlPrefix, version),
		DarwinAmd64UnsignedUrl:    fmt.Sprintf("%s/%s/hishtory-darwin-amd64-unsigned", urlPrefix, version),
		DarwinAmd64AttestationUrl: fmt.Sprintf("%s/%s/hishtory-darwin-amd64.intoto.jsonl", urlPrefix, version),
		DarwinArm64Url:            fmt.Sprintf("%s/%s/hishtory-darwin-arm64", urlPrefix, version),
		DarwinArm64UnsignedUrl:    fmt.Sprintf("%s/%s/hishtory-darwin-arm64-unsigned", urlPrefix, version),
		DarwinArm64AttestationUrl: fmt.Sprintf("%s/%s/hishtory-darwin-arm64.intoto.jsonl", urlPrefix, version),
		Version:                   version,
	}
}

func (s *server) apiDownloadHandler(w http.ResponseWriter, r *http.Request) {
	updateInfo := buildUpdateInfo(ReleaseVersion)
	resp, err := json.Marshal(updateInfo)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func (s *server) slsaStatusHandler(w http.ResponseWriter, r *http.Request) {
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

func (s *server) feedbackHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	checkGormResult(s.db.WithContext(ctx).Create(feedback))

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.uninstall", []string{}, 1.0)
	}
}

type loggedResponseData struct {
	size int
}

type loggingResponseWriter struct {
	http.ResponseWriter
	responseData *loggedResponseData
	statusCode   int
}

func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func getFunctionName(temp interface{}) string {
	strs := strings.Split((runtime.FuncForPC(reflect.ValueOf(temp).Pointer()).Name()), ".")
	return strs[len(strs)-1]
}

func withLogging(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var responseData loggedResponseData
		lrw := loggingResponseWriter{
			ResponseWriter: rw,
			responseData:   &responseData,
		}
		start := time.Now()
		span, ctx := tracer.StartSpanFromContext(
			r.Context(),
			getFunctionName(h),
			tracer.SpanType(ext.SpanTypeSQL),
			tracer.ServiceName("hishtory-api"),
		)
		defer span.Finish()

		h(&lrw, r.WithContext(ctx))

		duration := time.Since(start)
		fmt.Printf("%s %s %d %#v %s %s %s\n", getRemoteAddr(r), r.Method, lrw.statusCode, r.RequestURI, getHishtoryVersion(r), duration.String(), byteCountToString(responseData.size))
		if GLOBAL_STATSD != nil {
			GLOBAL_STATSD.Distribution("hishtory.request_duration", float64(duration.Microseconds())/1_000, []string{"HANDLER=" + getFunctionName(h)}, 1.0)
			GLOBAL_STATSD.Incr("hishtory.request", []string{}, 1.0)
		}
	})
}

func byteCountToString(b int) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMG"[exp])
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
	defer tracer.Stop()
	// Stats
	ddStats, err := statsd.New("unix:///var/run/datadog/dsd.socket")
	if err != nil {
		fmt.Printf("Failed to start DataDog statsd: %v\n", err)
	}
	GLOBAL_STATSD = ddStats
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

type server struct {
	db *database.DB
}

func (s *server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isProductionEnvironment() {
		// Check that we have a reasonable looking set of devices/entries in the DB
		rows, err := s.db.Raw("SELECT true FROM enc_history_entries LIMIT 1 OFFSET 1000").Rows()
		if err != nil {
			panic(fmt.Sprintf("failed to count entries in DB: %v", err))
		}
		defer rows.Close()
		if !rows.Next() {
			panic("Suspiciously few enc history entries!")
		}
		var count int64
		checkGormResult(s.db.WithContext(ctx).Model(&shared.Device{}).Count(&count))
		if count < 100 {
			panic("Suspiciously few devices!")
		}
		// Check that we can write to the DB. This entry will get written and then eventually cleaned by the cron.
		checkGormResult(s.db.WithContext(ctx).Create(&shared.EncHistoryEntry{
			EncryptedData: []byte("data"),
			Nonce:         []byte("nonce"),
			DeviceId:      "healthcheck_device_id",
			UserId:        "healthcheck_user_id",
			Date:          time.Now(),
			EncryptedId:   "healthcheck_enc_id",
			ReadCount:     10000,
		}))
	} else {
		if err := s.db.Ping(); err != nil {
			panic(fmt.Sprintf("Ping: %v", err))
		}
	}

	fmt.Fprintf(w, "OK")
}

func main() {
	mux := httptrace.NewServeMux()

	db, err := InitDB()
	if err != nil {
		log.Fatalf("initDB: %v", err)
	}

	go runBackgroundJobs(context.Background(), db)

	if isProductionEnvironment() {
		defer configureObservability(mux)()
		go func() {
			if err := db.DeepCleanDatabase(context.Background()); err != nil {
				panic(fmt.Sprintf("failed to deep clean DB: %v", err))
			}
		}()
	}

	s := &server{db}

	mux.Handle("/api/v1/submit", withLogging(s.apiSubmitHandler))
	mux.Handle("/api/v1/get-dump-requests", withLogging(s.apiGetPendingDumpRequestsHandler))
	mux.Handle("/api/v1/submit-dump", withLogging(s.apiSubmitDumpHandler))
	mux.Handle("/api/v1/query", withLogging(s.apiQueryHandler))
	mux.Handle("/api/v1/bootstrap", withLogging(s.apiBootstrapHandler))
	mux.Handle("/api/v1/register", withLogging(s.apiRegisterHandler))
	mux.Handle("/api/v1/banner", withLogging(s.apiBannerHandler))
	mux.Handle("/api/v1/download", withLogging(s.apiDownloadHandler))
	mux.Handle("/api/v1/trigger-cron", withLogging(s.triggerCronHandler))
	mux.Handle("/api/v1/get-deletion-requests", withLogging(s.getDeletionRequestsHandler))
	mux.Handle("/api/v1/add-deletion-request", withLogging(s.addDeletionRequestHandler))
	mux.Handle("/api/v1/slsa-status", withLogging(s.slsaStatusHandler))
	mux.Handle("/api/v1/feedback", withLogging(s.feedbackHandler))
	mux.Handle("/healthcheck", withLogging(s.healthCheckHandler))
	mux.Handle("/internal/api/v1/usage-stats", withLogging(s.usageStatsHandler))
	mux.Handle("/internal/api/v1/stats", withLogging(s.statsHandler))
	if isTestEnvironment() {
		mux.Handle("/api/v1/wipe-db-entries", withLogging(s.wipeDbEntriesHandler))
		mux.Handle("/api/v1/get-num-connections", withLogging(s.getNumConnectionsHandler))
	}
	fmt.Println("Listening on localhost:8080")

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		<-ch
		fmt.Println("Shutting down...")
		if err := httpServer.Shutdown(context.Background()); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		}
	}()

	if err := httpServer.ListenAndServe(); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func checkGormResult(result *gorm.DB) {
	if result.Error != nil {
		_, filename, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("DB error at %s:%d: %v", filename, line, result.Error))
	}
}

func getMaximumNumberOfAllowedUsers() int {
	maxNumUsersStr := os.Getenv("HISHTORY_MAX_NUM_USERS")
	if maxNumUsersStr == "" {
		return math.MaxInt
	}
	maxNumUsers, err := strconv.Atoi(maxNumUsersStr)
	if err != nil {
		return math.MaxInt
	}
	return maxNumUsers
}

// TODO(optimization): Maybe optimize the endpoints a bit to reduce the number of round trips required?
