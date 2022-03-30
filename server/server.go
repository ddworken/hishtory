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

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var entry shared.HistoryEntry
	err := decoder.Decode(&entry)
	if err != nil {
		panic(err)
	}
	db, err := OpenDB()
	if err != nil {
		panic(err)
	}
	err = shared.Persist(db, entry)
	if err != nil {
		panic(err)
	}
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
	db, err := OpenDB()
	if err != nil {
		panic(err)
	}
	entries, err := shared.Search(db, userSecret, query, limit)
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

func main() {
	// db, err := sql.Open("postgres", "postgresql://postgres:O74Ji4735C@postgres-postgresql.default.svc.cluster.local:5432/cascara_prod?sslmode=disable")
	// if err != nil {
	// 	panic(err)
	// }
	// defer db.Close()

	// _, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s;", "hishtory"))
	// if err != nil {
	// 	panic(err)
	// }

	_, err := OpenDB()
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on localhost:8080")
	http.HandleFunc("/api/v1/submit", apiSubmitHandler)
	http.HandleFunc("/api/v1/search", apiSearchHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
