package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ddworken/hishtory/shared"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openDB() (*gorm.DB, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user's home directory: %v", err)
	}
	db, err := gorm.Open(sqlite.Open(path.Join(homedir, ".hishtory.db")), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	db.AutoMigrate(&shared.HistoryEntry{})
	return db, nil
}

func persist(entry shared.HistoryEntry) error {
	log.Printf("Saving %#v to the DB\n", entry)
	db, err := openDB()
	if err != nil {
		return err
	}
	conn, err := db.DB()
	defer conn.Close()
	db.Create(&entry).Commit()
	return nil
}

func search(db *gorm.DB, userSecret, query string, limit int) ([]*shared.HistoryEntry, error) {
	fmt.Println("Received search query: " + query)
	tokens, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %v")
	}
	tx := db.Debug().Where("user_secret = ?", userSecret)
	for _, token := range tokens {
		if strings.Contains(token, ":") {
			splitToken := strings.SplitN(token, ":", 2)
			field := splitToken[0]
			val := splitToken[1]
			// tx = tx.Where()
			panic("TODO(ddworken): Use " + field + val)
		} else {
			wildcardedToken := "%" + token + "%"
			tx = tx.Where("(command LIKE ? OR hostname LIKE ? OR current_working_directory LIKE ?)", wildcardedToken, wildcardedToken, wildcardedToken)
		}
	}
	tx = tx.Order("end_time DESC")
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	var historyEntries []*shared.HistoryEntry
	result := tx.Find(&historyEntries)
	if result.Error != nil {
		return nil, fmt.Errorf("DB query error: %v", result.Error)
	}
	return historyEntries, nil
}

func tokenize(query string) ([]string, error) {
	return strings.Split(query, " "), nil
}

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var entry shared.HistoryEntry
	err := decoder.Decode(&entry)
	if err != nil {
		panic(err)
	}
	err = persist(entry)
	if err != nil {
		panic(err)
	}
}

func apiSearchHandler(w http.ResponseWriter, r *http.Request) {
	userSecret := r.URL.Query().Get("user_secret")
	if userSecret == "" {
		panic("cannot search without specifying a user secret")
	}
	query := r.URL.Query().Get("query")
	limitStr := r.URL.Query().Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 0
	}
	db, err := openDB()
	if err != nil {
		panic(err)
	}
	entries, err := search(db, userSecret, query, limit)
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
	_, err := openDB()
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on localhost:8080")
	http.HandleFunc("/api/v1/submit", apiSubmitHandler)
	http.HandleFunc("/api/v1/search", apiSearchHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
