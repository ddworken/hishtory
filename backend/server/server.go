package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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

func updateUsageData(userId, deviceId string) {
	var usageData []UsageData
	GLOBAL_DB.Where("user_id = ? AND device_id = ?", userId, deviceId).Find(&usageData)
	if len(usageData) == 0 {
		GLOBAL_DB.Create(&UsageData{UserId: userId, DeviceId: deviceId, LastUsed: time.Now()})
	} else {
		GLOBAL_DB.Model(&UsageData{}).Where("user_id = ? AND device_id = ?", userId, deviceId).Update("last_used", time.Now())
	}
}

func apiESubmitHandler(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var entries []shared.EncHistoryEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		panic(fmt.Sprintf("body=%#v, err=%v", data, err))
	}
	fmt.Printf("apiESubmitHandler: received request containg %d EncHistoryEntry\n", len(entries))
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
		fmt.Printf("apiESubmitHandler: Found %d devices\n", len(devices))
		for _, device := range devices {
			entry.DeviceId = device.DeviceId
			result := GLOBAL_DB.Create(&entry)
			if result.Error != nil {
				panic(result.Error)
			}
		}
	}
}

func apiEQueryHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.URL.Query().Get("user_id")
	deviceId := r.URL.Query().Get("device_id")
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
	fmt.Printf("apiEQueryHandler: Found %d entries\n", len(historyEntries))
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

// TODO: bootstrap is a janky solution for the initial version of this. Long term, need to support deleting entries from the DB which means replacing bootstrap with a queued message sent to any live instances.
func apiEBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.URL.Query().Get("user_id")
	deviceId := r.URL.Query().Get("device_id")
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

func apiERegisterHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.URL.Query().Get("user_id")
	deviceId := r.URL.Query().Get("device_id")
	GLOBAL_DB.Create(&shared.Device{UserId: userId, DeviceId: deviceId, RegistrationIp: r.RemoteAddr, RegistrationDate: time.Now()})
	updateUsageData(userId, deviceId)
}

func apiBannerHandler(w http.ResponseWriter, r *http.Request) {
	commitHash := r.URL.Query().Get("commit_hash")
	deviceId := r.URL.Query().Get("device_id")
	forcedBanner := r.URL.Query().Get("forced_banner")
	fmt.Printf("apiBannerHandler: commit_hash=%#v, device_id=%#v, forced_banner=%#v\n", commitHash, deviceId, forcedBanner)
	w.Write([]byte(forcedBanner))
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
		return db, nil
	}

	db, err := gorm.Open(postgres.Open(PostgresDb), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %v", err)
	}
	db.AutoMigrate(&shared.EncHistoryEntry{})
	db.AutoMigrate(&shared.Device{})
	db.AutoMigrate(&UsageData{})
	return db, nil
}

func init() {
	if ReleaseVersion == "UNKNOWN" && !isTestEnvironment() {
		panic("server.go was built without a ReleaseVersion!")
	}
	InitDB()
	go runBackgroundJobs()
}

func runBackgroundJobs() {
	for {
		err := updateReleaseVersion()
		if err != nil {
			fmt.Println(err)
		}
		err = cleanDatabase()
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(10 * time.Minute)
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
	ReleaseVersion = info.Name
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

func bindaryDownloadHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64", ReleaseVersion), http.StatusFound)
}

func attestationDownloadHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64.intoto.jsonl", ReleaseVersion), http.StatusFound)
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
	return nil
}

func main() {
	fmt.Println("Listening on localhost:8080")
	http.Handle("/api/v1/esubmit", withLogging(apiESubmitHandler))
	http.Handle("/api/v1/equery", withLogging(apiEQueryHandler))
	http.Handle("/api/v1/ebootstrap", withLogging(apiEBootstrapHandler))
	http.Handle("/api/v1/eregister", withLogging(apiERegisterHandler))
	http.Handle("/api/v1/banner", withLogging(apiBannerHandler))
	http.Handle("/download/hishtory-linux-amd64", withLogging(bindaryDownloadHandler))
	http.Handle("/download/hishtory-linux-amd64.intoto.jsonl", withLogging(attestationDownloadHandler))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
