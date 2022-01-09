package shared

import (
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type HistoryEntry struct {
	UserSecret              string    `json:"user_secret"`
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
