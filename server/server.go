package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/ddworken/hishtory/shared"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	POSTGRES_DB = "postgresql://postgres:O74Ji4735C@postgres-postgresql.default.svc.cluster.local:5432/hishtory?sslmode=disable"
)

var GLOBAL_DB *gorm.DB

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var entry shared.HistoryEntry
	err := decoder.Decode(&entry)
	if err != nil {
		panic(err)
	}
	GLOBAL_DB.Create(&entry)
}

func apiESubmitHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var entries []shared.EncHistoryEntry
	err := decoder.Decode(&entries)
	if err != nil {
		panic(err)
	}
	GLOBAL_DB.Where("user_id = ?")
	for _, entry := range entries {
		tx := GLOBAL_DB.Where("user_id = ?", entry.UserId)
		var devices []*shared.Device
		result := tx.Find(&devices)
		if result.Error != nil {
			panic(fmt.Errorf("DB query error: %v", result.Error))
		}
		if len(devices) == 0 {
			panic(fmt.Errorf("Found no devices associated with user_id=%s, can't save history entry!", entry.UserId))
		}
		for _, device := range devices {
			entry.DeviceId = device.DeviceId
			GLOBAL_DB.Create(&entry)
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
	resp, err := json.Marshal(historyEntries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
}

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

func OpenDB() (*gorm.DB, error) {
	if shared.IsTestEnvironment() {
		return shared.OpenLocalSqliteDb()
	}

	db, err := gorm.Open(postgres.Open(POSTGRES_DB), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %v", err)
	}
	db.AutoMigrate(&shared.HistoryEntry{})
	db.AutoMigrate(&shared.EncHistoryEntry{})
	db.AutoMigrate(&shared.Device{})
	return db, nil
}

func apiSearchHandler(w http.ResponseWriter, r *http.Request) {
	userSecret := r.URL.Query().Get("user_secret")
	query := r.URL.Query().Get("query")
	fmt.Println("Received search query: " + query)
	limitStr := r.URL.Query().Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 0
	}
	entries, err := shared.Search(GLOBAL_DB, userSecret, query, limit)
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		entry.UserSecret = ""
	}
	resp, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	w.Write(resp)
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
	http.HandleFunc("/api/v1/submit", apiSubmitHandler)
	http.HandleFunc("/api/v1/search", apiSearchHandler)
	http.HandleFunc("/api/v1/esubmit", apiESubmitHandler)
	http.HandleFunc("/api/v1/equery", apiEQueryHandler)
	http.HandleFunc("/api/v1/ebootstrap", apiEBootstrapHandler)
	http.HandleFunc("/api/v1/eregister", apiERegisterHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
