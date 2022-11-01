package data

import (
	"testing"
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
