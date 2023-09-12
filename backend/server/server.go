package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	pprofhttp "net/http/pprof"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/ddworken/hishtory/internal/database"
	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"github.com/rodaine/table"
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"gopkg.in/DataDog/dd-trace-go.v1/profiler"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	PostgresDb = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"
)

var (
	GLOBAL_DB      *database.DB
	GLOBAL_STATSD  *statsd.Client
	ReleaseVersion string = "UNKNOWN"
)

func getRequiredQueryParam(r *http.Request, queryParam string) string {
	val := r.URL.Query().Get(queryParam)
	if val == "" {
		panic(fmt.Sprintf("request to %s is missing required query param=%#v", r.URL, queryParam))
	}
	return val
}

func getHishtoryVersion(r *http.Request) string {
	return r.Header.Get("X-Hishtory-Version")
}

func updateUsageData(r *http.Request, userId, deviceId string, numEntriesHandled int, isQuery bool) error {
	var usageData []shared.UsageData
	usageData, err := GLOBAL_DB.UsageDataFindByUserAndDevice(r.Context(), userId, deviceId)
	if err != nil {
		return fmt.Errorf("db.UsageDataFindByUserAndDevice: %w", err)
	}
	if len(usageData) == 0 {
		err := GLOBAL_DB.CreateUsageData(
			r.Context(),
			&shared.UsageData{
				UserId:            userId,
				DeviceId:          deviceId,
				LastUsed:          time.Now(),
				NumEntriesHandled: numEntriesHandled,
				Version:           getHishtoryVersion(r),
			},
		)
		if err != nil {
			return fmt.Errorf("db.CreateUsageData: %w", err)
		}
	} else {
		usage := usageData[0]

		if err := GLOBAL_DB.UpdateUsageData(r.Context(), userId, deviceId, time.Now(), getRemoteAddr(r)); err != nil {
			return fmt.Errorf("db.UpdateUsageData: %w", err)
		}
		if numEntriesHandled > 0 {
			if err := GLOBAL_DB.UpdateUsageDataForNumEntriesHandled(r.Context(), userId, deviceId, numEntriesHandled); err != nil {
				return fmt.Errorf("db.UpdateUsageDataForNumEntriesHandled: %w", err)
			}
		}
		if usage.Version != getHishtoryVersion(r) {
			if err := GLOBAL_DB.UpdateUsageDataClientVersion(r.Context(), userId, deviceId, getHishtoryVersion(r)); err != nil {
				return fmt.Errorf("db.UpdateUsageDataClientVersion: %w", err)
			}
		}
	}
	if isQuery {
		if err := GLOBAL_DB.UpdateUsageDataNumberQueries(r.Context(), userId, deviceId); err != nil {
			return fmt.Errorf("db.UpdateUsageDataNumberQueries: %w", err)
		}
	}

	return nil
}

