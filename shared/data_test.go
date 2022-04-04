package shared

import (
	"testing"
)

func TestPersist(t *testing.T) {
	defer BackupAndRestore(t)()
	Check(t, Setup([]string{}))
	db, err := OpenLocalSqliteDb()
	Check(t, err)

	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	Check(t, err)
	Check(t, Persist(db, *entry))
	var historyEntries []*HistoryEntry
	result := db.Find(&historyEntries)
	Check(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	if !EntryEquals(*entry, *dbEntry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v \ninput=%#v", *dbEntry, *entry)
	}
}

func TestSearch(t *testing.T) {
	defer BackupAndRestore(t)()
	Check(t, Setup([]string{}))
	db, err := OpenLocalSqliteDb()
	Check(t, err)

	// Insert data
	entry1, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /  ", "1641774958326745663"})
	Check(t, err)
	Check(t, Persist(db, *entry1))
	entry2, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  ls /foo  ", "1641774958326745663"})
	Check(t, err)
	Check(t, Persist(db, *entry2))
	entry3, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "120", " 123  echo hi  ", "1641774958326745663"})
	Check(t, err)
	Check(t, Persist(db, *entry3))

	// Search for data
	secret, err := GetUserSecret()
	Check(t, err)
	results, err := Search(db, secret, "ls", 5)
	Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2!", len(results))
	}
	if !EntryEquals(*results[0], *entry2) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[0], entry2)
	}
	if !EntryEquals(*results[1], *entry1) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[1], entry1)
	}
}

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
