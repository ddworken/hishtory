package shared

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type HistoryEntry struct {
	UserSecret              string    `json:"user_secret" gorm:"index"`
	LocalUsername           string    `json:"local_username"`
	Hostname                string    `json:"hostname"`
	Command                 string    `json:"command"`
	CurrentWorkingDirectory string    `json:"current_working_directory"`
	ExitCode                int       `json:"exit_code"`
	StartTime               time.Time `json:"start_time"`
	EndTime                 time.Time `json:"end_time"`
}

const (
	DB_PATH = ".hishtory.db"
)

func Persist(entry HistoryEntry) error {
	log.Printf("Saving %#v to the DB\n", entry)
	db, err := OpenDB()
	if err != nil {
		return err
	}
	conn, err := db.DB()
	defer conn.Close()
	db.Create(&entry).Commit()
	return nil
}

func OpenDB() (*gorm.DB, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user's home directory: %v", err)
	}
	db, err := gorm.Open(sqlite.Open(path.Join(homedir, DB_PATH)), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	db.AutoMigrate(&HistoryEntry{})
	return db, nil
}

func Search(db *gorm.DB, userSecret, query string, limit int) ([]*HistoryEntry, error) {
	tokens, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %v", err)
	}
	tx := db.Where("user_secret = ?", userSecret)
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
	var historyEntries []*HistoryEntry
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
