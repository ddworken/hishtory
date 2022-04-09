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
	// The IP address that was used to register the device. Recorded so
	// that I can count how many people are using hishtory and roughly
	// from where. If you would like this deleted, please email me at
	// david@daviddworken.com and I can clear it from your device entries.
	RegistrationIp   string    `json:"registration_ip"`
	RegistrationDate time.Time `json:"registration_date"`
}

const (
	CONFIG_PATH   = ".hishtory.config"
	HISHTORY_PATH = ".hishtory"
	DB_PATH       = ".hishtory.db"
)