func usageStatsHandler(w http.ResponseWriter, r *http.Request) {
	usageData, err := GLOBAL_DB.UsageDataStats(r.Context())
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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	numDevices, err := GLOBAL_DB.CountAllDevices(r.Context())
	checkGormError(err, 0)

	numEntriesProcessed, err := GLOBAL_DB.UsageDataTotal(r.Context())
	checkGormError(err, 0)

	numDbEntries, err := GLOBAL_DB.CountHistoryEntries(r.Context())
	checkGormError(err, 0)

	oneWeek := time.Hour * 24 * 7
	weeklyActiveInstalls, err := GLOBAL_DB.CountActiveInstalls(r.Context(), oneWeek)
	checkGormError(err, 0)

	weeklyQueryUsers, err := GLOBAL_DB.CountQueryUsers(r.Context(), oneWeek)
	checkGormError(err, 0)

	lastRegistration, err := GLOBAL_DB.DateOfLastRegistration(r.Context())
	checkGormError(err, 0)

	_, _ = fmt.Fprintf(w, "Num devices: %d\n", numDevices)
	_, _ = fmt.Fprintf(w, "Num history entries processed: %d\n", numEntriesProcessed)
	_, _ = fmt.Fprintf(w, "Num DB entries: %d\n", numDbEntries)
	_, _ = fmt.Fprintf(w, "Weekly active installs: %d\n", weeklyActiveInstalls)
	_, _ = fmt.Fprintf(w, "Weekly active queries: %d\n", weeklyQueryUsers)
	_, _ = fmt.Fprintf(w, "Last registration: %s\n", lastRegistration)
}

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
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
	_ = updateUsageData(r, entries[0].UserId, entries[0].DeviceId /* numEntriesHandled = */, len(entries) /* isQuery = */, false)

	devices, err := GLOBAL_DB.DevicesForUser(r.Context(), entries[0].UserId)
	checkGormError(err, 0)

	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId))
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))

	err = GLOBAL_DB.AddHistoryEntriesForAllDevices(r.Context(), devices, entries)
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add entries to DB: %w", err))
	}
	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Count("hishtory.submit", int64(len(devices)), []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	_ = updateUsageData(r, userId, deviceId /* numEntriesHandled = */, 0 /* isQuery = */, false)
	historyEntries, err := GLOBAL_DB.AllHistoryEntriesForUser(r.Context(), userId)
	checkGormError(err, 1)
	fmt.Printf("apiBootstrapHandler: Found %d entries\n", len(historyEntries))
	if err := json.NewEncoder(w).Encode(historyEntries); err != nil {
		panic(err)
	}
}

func apiQueryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	_ = updateUsageData(r, userId, deviceId /* numEntriesHandled = */, 0 /* isQuery = */, true)

	// Delete any entries that match a pending deletion request
	deletionRequests, err := GLOBAL_DB.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err, 0)
	for _, request := range deletionRequests {
		_, err := GLOBAL_DB.ApplyDeletionRequestsToBackend(r.Context(), request)
		checkGormError(err, 0)
	}

	// Then retrieve
	historyEntries, err := GLOBAL_DB.HistoryEntriesForDevice(r.Context(), deviceId, 5)
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
			err := GLOBAL_DB.IncrementEntryReadCountsForDevice(ctx, deviceId)
			span.Finish(tracer.WithError(err))
		}()
	} else {
		err := GLOBAL_DB.IncrementEntryReadCountsForDevice(ctx, deviceId)
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

func apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
	if getMaximumNumberOfAllowedUsers() < math.MaxInt {
		numDistinctUsers, err := GLOBAL_DB.DistinctUsers(r.Context())
		if err != nil {
			panic(fmt.Errorf("db.DistinctUsers: %w", err))
		}
		if numDistinctUsers >= int64(getMaximumNumberOfAllowedUsers()) {
			panic(fmt.Sprintf("Refusing to allow registration of new device since there are currently %d users and this server allows a max of %d users", numDistinctUsers, getMaximumNumberOfAllowedUsers()))
		}
	}
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	existingDevicesCount, err := GLOBAL_DB.CountDevicesForUser(r.Context(), userId)
	checkGormError(err, 0)
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	if err := GLOBAL_DB.CreateDevice(r.Context(), &shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now()}); err != nil {
		checkGormError(err, 0)
	}

	if existingDevicesCount > 0 {
		err := GLOBAL_DB.DumpRequestCreate(r.Context(), &shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()})
		checkGormError(err, 0)
	}
	_ = updateUsageData(r, userId, deviceId /* numEntriesHandled = */, 0 /* isQuery = */, false)

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.register", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func apiGetPendingDumpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	dumpRequests, err := GLOBAL_DB.DumpRequestForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err, 0)

	if err := json.NewEncoder(w).Encode(dumpRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %w", err))
	}
}

func apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
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

	err = GLOBAL_DB.AddHistoryEntries(r.Context(), entries...)
	checkGormError(err, 0)
	err = GLOBAL_DB.DumpRequestDeleteForUserAndDevice(r.Context(), userId, requestingDeviceId)
	checkGormError(err, 0)
	_ = updateUsageData(r, userId, srcDeviceId /* numEntriesHandled = */, len(entries) /* isQuery = */, false)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func apiBannerHandler(w http.ResponseWriter, r *http.Request) {
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

func getDeletionRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// Increment the ReadCount
	err := GLOBAL_DB.DeletionRequestInc(r.Context(), userId, deviceId)
	checkGormError(err, 0)

	// Return all the deletion requests
	deletionRequests, err := GLOBAL_DB.DeletionRequestsForUserAndDevice(r.Context(), userId, deviceId)
	checkGormError(err, 0)
	if err := json.NewEncoder(w).Encode(deletionRequests); err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %w", err))
	}
}

func addDeletionRequestHandler(w http.ResponseWriter, r *http.Request) {
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

	err = GLOBAL_DB.DeletionRequestCreate(r.Context(), &request)
	checkGormError(err, 0)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if isProductionEnvironment() {
		encHistoryEntryCount, err := GLOBAL_DB.CountHistoryEntries(r.Context())
		checkGormError(err, 0)
		if encHistoryEntryCount < 1000 {
			panic("Suspiciously few enc history entries!")
		}

		deviceCount, err := GLOBAL_DB.CountAllDevices(r.Context())
		checkGormError(err, 0)
		if deviceCount < 100 {
			panic("Suspiciously few devices!")
		}
		// Check that we can write to the DB. This entry will get written and then eventually cleaned by the cron.
		err = GLOBAL_DB.AddHistoryEntries(r.Context(), &shared.EncHistoryEntry{
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
		err := GLOBAL_DB.Ping()
		if err != nil {
			panic(fmt.Errorf("failed to ping DB: %w", err))
		}
	}
	w.Write([]byte("OK"))
}

func wipeDbEntriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Host == "api.hishtory.dev" || isProductionEnvironment() {
		panic("refusing to wipe the DB for prod")
	}
	if !isTestEnvironment() {
		panic("refusing to wipe the DB non-test environment")
	}

	err := GLOBAL_DB.Unsafe_DeleteAllHistoryEntries(r.Context())
	checkGormError(err, 0)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func getNumConnectionsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := GLOBAL_DB.Stats()
	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintf(w, "%#v", stats.OpenConnections)
}

func isTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func isProductionEnvironment() bool {
	return os.Getenv("HISHTORY_ENV") == "prod"
}

func OpenDB() (*database.DB, error) {
	if isTestEnvironment() {
		db, err := database.OpenSQLite("file::memory:?_journal_mode=WAL&cache=shared", &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
		underlyingDb, err := db.DB.DB()
		if err != nil {
			return nil, fmt.Errorf("failed to access underlying DB: %w", err)
		}
		underlyingDb.SetMaxOpenConns(1)
		db.Exec("PRAGMA journal_mode = WAL")
		err = db.AddDatabaseTables()
		if err != nil {
			return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
		}
		return db, nil
	}

	// The same as the default logger, except with a higher SlowThreshold
	customLogger := logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             1000 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
	})

	var sqliteDb string
	if os.Getenv("HISHTORY_SQLITE_DB") != "" {
		sqliteDb = os.Getenv("HISHTORY_SQLITE_DB")
	}

	config := gorm.Config{Logger: customLogger}

	var db *database.DB
	if sqliteDb != "" {
		var err error
		db, err = database.OpenSQLite(sqliteDb, &config)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	} else {
		var err error
		postgresDb := fmt.Sprintf(PostgresDb, os.Getenv("POSTGRESQL_PASSWORD"))
		if os.Getenv("HISHTORY_POSTGRES_DB") != "" {
			postgresDb = os.Getenv("HISHTORY_POSTGRES_DB")
		}

		db, err = database.OpenPostgres(postgresDb, &config)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %w", err)
		}
	}
	err := db.AddDatabaseTables()
	if err != nil {
		return nil, fmt.Errorf("failed to create underlying DB tables: %w", err)
	}
	return db, nil
}

