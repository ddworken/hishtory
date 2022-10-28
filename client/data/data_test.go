package data

import (
	"testing"
	"time"
)

func TestEncryptDecrypt(t *testing.T) {
	k1 := EncryptionKey("key")
	k2 := EncryptionKey("key")
	if string(k1) != string(k2) {
		t.Fatalf("Expected EncryptionKey to be deterministic!")
	}

	ciphertext, nonce, err := Encrypt("key", []byte("hello world!"), []byte("extra"))
	checkError(t, err)
	plaintext, err := Decrypt("key", ciphertext, []byte("extra"), nonce)
	checkError(t, err)
	if string(plaintext) != "hello world!" {
		t.Fatalf("Expected decrypt(encrypt(x)) to work, but it didn't!")
	}
}

func TestParseTimeGenerously(t *testing.T) {
	ts, err := parseTimeGenerously("2006-01-02T15:04:00-08:00")
	checkError(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02 T15:04:00 -08:00")
	checkError(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_T15:04:00_-08:00")
	checkError(t, err)
	if ts.Unix() != 1136243040 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04:00")
	checkError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_T15:04:00")
	checkError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_15:04:00")
	checkError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02T15:04")
	checkError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02_15:04")
	checkError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 15 || ts.Minute() != 4 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
	ts, err = parseTimeGenerously("2006-01-02")
	checkError(t, err)
	if ts.Year() != 2006 || ts.Month() != time.January || ts.Day() != 2 || ts.Hour() != 0 || ts.Minute() != 0 || ts.Second() != 0 {
		t.Fatalf("parsed time incorrectly: %d", ts.Unix())
	}
}

func checkError(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}

func TestCustomColumnSerialization(t *testing.T) {
	cc1 := CustomColumn{
		Name: "name1",
		Val:  "val1",
	}
	cc2 := CustomColumn{
		Name: "name2",
		Val:  "val2",
	}
	var ccs CustomColumns = make(CustomColumns, 0)

	// Empty array
	v, err := ccs.Value()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	val := string(v.([]uint8))
	if val != "[]" {
		t.Fatalf("unexpected val for empty CustomColumns: %#v", val)
	}

	// Non-empty array
	ccs = append(ccs, cc1, cc2)
	v, err = ccs.Value()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	val = string(v.([]uint8))
	if val != "[{\"name\":\"name1\",\"value\":\"val1\"},{\"name\":\"name2\",\"value\":\"val2\"}]" {
		t.Fatalf("unexpected val for empty CustomColumns: %#v", val)
	}

}
