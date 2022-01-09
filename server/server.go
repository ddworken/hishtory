package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/ddworken/hishtory/shared"
)

func search(db *gorm.DB, userSecret, query string, limit int) ([]*shared.HistoryEntry, error) {
	fmt.Println("Received search query: " + query)
	tokens, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %v", err)
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
	if query == "" {
		return []string{}, nil
	}
	return strings.Split(query, " "), nil
}

func apiSubmitHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var entry shared.HistoryEntry
	err := decoder.Decode(&entry)
	if err != nil {
		panic(err)
	}
	err = shared.Persist(entry)
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
	db, err := shared.OpenDB()
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
	_, err := shared.OpenDB()
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on localhost:8080")
	http.HandleFunc("/api/v1/submit", apiSubmitHandler)
	http.HandleFunc("/api/v1/search", apiSearchHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
