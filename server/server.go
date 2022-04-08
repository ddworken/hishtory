package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	POSTGRES_DB = "postgresql://postgres:O74Ji4735C@postgres-postgresql.default.svc.cluster.local:5432/hishtory?sslmode=disable"
)

var GLOBAL_DB *gorm.DB

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
	GLOBAL_DB.Where("user_id = ?")
	fmt.Printf("apiESubmitHandler: received request containg %d EncHistoryEntry\n", len(entries))
	for _, entry := range entries {
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
	deviceId := r.URL.Query().Get("device_id")
	// Increment the count
	GLOBAL_DB.Exec("UPDATE enc_history_entries SET read_count = read_count + 1 WHERE device_id = ?", deviceId)

	// Then retrieve, to avoid a race condition
	tx := GLOBAL_DB.Where("device_id = ?", deviceId)
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
	GLOBAL_DB.Create(&shared.Device{UserId: userId, DeviceId: deviceId})
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
		return db, nil
	}

	db, err := gorm.Open(postgres.Open(POSTGRES_DB), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %v", err)
	}
	db.AutoMigrate(&shared.EncHistoryEntry{})
	db.AutoMigrate(&shared.Device{})
	return db, nil
}

func init() {
	InitDB()
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

func main() {
	fmt.Println("Listening on localhost:8080")
	http.HandleFunc("/api/v1/esubmit", apiESubmitHandler)
	http.HandleFunc("/api/v1/equery", apiEQueryHandler)
	http.HandleFunc("/api/v1/ebootstrap", apiEBootstrapHandler)
	http.HandleFunc("/api/v1/eregister", apiERegisterHandler)
	http.HandleFunc("/api/v1/banner", apiBannerHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
