package data

import (
	"testing"

	"github.com/ddworken/hishtory/shared"
)

func TestEncryptDecrypt(t *testing.T) {
	k1 := EncryptionKey("key")
	k2 := EncryptionKey("key")
	if string(k1) != string(k2) {
		t.Fatalf("Expected EncryptionKey to be deterministic!")
	}

	ciphertext, nonce, err := Encrypt("key", []byte("hello world!"), []byte("extra"))
	shared.Check(t, err)
	plaintext, err := Decrypt("key", ciphertext, []byte("extra"), nonce)
	shared.Check(t, err)
	if string(plaintext) != "hello world!" {
		t.Fatalf("Expected decrypt(encrypt(x)) to work, but it didn't!")
	}
}

func TestParseTimeGenerously(t *testing.T) {
	ts, err := parseTimeGenerously("2006-01-02T15:04:00-08:00")
	shared.Check(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04:00")
	shared.Check(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04")
	shared.Check(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02")
	shared.Check(t, err)
	if ts.Unix() != 1136160000 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
}

// TODO: Maybe some dedicated unit tests for Search()
