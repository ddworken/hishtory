package shared

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	k1, err := EncryptionKey("key")
	Check(t, err)
	k2, err := EncryptionKey("key")
	Check(t, err)
	if string(k1) != string(k2) {
		t.Fatalf("Expected EncryptionKey to be deterministic!")
	}

	ciphertext, nonce, err := Encrypt("key", []byte("hello world!"), []byte("extra"))
	Check(t, err)
	plaintext, err := Decrypt("key", ciphertext, []byte("extra"), nonce)
	Check(t, err)
	if string(plaintext) != "hello world!" {
		t.Fatalf("Expected decrypt(encrypt(x)) to work, but it didn't!")
	}
}
