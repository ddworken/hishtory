package shared

import (
	"time"
)

type EncHistoryEntry struct {
	EncryptedData []byte    `json:"enc_data"`
	Nonce         []byte    `json:"nonce"`
	DeviceId      string    `json:"device_id"`
	UserId        string    `json:"user_id"`
	Date          time.Time `json:"time"`
	EncryptedId   string    `json:"id"`
	ReadCount     int       `json:"read_count"`
}

type Device struct {
	UserId   string `json:"user_id"`
	DeviceId string `json:"device_id"`
}

const (
	CONFIG_PATH   = ".hishtory.config"
	HISHTORY_PATH = ".hishtory"
	DB_PATH       = ".hishtory.db"
)
