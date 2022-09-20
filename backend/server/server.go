package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	// This password is for the postgres cluster running in my k8s cluster that is not publicly accessible. Some day I'll migrate it out of the code and rotate the password, but until then, this is fine.
	PostgresDb = "postgresql://postgres:O74Ji4735C@postgres-postgresql.default.svc.cluster.local:5432/hishtory?sslmode=disable"
)

var (
	GLOBAL_DB      *gorm.DB
	ReleaseVersion string = "UNKNOWN"
)

type UsageData struct {
	UserId   string    `json:"user_id" gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	DeviceId string    `json:"device_id"  gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	LastUsed time.Time `json:"last_used"`
}

func getRequiredQueryParam(r *http.Request, queryParam string) string {
	val := r.URL.Query().Get(queryParam)
	if val == "" {
		panic(fmt.Sprintf("request to %s is missing required query param=%#v", r.URL, queryParam))
	}
	return val
}

func updateUsageData(userId, deviceId string) {
	var usageData []UsageData
	GLOBAL_DB.Where("user_id = ? AND device_id = ?", userId, deviceId).Find(&usageData)
	if len(usageData) == 0 {
		GLOBAL_DB.Create(&UsageData{UserId: userId, DeviceId: deviceId, LastUsed: time.Now()})
	} else {
		GLOBAL_DB.Model(&UsageData{}).Where("user_id = ? AND device_id = ?", userId, deviceId).Update("last_used", time.Now())
	}
}

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var entries []shared.EncHistoryEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiSubmitHandler: received request containg %d EncHistoryEntry\n", len(entries))
	for _, entry := range entries {
		updateUsageData(entry.UserId, entry.DeviceId)
		tx := GLOBAL_DB.Where("user_id = ?", entry.UserId)
		var devices []*shared.Device
		result := tx.Find(&devices)
		if result.Error != nil {
			panic(fmt.Errorf("DB query error: %v", result.Error))
		}
		if len(devices) == 0 {
			panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", entry.UserId))
		}
		fmt.Printf("apiSubmitHandler: Found %d devices\n", len(devices))
		for _, device := range devices {
			entry.DeviceId = device.DeviceId
			result := GLOBAL_DB.Create(&entry)
			if result.Error != nil {
				panic(result.Error)
			}
		}
	}
}

func apiBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	updateUsageData(userId, deviceId)
	tx := GLOBAL_DB.Where("user_id = ?", userId)
	var historyEntries []*shared.EncHistoryEntry
	result := tx.Find(&historyEntries)
	if result.Error != nil {
		panic(fmt.Errorf("DB query error: %v", result.Error))
	}
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

func apiQueryHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	updateUsageData(userId, deviceId)
	// Increment the count
	GLOBAL_DB.Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceId)

	// Then retrieve, to avoid a race condition
	tx := GLOBAL_DB.Where("device_id = ? AND read_count < 5", deviceId)
	var historyEntries []*shared.EncHistoryEntry
	result := tx.Find(&historyEntries)
	if result.Error != nil {
		panic(fmt.Errorf("DB query error: %v", result.Error))
	}
	fmt.Printf("apiQueryHandler: Found %d entries\n", len(historyEntries))
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)

	// TODO: Make thsi method also check the pending deletion requests
	// And then can delete the extra round trip of doing processDeletionRequests() after pulling from the remote
}

func apiRegisterHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var existingDevicesCount int64 = -1
	result := GLOBAL_DB.Model(&shared.Device{}).Where("user_id = ?", userId).Count(&existingDevicesCount)
	fmt.Printf("apiRegisterHandler: existingDevicesCount=%d\n", existingDevicesCount)
	if result.Error != nil {
		panic(result.Error)
	}
	GLOBAL_DB.Create(&shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: r.RemoteAddr, RegistrationDate: time.Now()})
	if existingDevicesCount > 0 {
		GLOBAL_DB.Create(&shared.DumpRequest{UserId: userId, RequestingDeviceId: deviceId, RequestTime: time.Now()})
	}
	updateUsageData(userId, deviceId)
}

func apiGetPendingDumpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var dumpRequests []*shared.DumpRequest
	// Filter out ones requested by the hishtory instance that sent this request
	result := GLOBAL_DB.Where("user_id = ? AND requesting_device_id != ?", userId, deviceId).Find(&dumpRequests)
	if result.Error != nil {
		panic(fmt.Errorf("DB query error: %v", result.Error))
	}
	respBody, err := json.Marshal(dumpRequests)
	if err != nil {
		panic(fmt.Errorf("failed to JSON marshall the dump requests: %v", err))
	}
	w.Write(respBody)
}

