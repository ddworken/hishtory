package shared

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
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

/*
Manually created the indices:
CREATE INDEX CONCURRENTLY device_id_idx ON enc_history_entries USING btree(device_id);
CREATE INDEX CONCURRENTLY read_count_idx ON enc_history_entries USING btree(read_count);
CREATE INDEX CONCURRENTLY redact_idx ON enc_history_entries USING btree(user_id, device_id, date);
*/

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

type DumpRequest struct {
	UserId             string    `json:"user_id"`
	RequestingDeviceId string    `json:"requesting_device_id"`
	RequestTime        time.Time `json:"request_time"`
}

type UpdateInfo struct {
	LinuxAmd64Url             string `json:"linux_amd_64_url"`
	LinuxAmd64AttestationUrl  string `json:"linux_amd_64_attestation_url"`
	LinuxArm64Url             string `json:"linux_arm_64_url"`
	LinuxArm64AttestationUrl  string `json:"linux_arm_64_attestation_url"`
	LinuxArm7Url              string `json:"linux_arm_7_url"`
	LinuxArm7AttestationUrl   string `json:"linux_arm_7_attestation_url"`
	DarwinAmd64Url            string `json:"darwin_amd_64_url"`
	DarwinAmd64UnsignedUrl    string `json:"darwin_amd_64_unsigned_url"`
	DarwinAmd64AttestationUrl string `json:"darwin_amd_64_attestation_url"`
	DarwinArm64Url            string `json:"darwin_arm_64_url"`
	DarwinArm64UnsignedUrl    string `json:"darwin_arm_64_unsigned_url"`
	DarwinArm64AttestationUrl string `json:"darwin_arm_64_attestation_url"`
	Version                   string `json:"version"`
}

type DeletionRequest struct {
	UserId              string             `json:"user_id"`
	DestinationDeviceId string             `json:"destination_device_id"`
	SendTime            time.Time          `json:"send_time"`
	Messages            MessageIdentifiers `json:"messages"`
	ReadCount           int                `json:"read_count"`
}

type MessageIdentifiers struct {
	Ids []MessageIdentifier `json:"message_ids"`
}

type MessageIdentifier struct {
	DeviceId string    `json:"device_id"`
	Date     time.Time `json:"date"`
}

func (m *MessageIdentifiers) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal JSONB value: %v", value)
	}

	result := MessageIdentifiers{}
	err := json.Unmarshal(bytes, &result)
	*m = result
	return err
}

func (m MessageIdentifiers) Value() (driver.Value, error) {
	return json.Marshal(m)
}

type Feedback struct {
	UserId   string    `json:"user_id" gorm:"not null"`
	Date     time.Time `json:"date" gorm:"not null"`
	Feedback string    `json:"feedback"`
}

func Chunks[k any](slice []k, chunkSize int) [][]k {
	var chunks [][]k
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}
