package shared

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Represents an encrypted history entry
type EncHistoryEntry struct {
	EncryptedData []byte `json:"enc_data"`
	Nonce         []byte `json:"nonce"`
	DeviceId      string `json:"device_id"`
	UserId        string `json:"user_id"`
	// Note that EncHistoryEntry.Date == HistoryEntry.EndTime
	Date        time.Time `json:"time"`
	EncryptedId string    `json:"id"`
	ReadCount   int       `json:"read_count"`
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

// Represents a request to get all history entries from a given device. Used as part of bootstrapping
// a new device.
type DumpRequest struct {
	UserId             string    `json:"user_id"`
	RequestingDeviceId string    `json:"requesting_device_id"`
	RequestTime        time.Time `json:"request_time"`
}

// Identifies where updates can be downloaded from
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

// Represents a request to delete history entries
type DeletionRequest struct {
	// The UserID that we're deleting entries for
	UserId string `json:"user_id"`
	// The DeviceID that is handling this deletion request. This struct is duplicated and put into the queue
	// for each of a user's devices.
	DestinationDeviceId string `json:"destination_device_id"`
	// When this deletion request was sent
	SendTime time.Time `json:"send_time"`
	// The history entries to delete
	Messages MessageIdentifiers `json:"messages"`
	// How many times this request has been processed
	ReadCount int `json:"read_count"`
}

// Identifies a list of history entries that should be deleted
type MessageIdentifiers struct {
	Ids []MessageIdentifier `json:"message_ids"`
}

// Identifies a single history entry based on the device that recorded the entry, and the end time. Note that
// this does not include the command itself since that would risk including the sensitive data that is meant
// to be deleted
type MessageIdentifier struct {
	// The device that the entry was recorded on (NOT the device where it is stored/requesting deletion)
	DeviceId string `json:"device_id"`
	// The timestamp when the command finished running. Serialized as "date" for legacy compatibility.
	EndTime time.Time `json:"date"`
	// The timestamp when the command started running.
	// Note this field was added as part of supporting pre-saving commands, so older clients do not set this field
	StartTime time.Time `json:"start_time"`
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

// Represents a piece of user feedback, submitted upon uninstall
type Feedback struct {
	UserId   string    `json:"user_id" gorm:"not null"`
	Date     time.Time `json:"date" gorm:"not null"`
	Feedback string    `json:"feedback"`
}

// Response from submitting new history entries. Contains deletion requests and dump requests to avoid
// extra round-trip requests to the hishtory backend.
type SubmitResponse struct {
	DumpRequests     []*DumpRequest     `json:"dump_requests"`
	DeletionRequests []*DeletionRequest `json:"deletion_requests"`
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
