package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"github.com/rodaine/table"
	"gorm.io/driver/postgres"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	PostgresDb = "postgresql://postgres:%s@postgres:5432/hishtory?sslmode=disable"
)

var (
	GLOBAL_DB      *gorm.DB
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

func updateUsageData(r *http.Request, userId, deviceId string, numEntriesHandled int, isQuery bool) {
	var usageData []UsageData
	GLOBAL_DB.Where("user_id = ? AND device_id = ?", userId, deviceId).Find(&usageData)
	if len(usageData) == 0 {
		GLOBAL_DB.Create(&UsageData{UserId: userId, DeviceId: deviceId, LastUsed: time.Now(), NumEntriesHandled: numEntriesHandled, Version: getHishtoryVersion(r)})
	} else {
		usage := usageData[0]
		GLOBAL_DB.Model(&UsageData{}).Where("user_id = ? AND device_id = ?", userId, deviceId).Update("last_used", time.Now()).Update("last_ip", getRemoteAddr(r))
		if numEntriesHandled > 0 {
			GLOBAL_DB.Exec("UPDATE usage_data SET num_entries_handled = COALESCE(num_entries_handled, 0) + ? WHERE user_id = ? AND device_id = ?", numEntriesHandled, userId, deviceId)
		}
		if usage.Version != getHishtoryVersion(r) {
			GLOBAL_DB.Exec("UPDATE usage_data SET version = ? WHERE user_id = ? AND device_id = ?", getHishtoryVersion(r), userId, deviceId)
		}
	}
	if isQuery {
		GLOBAL_DB.Exec("UPDATE usage_data SET num_queries = COALESCE(num_queries, 0) + 1, last_queried = ? WHERE user_id = ? AND device_id = ?", time.Now(), userId, deviceId)
	}
}

func usageStatsHandler(w http.ResponseWriter, r *http.Request) {
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
	rows, err := GLOBAL_DB.Raw(query).Rows()
	if err != nil {
		panic(err)
	}
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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	var numDevices int64 = 0
	checkGormResult(GLOBAL_DB.Model(&shared.Device{}).Count(&numDevices))
	type numEntriesProcessed struct {
		Total int
	}
	nep := numEntriesProcessed{}
	checkGormResult(GLOBAL_DB.Model(&UsageData{}).Select("SUM(num_entries_handled) as total").Find(&nep))
	var numDbEntries int64 = 0
	checkGormResult(GLOBAL_DB.Model(&shared.EncHistoryEntry{}).Count(&numDbEntries))

	lastWeek := time.Now().AddDate(0, 0, -7)
	var weeklyActiveInstalls int64 = 0
	checkGormResult(GLOBAL_DB.Model(&UsageData{}).Where("last_used > ?", lastWeek).Count(&weeklyActiveInstalls))
	var weeklyQueryUsers int64 = 0
	checkGormResult(GLOBAL_DB.Model(&UsageData{}).Where("last_queried > ?", lastWeek).Count(&weeklyQueryUsers))
	w.Write([]byte(fmt.Sprintf("Num devices: %d\n", numDevices)))
	w.Write([]byte(fmt.Sprintf("Num history entries processed: %d\n", nep.Total)))
	w.Write([]byte(fmt.Sprintf("Num DB entries: %d\n", numDbEntries)))
	w.Write([]byte(fmt.Sprintf("Weekly active installs: %d\n", weeklyActiveInstalls)))
	w.Write([]byte(fmt.Sprintf("Weekly active queries: %d\n", weeklyQueryUsers)))
}

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
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
	updateUsageData(r, entries[0].UserId, entries[0].DeviceId, len(entries), false)
	tx := GLOBAL_DB.Where("user_id = ?", entries[0].UserId)
	var devices []*shared.Device
	checkGormResult(tx.Find(&devices))
	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entries[0].UserId))
	}
	fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))
	for _, device := range devices {
		for _, entry := range entries {
			entry.DeviceId = device.DeviceId
		}
		checkGormResult(GLOBAL_DB.Create(&entries))
	}
}

func apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	updateUsageData(r, userId, deviceId, 0, false)
	tx := GLOBAL_DB.Where("user_id = ?", userId)
	var historyEntries []*shared.EncHistoryEntry
	checkGormResult(tx.Find(&historyEntries))
	fmt.Printf("apiBootstrapHandler: Found %d entries\n", len(historyEntries))
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func apiQueryHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	updateUsageData(r, userId, deviceId, 0, true)
	// Increment the count
	checkGormResult(GLOBAL_DB.Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceId))

	// Delete any entries that match a pending deletion request
	var deletionRequests []*shared.DeletionRequest
	checkGormResult(GLOBAL_DB.Where("destination_device_id = ? AND user_id = ?", deviceId, userId).Find(&deletionRequests))
	for _, request := range deletionRequests {
		_, err := applyDeletionRequestsToBackend(*request)
		if err != nil {
			panic(err)
		}
	}

	// Then retrieve, to avoid a race condition
	tx := GLOBAL_DB.Where("device_id = ? AND read_count < 5", deviceId)
	var historyEntries []*shared.EncHistoryEntry
	checkGormResult(tx.Find(&historyEntries))
	fmt.Printf("apiQueryHandler: Found %d entries for %s\n", len(historyEntries), r.URL)
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func getRemoteAddr(r *http.Request) string {
	addr, ok := r.Header["X-Real-Ip"]
	if !ok || len(addr) == 0 {
		return "UnknownIp"
	}
	return addr[0]
}

func apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var existingDevicesCount int64 = -1
	checkGormResult(GLOBAL_DB.Model(&shared.Device{}).Where("user_id = ?", userId).Count(&existingDevicesCount))
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	checkGormResult(GLOBAL_DB.Create(&shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: getRemoteAddr(r), RegistrationDate: time.Now()}))
	if existingDevicesCount > 0 {
		checkGormResult(GLOBAL_DB.Create(&shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()}))
	}
	updateUsageData(r, userId, deviceId, 0, false)
}

func apiGetPendingDumpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	checkGormResult(GLOBAL_DB.Where("user_id = ? AND requesting_device_id != ?", userId, deviceId).Find(&dumpRequests))
	respBody, err := json.Marshal(dumpRequests)
	if err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %v", err))
	}
	w.Write(respBody)
}

func apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	srcDeviceId := getRequiredQueryParam(r, "source_device_id")
	requestingDeviceId := getRequiredQueryParam(r, "requesting_device_id")
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var entries []shared.EncHistoryEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiSubmitDumpHandler: received request containg %d EncHistoryEntry\n", len(entries))
	err = GLOBAL_DB.Transaction(func(tx *gorm.DB) error {
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
	checkGormResult(GLOBAL_DB.Delete(&shared.DumpRequest{}, "user_id = ? AND requesting_device_id = ?", userId, requestingDeviceId))
	updateUsageData(r, userId, srcDeviceId, len(entries), false)
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
	checkGormResult(GLOBAL_DB.Exec("UPDATE deletion_requests SET read_count = read_count + 1 WHERE destination_device_id = ? AND user_id = ?", deviceId, userId))

	// Return all the deletion requests
	var deletionRequests []*shared.DeletionRequest
	checkGormResult(GLOBAL_DB.Where("user_id = ? AND destination_device_id = ?", userId, deviceId).Find(&deletionRequests))
	respBody, err := json.Marshal(deletionRequests)
	if err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %v", err))
	}
	w.Write(respBody)
}

func addDeletionRequestHandler(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
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
	tx := GLOBAL_DB.Where("user_id = ?", request.UserId)
	var devices []*shared.Device
	checkGormResult(tx.Find(&devices))
	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", request.UserId))
	}
	fmt.Printf("addDeletionRequestHandler: Found %d devices\n", len(devices))
	for _, device := range devices {
		request.DestinationDeviceId = device.DeviceId
		checkGormResult(GLOBAL_DB.Create(&request))
	}

	// Also delete anything currently in the DB matching it
	numDeleted, err := applyDeletionRequestsToBackend(request)
	if err != nil {
		panic(err)
	}
	fmt.Printf("addDeletionRequestHandler: Deleted %d rows in the backend\n", numDeleted)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	var count int64
	checkGormResult(GLOBAL_DB.Model(&shared.EncHistoryEntry{}).Count(&count))
	if count < 100 {
		panic("Suspiciously few enc history entries!")
	}
	checkGormResult(GLOBAL_DB.Model(&shared.Device{}).Count(&count))
	if count < 50 {
		panic("Suspiciously few devices!")
	}
	ok := "OK"
	w.Write([]byte(ok))
}

func applyDeletionRequestsToBackend(request shared.DeletionRequest) (int, error) {
	tx := GLOBAL_DB.Where("false")
	for _, message := range request.Messages.Ids {
		tx = tx.Or(GLOBAL_DB.Where("user_id = ? AND device_id = ? AND date = ?", request.UserId, message.DeviceId, message.Date))
	}
	result := tx.Delete(&shared.EncHistoryEntry{})
	checkGormResult(result)
	return int(result.RowsAffected), nil
}

func wipeDbHandler(w http.ResponseWriter, r *http.Request) {
	checkGormResult(GLOBAL_DB.Exec("DELETE FROM enc_history_entries"))
}

func isTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func OpenDB() (*gorm.DB, error) {
	if isTestEnvironment() {
		db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to the DB: %v", err)
		}
		db.AutoMigrate(&shared.EncHistoryEntry{})
		db.AutoMigrate(&shared.Device{})
		db.AutoMigrate(&UsageData{})
		db.AutoMigrate(&shared.DumpRequest{})
		db.AutoMigrate(&shared.DeletionRequest{})
		db.Exec("PRAGMA journal_mode = WAL")
		return db, nil
	}

	var sqliteDb string
	if os.Getenv("HISHTORY_SQLITE_DB") != "" {
		sqliteDb = os.Getenv("HISHTORY_SQLITE_DB")
	}

	var db *gorm.DB
	var err error

	if sqliteDb != "" {
		db, err = gorm.Open(sqlite.Open(sqliteDb), &gorm.Config{})
	} else {
		postgresDb := fmt.Sprintf(PostgresDb, os.Getenv("POSTGRESQL_PASSWORD"))
		if os.Getenv("HISHTORY_POSTGRES_DB") != "" {
			postgresDb = os.Getenv("HISHTORY_POSTGRES_DB")
		}
		db, err = gorm.Open(postgres.Open(postgresDb), &gorm.Config{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %v", err)
	}

	db.AutoMigrate(&shared.EncHistoryEntry{})
	db.AutoMigrate(&shared.Device{})
	db.AutoMigrate(&UsageData{})
	db.AutoMigrate(&shared.DumpRequest{})
	db.AutoMigrate(&shared.DeletionRequest{})
	return db, nil
}

func init() {
	if ReleaseVersion == "UNKNOWN" && !isTestEnvironment() {
		panic("server.go was built without a ReleaseVersion!")
	}
	InitDB()
	go runBackgroundJobs()
}

func cron() error {
	err := updateReleaseVersion()
	if err != nil {
		fmt.Println(err)
	}
	err = cleanDatabase()
	if err != nil {
		fmt.Println(err)
	}
	return nil
}

func runBackgroundJobs() {
	time.Sleep(5 * time.Second)
	for {
		err := cron()
		if err != nil {
			fmt.Printf("Cron failure: %v", err)
		}
		time.Sleep(10 * time.Minute)
	}
}

func triggerCronHandler(w http.ResponseWriter, r *http.Request) {
	err := cron()
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
	respBody, err := ioutil.ReadAll(resp.Body)
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
	urls := []string{updateInfo.LinuxAmd64Url, updateInfo.LinuxAmd64AttestationUrl,
		updateInfo.DarwinAmd64Url, updateInfo.DarwinAmd64UnsignedUrl, updateInfo.DarwinAmd64AttestationUrl,
		updateInfo.DarwinArm64Url, updateInfo.DarwinArm64UnsignedUrl, updateInfo.DarwinArm64AttestationUrl}
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to retrieve URL %#v: %v", url, err)
		}
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
	tx, err := GLOBAL_DB.DB()
	if err != nil {
		panic(err)
	}
	err = tx.Ping()
	if err != nil {
		panic(err)
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

func withLogging(h func(http.ResponseWriter, *http.Request)) http.Handler {
	logFn := func(rw http.ResponseWriter, r *http.Request) {
		var responseData loggedResponseData
		lrw := loggingResponseWriter{
			ResponseWriter: rw,
			responseData:   &responseData,
		}
		start := time.Now()

		h(&lrw, r)

		duration := time.Since(start)
		fmt.Printf("%s %s %#v %s %s %s\n", getRemoteAddr(r), r.Method, r.RequestURI, getHishtoryVersion(r), duration.String(), byteCountToString(responseData.size))
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

func cleanDatabase() error {
	checkGormResult(GLOBAL_DB.Exec("DELETE FROM enc_history_entries WHERE read_count > 10"))
	checkGormResult(GLOBAL_DB.Exec("DELETE FROM deletion_requests WHERE read_count > 100"))
	// TODO(optimization): Clean the database by deleting entries for users that haven't been used in X amount of time
	return nil
}

func main() {
	fmt.Println("Listening on localhost:8080")
	http.Handle("/api/v1/submit", withLogging(apiSubmitHandler))
	http.Handle("/api/v1/get-dump-requests", withLogging(apiGetPendingDumpRequestsHandler))
	http.Handle("/api/v1/submit-dump", withLogging(apiSubmitDumpHandler))
	http.Handle("/api/v1/query", withLogging(apiQueryHandler))
	http.Handle("/api/v1/bootstrap", withLogging(apiBootstrapHandler))
	http.Handle("/api/v1/register", withLogging(apiRegisterHandler))
	http.Handle("/api/v1/banner", withLogging(apiBannerHandler))
	http.Handle("/api/v1/download", withLogging(apiDownloadHandler))
	http.Handle("/api/v1/trigger-cron", withLogging(triggerCronHandler))
	http.Handle("/api/v1/get-deletion-requests", withLogging(getDeletionRequestsHandler))
	http.Handle("/api/v1/add-deletion-request", withLogging(addDeletionRequestHandler))
	http.Handle("/api/v1/slsa-status", withLogging(slsaStatusHandler))
	http.Handle("/healthcheck", withLogging(healthCheckHandler))
	http.Handle("/internal/api/v1/usage-stats", withLogging(usageStatsHandler))
	http.Handle("/internal/api/v1/stats", withLogging(statsHandler))
	if isTestEnvironment() {
		http.Handle("/api/v1/wipe-db", withLogging(wipeDbHandler))
	}
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func checkGormResult(result *gorm.DB) {
	if result.Error != nil {
		_, filename, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("DB error at %s:%d: %v", filename, line, result.Error))
	}
}

// TODO(optimization): Maybe optimize the endpoints a bit to reduce the number of round trips required?
