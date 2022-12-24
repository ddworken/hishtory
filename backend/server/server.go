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
	"github.com/ddworken/hishtory/shared"
	"github.com/jackc/pgx/v4/stdlib"
	_ "github.com/lib/pq"
	"github.com/rodaine/table"
	sqltrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/database/sql"
	gormtrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/gorm.io/gorm.v1"
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"gopkg.in/DataDog/dd-trace-go.v1/profiler"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	PostgresDb = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"
)

var (
	GLOBAL_DB      *gorm.DB
	GLOBAL_STATSD  *statsd.Client
	ReleaseVersion string = "UNKNOWN"
)

type UsageData struct {
	UserId            string    `json:"user_id" gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	DeviceId          string    `json:"device_id"  gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	LastUsed          time.Time `json:"last_used"`
	LastIp            string    `json:"last_ip"`
	NumEntriesHandled int       `json:"num_entries_handled"`
	LastQueried       time.Time `json:"last_queried"`
	NumQueries        int       `json:"num_queries"`
	Version           string    `json:"version"`
}

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

func updateUsageData(ctx context.Context, r *http.Request, userId, deviceId string, numEntriesHandled int, isQuery bool) {
	var usageData []UsageData
	GLOBAL_DB.WithContext(ctx).Where("user_id = ? AND device_id = ?", userId, deviceId).Find(&usageData)
	if len(usageData) == 0 {
		GLOBAL_DB.WithContext(ctx).Create(&UsageData{UserId: userId, DeviceId: deviceId, LastUsed: time.Now(), NumEntriesHandled: numEntriesHandled, Version: getHishtoryVersion(r)})
	} else {
		usage := usageData[0]
		GLOBAL_DB.WithContext(ctx).Model(&UsageData{}).Where("user_id = ? AND device_id = ?", userId, deviceId).Update("last_used", time.Now()).Update("last_ip", getRemoteAddr(r))
		if numEntriesHandled > 0 {
			GLOBAL_DB.WithContext(ctx).Exec("UPDATE usage_data SET num_entries_handled = COALESCE(num_entries_handled, 0) + ? WHERE user_id = ? AND device_id = ?", numEntriesHandled, userId, deviceId)
		}
		if usage.Version != getHishtoryVersion(r) {
			GLOBAL_DB.WithContext(ctx).Exec("UPDATE usage_data SET version = ? WHERE user_id = ? AND device_id = ?", getHishtoryVersion(r), userId, deviceId)
		}
	}
	if isQuery {
		GLOBAL_DB.WithContext(ctx).Exec("UPDATE usage_data SET num_queries = COALESCE(num_queries, 0) + 1, last_queried = ? WHERE user_id = ? AND device_id = ?", time.Now(), userId, deviceId)
	}
}

func usageStatsHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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
	rows, err := GLOBAL_DB.WithContext(ctx).Raw(query).Rows()
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

func statsHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var numDevices int64 = 0
	checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&shared.Device{}).Count(&numDevices))
	type numEntriesProcessed struct {
		Total int
	}
	nep := numEntriesProcessed{}
	checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&UsageData{}).Select("SUM(num_entries_handled) as total").Find(&nep))
	var numDbEntries int64 = 0
	checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&shared.EncHistoryEntry{}).Count(&numDbEntries))

	lastWeek := time.Now().AddDate(0, 0, -7)
	var weeklyActiveInstalls int64 = 0
	checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&UsageData{}).Where("last_used > ?", lastWeek).Count(&weeklyActiveInstalls))
	var weeklyQueryUsers int64 = 0
	checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&UsageData{}).Where("last_queried > ?", lastWeek).Count(&weeklyQueryUsers))
	var lastRegistration string = ""
	row := GLOBAL_DB.WithContext(ctx).Raw("select to_char(max(registration_date), 'DD Month YYYY HH24:MI') from devices").Row()
	err := row.Scan(&lastRegistration)
	if err != nil {
		panic(err)
	}
	w.Write([]byte(fmt.Sprintf("Num devices: %d\n", numDevices)))
	w.Write([]byte(fmt.Sprintf("Num history entries processed: %d\n", nep.Total)))
	w.Write([]byte(fmt.Sprintf("Num DB entries: %d\n", numDbEntries)))
	w.Write([]byte(fmt.Sprintf("Weekly active installs: %d\n", weeklyActiveInstalls)))
	w.Write([]byte(fmt.Sprintf("Weekly active queries: %d\n", weeklyQueryUsers)))
	w.Write([]byte(fmt.Sprintf("Last registration: %s\n", lastRegistration)))
}

func apiSubmitHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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
	updateUsageData(ctx, r, entries[0].UserId, entries[0].DeviceId, len(entries), false)
	tx := GLOBAL_DB.WithContext(ctx).Where("user_id = ?", entries[0].UserId)
	var devices []*shared.Device
	checkGormResult(tx.Find(&devices))
	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId))
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))
	err = GLOBAL_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, device := range devices {
			for _, entry := range entries {
				entry.DeviceId = device.DeviceId
			}
			// Chunk the inserts to prevent the `extended protocol limited to 65535 parameters` error
			for _, entriesChunk := range shared.Chunks(entries, 1000) {
				checkGormResult(tx.Create(&entriesChunk))
			}
		}
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add entries to DB: %v", err))
	}
	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Count("hishtory.submit", int64(len(devices)), []string{}, 1.0)
	}
}

func apiBootstrapHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	updateUsageData(ctx, r, userId, deviceId, 0, false)
	tx := GLOBAL_DB.WithContext(ctx).Where("user_id = ?", userId)
	var historyEntries []*shared.EncHistoryEntry
	checkGormResult(tx.Find(&historyEntries))
	fmt.Printf("apiBootstrapHandler: Found %d entries\n", len(historyEntries))
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func apiQueryHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	updateUsageData(ctx, r, userId, deviceId, 0, true)

	// Delete any entries that match a pending deletion request
	var deletionRequests []*shared.DeletionRequest
	checkGormResult(GLOBAL_DB.WithContext(ctx).Where("destination_device_id = ? AND user_id = ?", deviceId, userId).Find(&deletionRequests))
	for _, request := range deletionRequests {
		_, err := applyDeletionRequestsToBackend(ctx, *request)
		if err != nil {
			panic(err)
		}
	}

	// Then retrieve
	tx := GLOBAL_DB.WithContext(ctx).Where("device_id = ? AND read_count < 5", deviceId)
	var historyEntries []*shared.EncHistoryEntry
	checkGormResult(tx.Find(&historyEntries))
	fmt.Printf("apiQueryHandler: Found %d entries for %s\n", len(historyEntries), r.URL)
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)

	// And finally, kick off a background goroutine that will increment the read count. Doing it in the background avoids
	// blocking the entire response. This does have a potential race condition, but that is fine.
	if isProductionEnvironment() {
		go func() {
			span, ctx := tracer.StartSpanFromContext(ctx, "apiQueryHandler.incrementReadCount")
			err = incrementReadCounts(ctx, deviceId)
			span.Finish(tracer.WithError(err))
		}()
	} else {
		err = incrementReadCounts(ctx, deviceId)
		if err != nil {
			panic("failed to increment read counts")
		}
	}

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.query", []string{}, 1.0)
	}
}

func incrementReadCounts(ctx context.Context, deviceId string) error {
	return GLOBAL_DB.WithContext(ctx).Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceId).Error
}

func getRemoteAddr(r *http.Request) string {
	addr, ok := r.Header["X-Real-Ip"]
	if !ok || len(addr) == 0 {
		return "UnknownIp"
	}
	return addr[0]
}

func apiRegisterHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if getMaximumNumberOfAllowedUsers() < math.MaxInt {
		row := GLOBAL_DB.WithContext(ctx).Raw("SELECT COUNT(DISTINCT devices.user_id) FROM devices").Row()
		var numDistinctUsers int64 = 0
		err := row.Scan(&numDistinctUsers)
		if err != nil {
			panic(err)
		}
		if numDistinctUsers >= int64(getMaximumNumberOfAllowedUsers()) {
			panic(fmt.Sprintf("Refusing to allow registration of new device since there are currently %d users and this server allows a max of %d users", numDistinctUsers, getMaximumNumberOfAllowedUsers()))
		}
	}
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var existingDevicesCount int64 = -1
	checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&shared.Device{}).Where("user_id = ?", userId).Count(&existingDevicesCount))
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	checkGormResult(GLOBAL_DB.WithContext(ctx).Create(&shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now()}))
	if existingDevicesCount > 0 {
		checkGormResult(GLOBAL_DB.WithContext(ctx).Create(&shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()}))
	}
	updateUsageData(ctx, r, userId, deviceId, 0, false)

	if GLOBAL_STATSD != nil {
		GLOBAL_STATSD.Incr("hishtory.register", []string{}, 1.0)
	}
}

func apiGetPendingDumpRequestsHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	checkGormResult(GLOBAL_DB.WithContext(ctx).Where("user_id = ? AND requesting_device_id != ?", userId, deviceId).Find(&dumpRequests))
	respBody, err := json.Marshal(dumpRequests)
	if err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %v", err))
	}
	w.Write(respBody)
}

func apiSubmitDumpHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	srcDeviceId := getRequiredQueryParam(r, "source_device_id")
	requestingDeviceId := getRequiredQueryParam(r, "requesting_device_id")
	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var entries []shared.EncHistoryEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiSubmitDumpHandler: received request containg %d EncHistoryEntry\n", len(entries))
	err = GLOBAL_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		panic(fmt.Errorf("failed to execute transaction to add dumped DB: %v", err))
	}
	checkGormResult(GLOBAL_DB.WithContext(ctx).Delete(&shared.DumpRequest{}, "user_id = ? AND requesting_device_id = ?", userId, requestingDeviceId))
	updateUsageData(ctx, r, userId, srcDeviceId, len(entries), false)
}

func apiBannerHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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

func getDeletionRequestsHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")

	// Increment the ReadCount
	checkGormResult(GLOBAL_DB.WithContext(ctx).Exec("UPDATE deletion_requests SET read_count = read_count + 1 WHERE destination_device_id = ? AND user_id = ?", deviceId, userId))

	// Return all the deletion requests
	var deletionRequests []*shared.DeletionRequest
	checkGormResult(GLOBAL_DB.WithContext(ctx).Where("user_id = ? AND destination_device_id = ?", userId, deviceId).Find(&deletionRequests))
	respBody, err := json.Marshal(deletionRequests)
	if err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %v", err))
	}
	w.Write(respBody)
}

func addDeletionRequestHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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
	tx := GLOBAL_DB.WithContext(ctx).Where("user_id = ?", request.UserId)
	var devices []*shared.Device
	checkGormResult(tx.Find(&devices))
	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", request.UserId))
	}
	fmt.Printf("addDeletionRequestHandler: Found %d devices\n", len(devices))
	for _, device := range devices {
		request.DestinationDeviceId = device.DeviceId
		checkGormResult(GLOBAL_DB.WithContext(ctx).Create(&request))
	}

	// Also delete anything currently in the DB matching it
	numDeleted, err := applyDeletionRequestsToBackend(ctx, request)
	if err != nil {
		panic(err)
	}
	fmt.Printf("addDeletionRequestHandler: Deleted %d rows in the backend\n", numDeleted)
}

func healthCheckHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if isProductionEnvironment() {
		// Check that we have a reasonable looking set of devices/entries in the DB
		rows, err := GLOBAL_DB.Raw("SELECT true FROM enc_history_entries LIMIT 1 OFFSET 1000").Rows()
		if err != nil {
			panic(fmt.Sprintf("failed to count entries in DB: %v", err))
		}
		defer rows.Close()
		if !rows.Next() {
			panic("Suspiciously few enc history entries!")
		}
		var count int64
		checkGormResult(GLOBAL_DB.WithContext(ctx).Model(&shared.Device{}).Count(&count))
		if count < 100 {
			panic("Suspiciously few devices!")
		}
		// Check that we can write to the DB. This entry will get written and then eventually cleaned by the cron.
		checkGormResult(GLOBAL_DB.WithContext(ctx).Create(&shared.EncHistoryEntry{
			EncryptedData: []byte("data"),
			Nonce:         []byte("nonce"),
			DeviceId:      "healthcheck_device_id",
			UserId:        "healthcheck_user_id",
			Date:          time.Now(),
			EncryptedId:   "healthcheck_enc_id",
			ReadCount:     10000,
		}))
	} else {
		db, err := GLOBAL_DB.DB()
		if err != nil {
			panic(fmt.Sprintf("failed to get DB: %v", err))
		}
		err = db.Ping()
		if err != nil {
			panic(fmt.Sprintf("failed to ping DB: %v", err))
		}
	}
	ok := "OK"
	w.Write([]byte(ok))
}

func applyDeletionRequestsToBackend(ctx context.Context, request shared.DeletionRequest) (int, error) {
	tx := GLOBAL_DB.WithContext(ctx).Where("false")
	for _, message := range request.Messages.Ids {
		tx = tx.Or(GLOBAL_DB.WithContext(ctx).Where("user_id = ? AND device_id = ? AND date = ?", request.UserId, message.DeviceId, message.Date))
	}
	result := tx.Delete(&shared.EncHistoryEntry{})
	checkGormResult(result)
	return int(result.RowsAffected), nil
}

func wipeDbEntriesHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if r.Host == "api.hishtory.dev" || isProductionEnvironment() {
		panic("refusing to wipe the DB for prod")
	}
	if !isTestEnvironment() {
		panic("refusing to wipe the DB non-test environment")
	}
	checkGormResult(GLOBAL_DB.WithContext(ctx).Exec("DELETE FROM enc_history_entries"))
}

func getNumConnectionsHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	sqlDb, err := GLOBAL_DB.DB()
	if err != nil {
		panic(err)
	}
	w.Write([]byte(fmt.Sprintf("%#v", sqlDb.Stats().OpenConnections)))
}

func isTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func isProductionEnvironment() bool {
	return os.Getenv("HISHTORY_ENV") == "prod"
}

func OpenDB() (*gorm.DB, error) {
	if isTestEnvironment() {
		db, err := gorm.Open(sqlite.Open("file::memory:?_journal_mode=WAL&cache=shared"), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %v", err)
		}
		db.Exec("PRAGMA journal_mode = WAL")
		AddDatabaseTables(db)
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

	var db *gorm.DB
	if sqliteDb != "" {
		var err error
		db, err = gorm.Open(sqlite.Open(sqliteDb), &gorm.Config{Logger: customLogger})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %v", err)
		}
	} else {
		postgresDb := fmt.Sprintf(PostgresDb, os.Getenv("POSTGRESQL_PASSWORD"))
		if os.Getenv("HISHTORY_POSTGRES_DB") != "" {
			postgresDb = os.Getenv("HISHTORY_POSTGRES_DB")
		}
		sqltrace.Register("pgx", &stdlib.Driver{}, sqltrace.WithServiceName("hishtory-api"))
		sqlDb, err := sqltrace.Open("pgx", postgresDb)
		if err != nil {
			log.Fatal(err)
		}
		db, err = gormtrace.Open(postgres.New(postgres.Config{Conn: sqlDb}), &gorm.Config{Logger: customLogger})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %v", err)
		}
	}
	AddDatabaseTables(db)
	return db, nil
}

