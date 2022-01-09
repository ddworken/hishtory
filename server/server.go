package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/ddworken/hishtory/shared"
)

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
	query := r.URL.Query().Get("query")
	fmt.Println("Received search query: " + query)
	limitStr := r.URL.Query().Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 0
	}
	db, err := shared.OpenDB()
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
	_, err := shared.OpenDB()
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on localhost:8080")
	http.HandleFunc("/api/v1/submit", apiSubmitHandler)
	http.HandleFunc("/api/v1/search", apiSearchHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