func init() {
	if ReleaseVersion == "UNKNOWN" && !isTestEnvironment() {
		panic("server.go was built without a ReleaseVersion!")
	}
	InitDB()
	go runBackgroundJobs(context.Background())
}

func cron(ctx context.Context) error {
	err := updateReleaseVersion()
	if err != nil {
		panic(err)
	}
	err = GLOBAL_DB.Clean(ctx)
	if err != nil {
		panic(err)
	}
	if GLOBAL_STATSD != nil {
		err = GLOBAL_STATSD.Flush()
		if err != nil {
			panic(err)
		}
	}
	return nil
}

func runBackgroundJobs(ctx context.Context) {
	time.Sleep(5 * time.Second)
	for {
		err := cron(ctx)
		if err != nil {
			fmt.Printf("Cron failure: %v", err)
		}
		time.Sleep(10 * time.Minute)
	}
}

func triggerCronHandler(w http.ResponseWriter, r *http.Request) {
	err := cron(r.Context())
	if err != nil {
		panic(err)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
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
	if resp.StatusCode == 403 && strings.Contains(string(respBody), "API rate limit exceeded for ") {
		return nil
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to call github API, status_code=%d, body=%#v", resp.StatusCode, string(respBody))
	}
	var info releaseInfo
	err = json.Unmarshal(respBody, &info)
	if err != nil {
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
	urls := []string{updateInfo.LinuxAmd64Url, updateInfo.LinuxAmd64AttestationUrl, updateInfo.LinuxArm64Url, updateInfo.LinuxArm64AttestationUrl,
		updateInfo.LinuxArm7Url, updateInfo.LinuxArm7AttestationUrl,
		updateInfo.DarwinAmd64Url, updateInfo.DarwinAmd64UnsignedUrl, updateInfo.DarwinAmd64AttestationUrl,
		updateInfo.DarwinArm64Url, updateInfo.DarwinArm64UnsignedUrl, updateInfo.DarwinArm64AttestationUrl}
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to retrieve URL %#v: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 {
			return fmt.Errorf("URL %#v returned 404", url)
		}
	}
	return nil
}

func InitDB() {
	var err error
	GLOBAL_DB, err = OpenDB()
	if err != nil {
		panic(fmt.Errorf("OpenDB: %w", err))
	}

	if err := GLOBAL_DB.Ping(); err != nil {
		panic(fmt.Errorf("ping: %w", err))
	}
	if isProductionEnvironment() {
		if err := GLOBAL_DB.SetMaxIdleConns(10); err != nil {
			panic(fmt.Errorf("failed to set max idle conns: %w", err))
		}
	}
	if isTestEnvironment() {
		if err := GLOBAL_DB.SetMaxIdleConns(1); err != nil {
			panic(fmt.Errorf("failed to set max idle conns: %w", err))
		}
	}
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
	return shared.UpdateInfo{
		LinuxAmd64Url:             fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64", version),
		LinuxAmd64AttestationUrl:  fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64.intoto.jsonl", version),
		LinuxArm64Url:             fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm64", version),
		LinuxArm64AttestationUrl:  fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm64.intoto.jsonl", version),
		LinuxArm7Url:              fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm", version),
		LinuxArm7AttestationUrl:   fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm.intoto.jsonl", version),
		DarwinAmd64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64", version),
		DarwinAmd64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64-unsigned", version),
		DarwinAmd64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64.intoto.jsonl", version),
		DarwinArm64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64", version),
		DarwinArm64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64-unsigned", version),
		DarwinArm64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64.intoto.jsonl", version),
		Version:                   version,
	}
}

