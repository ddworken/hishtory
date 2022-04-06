package shared

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type HistoryEntry struct {
	LocalUsername           string    `json:"local_username" gorm:"uniqueIndex:compositeindex"`
	Hostname                string    `json:"hostname" gorm:"uniqueIndex:compositeindex"`
	Command                 string    `json:"command" gorm:"uniqueIndex:compositeindex"`
	CurrentWorkingDirectory string    `json:"current_working_directory" gorm:"uniqueIndex:compositeindex"`
	ExitCode                int       `json:"exit_code" gorm:"uniqueIndex:compositeindex"`
	StartTime               time.Time `json:"start_time" gorm:"uniqueIndex:compositeindex"`
	EndTime                 time.Time `json:"end_time" gorm:"uniqueIndex:compositeindex"`
}

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

// const (
// 	MESSAGE_TYPE_REQUEST_DUMP = iota
// )

// type AsyncMessage struct {
// 	MessageType int `json:"message_type"`
// }

const (
	CONFIG_PATH = ".hishtory.config"
	HISHTORY_PATH      = ".hishtory"
	DB_PATH            = ".hishtory.db"
	KDF_USER_ID        = "user_id"
	KDF_DEVICE_ID      = "device_id"
	KDF_ENCRYPTION_KEY = "encryption_key"
)

func Hmac(key, additionalData string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(additionalData))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

func UserId(key string) string {
	return Hmac(key, KDF_USER_ID)
}

func EncryptionKey(userSecret string) ([]byte, error) {
	encryptionKey, err := base64.URLEncoding.DecodeString(Hmac(userSecret, KDF_ENCRYPTION_KEY))
	if err != nil {
		return []byte{}, fmt.Errorf("Impossible state, decode(encode(hmac)) failed: %v", err)
	}
	return encryptionKey, nil
}

func makeAead(userSecret string) (cipher.AEAD, error) {
	key, err := EncryptionKey(userSecret)
	if err != nil {
		return nil, err
	}
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
		return []byte{}, []byte{}, fmt.Errorf("Failed to make AEAD: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return []byte{}, []byte{}, fmt.Errorf("Failed to read a nonce: %v", err)
	}
	ciphertext := aead.Seal(nil, nonce, data, additionalData)
	_, err = aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		panic(err)
	}
	return ciphertext, nonce, nil
}

func Decrypt(userSecret string, data, additionalData, nonce []byte) ([]byte, error) {
	aead, err := makeAead(userSecret)
	if err != nil {
		return []byte{}, fmt.Errorf("Failed to make AEAD: %v", err)
	}
	plaintext, err := aead.Open(nil, nonce, data, additionalData)
	if err != nil {
		return []byte{}, fmt.Errorf("Failed to decrypt: %v", err)
	}
	return plaintext, nil
}

func EncryptHistoryEntry(userSecret string, entry HistoryEntry) (EncHistoryEntry, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return EncHistoryEntry{}, err
	}
	ciphertext, nonce, err := Encrypt(userSecret, data, []byte(UserId(userSecret)))
	if err != nil {
		return EncHistoryEntry{}, err
	}
	return EncHistoryEntry{
		EncryptedData: ciphertext,
		Nonce:         nonce,
		UserId:        UserId(userSecret),
		Date:          time.Now(),
		EncryptedId:   uuid.Must(uuid.NewRandom()).String(),
		ReadCount:     0,
	}, nil
}

func DecryptHistoryEntry(userSecret string, entry EncHistoryEntry) (HistoryEntry, error) {
	if entry.UserId != UserId(userSecret) {
		return HistoryEntry{}, fmt.Errorf("Refusing to decrypt history entry with mismatching UserId")
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

func IsTestEnvironment() bool {
	return os.Getenv("HISHTORY_TEST") != ""
}

func OpenLocalSqliteDb() (*gorm.DB, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user's home directory: %v", err)
	}
	err = os.MkdirAll(path.Join(homedir, HISHTORY_PATH), 0744)
	if err != nil {
		return nil, fmt.Errorf("failed to create ~/.hishtory dir: %v", err)
	}
	db, err := gorm.Open(sqlite.Open(path.Join(homedir, HISHTORY_PATH, DB_PATH)), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %v", err)
	}
	tx, err := db.DB()
	if err != nil {
		return nil, err
	}
	err = tx.Ping()
	if err != nil {
		return nil, err
	}
	db.AutoMigrate(&HistoryEntry{})
	db.AutoMigrate(&EncHistoryEntry{})
	db.AutoMigrate(&Device{})
	return db, nil
}

func Search(db *gorm.DB, query string, limit int) ([]*HistoryEntry, error) {
	tokens, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %v", err)
	}
	tx := db.Where("true")
	for _, token := range tokens {
		if strings.Contains(token, ":") {
			splitToken := strings.SplitN(token, ":", 2)
			field := splitToken[0]
			val := splitToken[1]
			// tx = tx.Where()
			panic("TODO(ddworken): Use " + field + val)
		} else if strings.HasPrefix(token, "-") {
			panic("TODO(ddworken): Implement -foo as filtering out foo")
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