func AddDatabaseTables(db *gorm.DB) {
	db.AutoMigrate(&shared.EncHistoryEntry{})
	db.AutoMigrate(&shared.Device{})
	db.AutoMigrate(&UsageData{})
	db.AutoMigrate(&shared.DumpRequest{})
	db.AutoMigrate(&shared.DeletionRequest{})
	db.AutoMigrate(&shared.Feedback{})
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
	err = cleanDatabase(ctx)
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

func triggerCronHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	err := cron(ctx)
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
		return fmt.Errorf("failed to get latest release version: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read github API response body: %v", err)
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
		return fmt.Errorf("failed to parse github API response: %v", err)
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
		updateInfo.DarwinAmd64Url, updateInfo.DarwinAmd64UnsignedUrl, updateInfo.DarwinAmd64AttestationUrl,
		updateInfo.DarwinArm64Url, updateInfo.DarwinArm64UnsignedUrl, updateInfo.DarwinArm64AttestationUrl}
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to retrieve URL %#v: %v", url, err)
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
		panic(err)
	}
	sqlDb, err := GLOBAL_DB.DB()
	if err != nil {
		panic(err)
	}
	err = sqlDb.Ping()
	if err != nil {
		panic(err)
	}
	if isProductionEnvironment() {
		sqlDb.SetMaxIdleConns(10)
	}
	if isTestEnvironment() {
		sqlDb.SetMaxIdleConns(1)
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
		DarwinAmd64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64", version),
		DarwinAmd64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64-unsigned", version),
		DarwinAmd64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64.intoto.jsonl", version),
		DarwinArm64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64", version),
		DarwinArm64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64-unsigned", version),
		DarwinArm64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64.intoto.jsonl", version),
		Version:                   version,
	}
}

func apiDownloadHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	updateInfo := buildUpdateInfo(ReleaseVersion)
	resp, err := json.Marshal(updateInfo)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func slsaStatusHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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

func feedbackHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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
	checkGormResult(GLOBAL_DB.WithContext(ctx).Create(feedback))

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

func withLogging(h func(context.Context, http.ResponseWriter, *http.Request)) http.Handler {
	logFn := func(rw http.ResponseWriter, r *http.Request) {
		var responseData loggedResponseData
		lrw := loggingResponseWriter{
			ResponseWriter: rw,
			responseData:   &responseData,
		}
		start := time.Now()
		span, ctx := tracer.StartSpanFromContext(
			context.Background(),
			getFunctionName(h),
			tracer.SpanType(ext.SpanTypeSQL),
			tracer.ServiceName("hishtory-api"),
		)
		defer span.Finish()

		h(ctx, &lrw, r)

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

func cleanDatabase(ctx context.Context) error {
	r := GLOBAL_DB.WithContext(ctx).Exec("DELETE FROM enc_history_entries WHERE read_count > 10")
	if r.Error != nil {
		return r.Error
	}
	r = GLOBAL_DB.WithContext(ctx).Exec("DELETE FROM deletion_requests WHERE read_count > 100")
	if r.Error != nil {
		return r.Error
	}
	return nil
}

func deepCleanDatabase(ctx context.Context) {
	err := GLOBAL_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := tx.Exec(`
		CREATE TEMP TABLE temp_users_with_one_device AS (
			SELECT user_id
			FROM devices
			GROUP BY user_id
			HAVING COUNT(DISTINCT device_id) > 1
		)	
		`)
		if r.Error != nil {
			return r.Error
		}
		r = tx.Exec(`
		CREATE TEMP TABLE temp_inactive_users AS (
			SELECT user_id
			FROM usage_data
			WHERE last_used <= (now() - INTERVAL '90 days')
		)	
		`)
		if r.Error != nil {
			return r.Error
		}
		r = tx.Exec(`
		SELECT COUNT(*) FROM enc_history_entries WHERE
			date <= (now() - INTERVAL '90 days')
			AND user_id IN (SELECT * FROM temp_users_with_one_device)
			AND user_id IN (SELECT * FROM temp_inactive_users)
		`)
		if r.Error != nil {
			return r.Error
		}
		fmt.Printf("Ran deep clean and deleted %d rows\n", r.RowsAffected)
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed to deep clean DB: %v", err))
	}
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
		go deepCleanDatabase(context.Background())
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