func apiDownloadHandler(w http.ResponseWriter, r *http.Request) {
	updateInfo := buildUpdateInfo(ReleaseVersion)
	resp, err := json.Marshal(updateInfo)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func slsaStatusHandler(w http.ResponseWriter, r *http.Request) {
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

func feedbackHandler(w http.ResponseWriter, r *http.Request) {
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
	err = GLOBAL_DB.FeedbackCreate(r.Context(), &feedback)
	checkGormError(err, 0)

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.uninstall", []string{}, 1.0)
	}

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

type loggedResponseData struct {
	size int
}

type loggingResponseWriter struct {
	http.ResponseWriter
	responseData *loggedResponseData
}

func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
}

func getFunctionName(temp interface{}) string {
	strs := strings.Split((runtime.FuncForPC(reflect.ValueOf(temp).Pointer()).Name()), ".")
	return strs[len(strs)-1]
}

func withLogging(h http.HandlerFunc) http.Handler {
	logFn := func(rw http.ResponseWriter, r *http.Request) {
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
		fmt.Printf("%s %s %#v %s %s %s\n", getRemoteAddr(r), r.Method, r.RequestURI, getHishtoryVersion(r), duration.String(), byteCountToString(responseData.size))
		if GLOBAL_STATSD != nil {
			GLOBAL_STATSD.Distribution("hishtory.request_duration", float64(duration.Microseconds())/1_000, []string{"HANDLER=" + getFunctionName(h)}, 1.0)
			GLOBAL_STATSD.Incr("hishtory.request", []string{}, 1.0)
		}
	}
	return http.HandlerFunc(logFn)
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

func main() {
	mux := httptrace.NewServeMux()

	if isProductionEnvironment() {
		defer configureObservability(mux)()
		go func() {
			if err := GLOBAL_DB.DeepClean(context.Background()); err != nil {
				panic(err)
			}
		}()
	}

	mux.Handle("/api/v1/submit", withLogging(apiSubmitHandler))
	mux.Handle("/api/v1/get-dump-requests", withLogging(apiGetPendingDumpRequestsHandler))
	mux.Handle("/api/v1/submit-dump", withLogging(apiSubmitDumpHandler))
	mux.Handle("/api/v1/query", withLogging(apiQueryHandler))
	mux.Handle("/api/v1/bootstrap", withLogging(apiBootstrapHandler))
	mux.Handle("/api/v1/register", withLogging(apiRegisterHandler))
	mux.Handle("/api/v1/banner", withLogging(apiBannerHandler))
	mux.Handle("/api/v1/download", withLogging(apiDownloadHandler))
	mux.Handle("/api/v1/trigger-cron", withLogging(triggerCronHandler))
	mux.Handle("/api/v1/get-deletion-requests", withLogging(getDeletionRequestsHandler))
	mux.Handle("/api/v1/add-deletion-request", withLogging(addDeletionRequestHandler))
	mux.Handle("/api/v1/slsa-status", withLogging(slsaStatusHandler))
	mux.Handle("/api/v1/feedback", withLogging(feedbackHandler))
	mux.Handle("/healthcheck", withLogging(healthCheckHandler))
	mux.Handle("/internal/api/v1/usage-stats", withLogging(usageStatsHandler))
	mux.Handle("/internal/api/v1/stats", withLogging(statsHandler))
	if isTestEnvironment() {
		mux.Handle("/api/v1/wipe-db-entries", withLogging(wipeDbEntriesHandler))
		mux.Handle("/api/v1/get-num-connections", withLogging(getNumConnectionsHandler))
	}

	fmt.Println("Listening on localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func checkGormResult(result *gorm.DB) {
	checkGormError(result.Error, 1)
}

func checkGormError(err error, skip int) {
	if err == nil {
		return
	}

	_, filename, line, _ := runtime.Caller(skip + 1)
	panic(fmt.Sprintf("DB error at %s:%d: %v", filename, line, err))
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
// TODO: Add error checking for the calls to updateUsageData(...) that logs it/triggers an alert in prod, but is an error in test
