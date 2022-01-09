package shared

import "time"

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
