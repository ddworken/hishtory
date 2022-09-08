package data

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/ddworken/hishtory/shared"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	KdfUserID        = "user_id"
	KdfDeviceID      = "device_id"
	KdfEncryptionKey = "encryption_key"
)

type HistoryEntry struct {
	LocalUsername           string    `json:"local_username" gorm:"uniqueIndex:compositeindex"`
	Hostname                string    `json:"hostname" gorm:"uniqueIndex:compositeindex"`
	Command                 string    `json:"command" gorm:"uniqueIndex:compositeindex"`
	CurrentWorkingDirectory string    `json:"current_working_directory" gorm:"uniqueIndex:compositeindex"`
	ExitCode                int       `json:"exit_code" gorm:"uniqueIndex:compositeindex"`
	StartTime               time.Time `json:"start_time" gorm:"uniqueIndex:compositeindex"`
	EndTime                 time.Time `json:"end_time" gorm:"uniqueIndex:compositeindex"`
	DeviceId                string    `json:"device_id" gorm:"uniqueIndex:compositeindex"`
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
		return []byte{}, []byte{}, fmt.Errorf("failed to make AEAD: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return []byte{}, []byte{}, fmt.Errorf("failed to read a nonce: %v", err)
	}
	ciphertext := aead.Seal(nil, nonce, data, additionalData)
	_, err = aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return []byte{}, []byte{}, fmt.Errorf("failed to open AEAD: %v", err)
	}
	return ciphertext, nonce, nil
}

func Decrypt(userSecret string, data, additionalData, nonce []byte) ([]byte, error) {
	aead, err := makeAead(userSecret)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to make AEAD: %v", err)
	}
	plaintext, err := aead.Open(nil, nonce, data, additionalData)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to decrypt: %v", err)
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
		Date:          time.Now(),
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

func parseTimeGenerously(input string) (time.Time, error) {
	input = strings.ReplaceAll(input, "_", " ")
	return dateparse.ParseLocal(input)
}

func Search(db *gorm.DB, query string, limit int) ([]*HistoryEntry, error) {
	tokens, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize query: %v", err)
	}
	tx := db.Where("true")
	for _, token := range tokens {
		if strings.HasPrefix(token, "-") {
			if strings.Contains(token, ":") {
				query, val, err := parseAtomizedToken(token[1:])
				if err != nil {
					return nil, err
				}
				tx = tx.Where("NOT "+query, val)
			} else {
				query, v1, v2, v3, err := parseNonAtomizedToken(token[1:])
				if err != nil {
					return nil, err
				}
				tx = tx.Where("NOT "+query, v1, v2, v3)
			}
		} else if strings.Contains(token, ":") {
			query, val, err := parseAtomizedToken(token)
			if err != nil {
				return nil, err
			}
			tx = tx.Where(query, val)
		} else {
			query, v1, v2, v3, err := parseNonAtomizedToken(token)
			if err != nil {
				return nil, err
			}
			tx = tx.Where(query, v1, v2, v3)
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

func parseNonAtomizedToken(token string) (string, interface{}, interface{}, interface{}, error) {
	wildcardedToken := "%" + token + "%"
	return "(command LIKE ? OR hostname LIKE ? OR current_working_directory LIKE ?)", wildcardedToken, wildcardedToken, wildcardedToken, nil
}

func parseAtomizedToken(token string) (string, interface{}, error) {
	splitToken := strings.SplitN(token, ":", 2)
	field := splitToken[0]
	val := splitToken[1]
	switch field {
	case "user":
		return "(local_username = ?)", val, nil
	case "host":
		fallthrough
	case "hostname":
		return "(instr(hostname, ?) > 0)", val, nil
	case "cwd":
		// TODO: Can I make this support querying via ~/ too?
		return "(instr(current_working_directory, ?) > 0)", strings.TrimSuffix(val, "/"), nil
	case "exit_code":
		return "(exit_code = ?)", val, nil
	case "before":
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, fmt.Errorf("failed to parse before:%s as a timestamp: %v", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) < ?)", t.Unix(), nil
	case "after":
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, fmt.Errorf("failed to parse after:%s as a timestamp: %v", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) > ?)", t.Unix(), nil
	default:
		return "", nil, fmt.Errorf("search query contains unknown search atom %s", field)
	}
}

func tokenize(query string) ([]string, error) {
	if query == "" {
		return []string{}, nil
	}
	return strings.Split(query, " "), nil
}

func EntryEquals(entry1, entry2 HistoryEntry) bool {
	return entry1.LocalUsername == entry2.LocalUsername &&
		entry1.Hostname == entry2.Hostname &&
		entry1.Command == entry2.Command &&
		entry1.CurrentWorkingDirectory == entry2.CurrentWorkingDirectory &&
		entry1.ExitCode == entry2.ExitCode &&
		entry1.StartTime.Format(time.RFC3339) == entry2.StartTime.Format(time.RFC3339) &&
		entry1.EndTime.Format(time.RFC3339) == entry2.EndTime.Format(time.RFC3339)
}

func MakeFakeHistoryEntry(command string) HistoryEntry {
	return HistoryEntry{
		LocalUsername:           "david",
		Hostname:                "localhost",
		Command:                 command,
		CurrentWorkingDirectory: "/tmp/",
		ExitCode:                2,
		StartTime:               time.Now(),
		EndTime:                 time.Now(),
	}
}