func apiSubmitDumpHandler(w http.ResponseWriter, r *http.Request) {
	userId := getRequiredQueryParam(r, "user_id")
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
			result := tx.Create(&entry)
			if result.Error != nil {
				return fmt.Errorf("failed to create entry: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed to execute transaction to add dumped DB: %v", err))
	}
	result := GLOBAL_DB.Delete(&shared.DumpRequest{}, "user_id = ? AND requesting_device_id = ?", userId, requestingDeviceId)
	if result.Error != nil {
		panic(fmt.Errorf("failed to clear the dump request: %v", err))
	}
}

func apiBannerHandler(w http.ResponseWriter, r *http.Request) {
	commitHash := getRequiredQueryParam(r, "commit_hash")
	deviceId := getRequiredQueryParam(r, "device_id")
	forcedBanner := r.URL.Query().Get("forced_banner")
	fmt.Printf("apiBannerHandler: commit_hash=%#v, device_id=%#v, forced_banner=%#v\n", commitHash, deviceId, forcedBanner)
	w.Write([]byte(forcedBanner))
}

func getDeletionRequestsHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Count how many times they've been read and eventually delete them
	userId := getRequiredQueryParam(r, "user_id")
	deviceId := getRequiredQueryParam(r, "device_id")
	var deletionRequests []*shared.DeletionRequest
	result := GLOBAL_DB.Where("user_id = ? AND destination_device_id = ?", userId, deviceId).Find(&deletionRequests)
	if result.Error != nil {
		panic(fmt.Errorf("DB query error: %v", result.Error))
	}
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
	fmt.Printf("addDeletionRequestHandler: received request containg %d messages to be deleted\n", len(request.Messages.Ids))

	// Store the deletion request so all the devices will get it
	tx := GLOBAL_DB.Where("user_id = ?", request.UserId)
	var devices []*shared.Device
	result := tx.Find(&devices)
	if result.Error != nil {
		panic(fmt.Errorf("DB query error: %v", result.Error))
	}
	if len(devices) == 0 {
		panic(fmt.Errorf("found no devices associated with user_id=%s, can't save history entry", request.UserId))
	}
	fmt.Printf("addDeletionRequestHandler: Found %d devices\n", len(devices))
	for _, device := range devices {
		request.DestinationDeviceId = device.DeviceId
		result := GLOBAL_DB.Create(&request)
		if result.Error != nil {
			panic(result.Error)
		}
	}

	// Also delete anything currently in the DB matching it
	numDeleted := 0
	for _, message := range request.Messages.Ids {
		// TODO: Optimize this into one query
		tx = GLOBAL_DB.Where("user_id = ? AND device_id = ? AND date = ?", request.UserId, message.DeviceId, message.Date)
		result := tx.Delete(&shared.EncHistoryEntry{})
		if result.Error != nil {
			panic(result.Error)
		}
		numDeleted += int(result.RowsAffected)
	}
	fmt.Printf("addDeletionRequestHandler: Deleted %d rows in the backend\n", numDeleted)
}

func wipeDbHandler(w http.ResponseWriter, r *http.Request) {
	result := GLOBAL_DB.Exec("DELETE FROM enc_history_entries")
	if result.Error != nil {
		panic(result.Error)
	}
}

func isTestEnvironment() bool {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	return os.Getenv("HISHTORY_TEST") != "" || u.Username == "david"
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

	db, err := gorm.Open(postgres.Open(PostgresDb), &gorm.Config{})
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
		fmt.Printf("%s %s %#v %s %s\n", r.RemoteAddr, r.Method, r.RequestURI, duration.String(), byteCountToString(responseData.size))
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
	result := GLOBAL_DB.Exec("DELETE FROM enc_history_entries WHERE read_count > 10")
	if result.Error != nil {
		return result.Error
	}
	// TODO: Clean the database by deleting entries for users that haven't been used in X amount of time
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
	if isTestEnvironment() {
		http.Handle("/api/v1/wipe-db", withLogging(wipeDbHandler))
	}
	log.Fatal(http.ListenAndServe(":8080", nil))
}
