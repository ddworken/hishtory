package shared

import "time"

type UsageData struct {
	UserId            string    `json:"user_id" gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	DeviceId          string    `json:"device_id"  gorm:"not null; uniqueIndex:usageDataUniqueIndex"`
	LastUsed          time.Time `json:"last_used"`
	LastIp            string    `json:"last_ip"`
	NumEntriesHandled int       `json:"num_entries_handled"`
	LastQueried       time.Time `json:"last_queried"`
	NumQueries        int       `json:"num_queries"`
	Version           string    `json:"version"`
}
