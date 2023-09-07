package data

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ddworken/hishtory/shared"
	"github.com/google/uuid"
)

const (
	KdfUserID        = "user_id"
	KdfEncryptionKey = "encryption_key"
	CONFIG_PATH      = ".hishtory.config"
	DB_PATH          = ".hishtory.db"
)

const (
	defaultHishtoryPath = ".hishtory"
)

type HistoryEntry struct {
	LocalUsername           string        `json:"local_username" gorm:"uniqueIndex:compositeindex"`
	Hostname                string        `json:"hostname" gorm:"uniqueIndex:compositeindex"`
	Command                 string        `json:"command" gorm:"uniqueIndex:compositeindex"`
	CurrentWorkingDirectory string        `json:"current_working_directory" gorm:"uniqueIndex:compositeindex"`
	HomeDirectory           string        `json:"home_directory" gorm:"uniqueIndex:compositeindex"`
	ExitCode                int           `json:"exit_code" gorm:"uniqueIndex:compositeindex"`
	StartTime               time.Time     `json:"start_time" gorm:"uniqueIndex:compositeindex"`
	EndTime                 time.Time     `json:"end_time" gorm:"uniqueIndex:compositeindex,index:end_time_index"`
	DeviceId                string        `json:"device_id" gorm:"uniqueIndex:compositeindex"`
	CustomColumns           CustomColumns `json:"custom_columns"`
}

type CustomColumns []CustomColumn

type CustomColumn struct {
	Name string `json:"name"`
	Val  string `json:"value"`
}

func (c *CustomColumns) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal CustomColumns value %#v", value)
	}

	return json.Unmarshal(bytes, c)
}

func (c CustomColumns) Value() (driver.Value, error) {
	return json.Marshal(c)
}

func (h *HistoryEntry) GoString() string {
	return fmt.Sprintf("%#v", *h)
}

func sha256hmac(key, additionalData string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(additionalData))
	return h.Sum(nil)
}

func UserId(key string) string {
	return base64.URLEncoding.EncodeToString(sha256hmac(key, KdfUserID))
}

func EncryptionKey(userSecret string) []byte {
	return sha256hmac(userSecret, KdfEncryptionKey)
}

func makeAead(userSecret string) (cipher.AEAD, error) {
	key := EncryptionKey(userSecret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead, nil
}

func Encrypt(userSecret string, data, additionalData []byte) ([]byte, []byte, error) {
	aead, err := makeAead(userSecret)
	if err != nil {
		return []byte{}, []byte{}, fmt.Errorf("failed to make AEAD: %w", err)
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return []byte{}, []byte{}, fmt.Errorf("failed to read a nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, data, additionalData)
	_, err = aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return []byte{}, []byte{}, fmt.Errorf("failed to open AEAD: %w", err)
	}
	return ciphertext, nonce, nil
}

func Decrypt(userSecret string, data, additionalData, nonce []byte) ([]byte, error) {
	aead, err := makeAead(userSecret)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to make AEAD: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, data, additionalData)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to decrypt: %w", err)
	}
	return plaintext, nil
}

func EncryptHistoryEntry(userSecret string, entry HistoryEntry) (shared.EncHistoryEntry, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return shared.EncHistoryEntry{}, err
	}
	ciphertext, nonce, err := Encrypt(userSecret, data, []byte(UserId(userSecret)))
	if err != nil {
		return shared.EncHistoryEntry{}, err
	}
	return shared.EncHistoryEntry{
		EncryptedData: ciphertext,
		Nonce:         nonce,
		UserId:        UserId(userSecret),
		Date:          entry.EndTime,
		EncryptedId:   uuid.Must(uuid.NewRandom()).String(),
		ReadCount:     0,
	}, nil
}

func DecryptHistoryEntry(userSecret string, entry shared.EncHistoryEntry) (HistoryEntry, error) {
	if entry.UserId != UserId(userSecret) {
		return HistoryEntry{}, fmt.Errorf("refusing to decrypt history entry with mismatching UserId")
	}
	plaintext, err := Decrypt(userSecret, entry.EncryptedData, []byte(UserId(userSecret)), entry.Nonce)
	if err != nil {
		return HistoryEntry{}, nil
	}
	var decryptedEntry HistoryEntry
	err = json.Unmarshal(plaintext, &decryptedEntry)
	if err != nil {
		return HistoryEntry{}, nil
	}
	return decryptedEntry, nil
}

func EntryEquals(entry1, entry2 HistoryEntry) bool {
	return entry1.LocalUsername == entry2.LocalUsername &&
		entry1.Hostname == entry2.Hostname &&
		entry1.Command == entry2.Command &&
		entry1.CurrentWorkingDirectory == entry2.CurrentWorkingDirectory &&
		entry1.HomeDirectory == entry2.HomeDirectory &&
		entry1.ExitCode == entry2.ExitCode &&
		entry1.StartTime.Format(time.RFC3339) == entry2.StartTime.Format(time.RFC3339) &&
		entry1.EndTime.Format(time.RFC3339) == entry2.EndTime.Format(time.RFC3339)
}

func GetHishtoryPath() string {
	hishtoryPath := os.Getenv("HISHTORY_PATH")
	if hishtoryPath != "" {
		return hishtoryPath
	}
	return defaultHishtoryPath
}
