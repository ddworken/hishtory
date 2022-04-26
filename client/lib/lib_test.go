package lib

import (
	"os"
	"os/user"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/shared"
)

func TestSetup(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer()()
	homedir, err := os.UserHomeDir()
	shared.Check(t, err)
	if _, err := os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	shared.Check(t, Setup([]string{}))
	if _, err := os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	shared.Check(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
}

func TestBuildHistoryEntry(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer()()
	shared.Check(t, Setup([]string{}))

	// Test building an actual entry for bash
	entry, err := BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "bash", "120", " 123  ls /foo  ", "1641774958"})
	shared.Check(t, err)
	if entry.ExitCode != 120 {
		t.Fatalf("history entry has unexpected exit code: %v", entry.ExitCode)
	}
	user, err := user.Current()
	if err != nil {
		t.Fatalf("failed to retrieve user: %v", err)
	}
	if entry.LocalUsername != user.Username {
		t.Fatalf("history entry has unexpected user name: %v", entry.LocalUsername)
	}
	if !strings.HasPrefix(entry.CurrentWorkingDirectory, "/") && !strings.HasPrefix(entry.CurrentWorkingDirectory, "~/") {
		t.Fatalf("history entry has unexpected cwd: %v", entry.CurrentWorkingDirectory)
	}
	if entry.Command != "ls /foo" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-09T") && !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-10T") {
		t.Fatalf("history entry has incorrect date in the start time: %v", entry.StartTime.Format(time.RFC3339))
	}
	if entry.StartTime.Unix() != 1641774958 {
		t.Fatalf("history entry has incorrect Unix time in the start time: %v", entry.StartTime.Unix())
	}

	// Test building an entry for zsh
	entry, err = BuildHistoryEntry([]string{"unused", "saveHistoryEntry", "zsh", "120", "ls /foo\n", "1641774958"})
	shared.Check(t, err)
	if entry.ExitCode != 120 {
		t.Fatalf("history entry has unexpected exit code: %v", entry.ExitCode)
	}
	if entry.LocalUsername != user.Username {
		t.Fatalf("history entry has unexpected user name: %v", entry.LocalUsername)
	}
	if !strings.HasPrefix(entry.CurrentWorkingDirectory, "/") && !strings.HasPrefix(entry.CurrentWorkingDirectory, "~/") {
		t.Fatalf("history entry has unexpected cwd: %v", entry.CurrentWorkingDirectory)
	}
	if entry.Command != "ls /foo" {
		t.Fatalf("history entry has unexpected command: %v", entry.Command)
	}
	if !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-09T") && !strings.HasPrefix(entry.StartTime.Format(time.RFC3339), "2022-01-10T") {
		t.Fatalf("history entry has incorrect date in the start time: %v", entry.StartTime.Format(time.RFC3339))
	}
	if entry.StartTime.Unix() != 1641774958 {
		t.Fatalf("history entry has incorrect Unix time in the start time: %v", entry.StartTime.Unix())
	}
}

func TestGetUserSecret(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	defer shared.RunTestServer()()
	shared.Check(t, Setup([]string{}))
	secret1, err := GetUserSecret()
	shared.Check(t, err)
	if len(secret1) < 10 || strings.Contains(secret1, " ") || strings.Contains(secret1, "\n") {
		t.Fatalf("unexpected secret: %v", secret1)
	}

	shared.Check(t, Setup([]string{}))
	secret2, err := GetUserSecret()
	shared.Check(t, err)

	if secret1 == secret2 {
		t.Fatalf("GetUserSecret() returned the same values for different setups! val=%v", secret1)
	}
}

func TestPersist(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	entry := data.MakeFakeHistoryEntry("ls ~/")
	db.Create(entry)
	var historyEntries []*data.HistoryEntry
	result := db.Find(&historyEntries)
	shared.Check(t, result.Error)
	if len(historyEntries) != 1 {
		t.Fatalf("DB has %d entries, expected 1!", len(historyEntries))
	}
	dbEntry := historyEntries[0]
	if !data.EntryEquals(entry, *dbEntry) {
		t.Fatalf("DB data is different than input! \ndb   =%#v \ninput=%#v", *dbEntry, entry)
	}
}

func TestSearch(t *testing.T) {
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	// Insert data
	entry1 := data.MakeFakeHistoryEntry("ls /foo")
	db.Create(entry1)
	entry2 := data.MakeFakeHistoryEntry("ls /bar")
	db.Create(entry2)

	// Search for data
	results, err := data.Search(db, "ls", 5)
	shared.Check(t, err)
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, expected 2!", len(results))
	}
	if !data.EntryEquals(*results[0], entry2) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[0], entry2)
	}
	if !data.EntryEquals(*results[1], entry1) {
		t.Fatalf("Search()[0]=%#v, expected: %#v", results[1], entry1)
	}
}

func TestAddToDbIfNew(t *testing.T) {
	// Set up
	defer shared.BackupAndRestore(t)()
	db, err := OpenLocalSqliteDb()
	shared.Check(t, err)

	// Add duplicate entries
	entry1 := data.MakeFakeHistoryEntry("ls /foo")
	AddToDbIfNew(db, entry1)
	AddToDbIfNew(db, entry1)
	entry2 := data.MakeFakeHistoryEntry("ls /foo")
	AddToDbIfNew(db, entry2)
	AddToDbIfNew(db, entry2)
	AddToDbIfNew(db, entry1)

	// Check there should only be two entries
	var entries []data.HistoryEntry
	result := db.Find(&entries)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if len(entries) != 2 {
		t.Fatalf("entries has an incorrect length: %d", len(entries))
	}
}

func TestParseCrossPlatformInt(t *testing.T) {
	res, err := parseCrossPlatformInt("123")
	if err != nil {
		t.Fatalf("failed to parse int: %v", err)
	}
	if res != 123 {
		t.Fatalf("failed to parse cross platform int %d", res)
	}
	res, err = parseCrossPlatformInt("123N")
	if err != nil {
		t.Fatalf("failed to parse int: %v", err)
	}
	if res != 123 {
		t.Fatalf("failed to parse cross platform int %d", res)
	}
}

func TestParseXattr(t *testing.T) {
	dump := `com.apple.macl:
00000000  04 00 34 5A 0D 8F 9B 10 48 FB 9D 12 E2 11 C7 21  |................|
00000010  D3 17 04 00 7D 17 C7 D7 51 B6 4B C4 B0 E5 1A 58  |................|
00000020  21 53 DD 4C 00 00 00 00 00 00 00 00 00 00 00 00  |!S.L............|
00000030  00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00  |................|
00000040  00 00 00 00 00 00 00 00                          |........�......|
00000048
com.apple.metadata:kMDItemDownloadedDate:
00000000  62 70 6C 69 73 74 30 30 A1 01 33 41 C4 07 D4 F5  |bplist00..3A....|
00000010  D0 E7 A3 08 0A 00 00 00 00 00 00 01 01 00 00 00  |................|
00000020  00 00 00 00 02 00 00 00 00 00 00 00 00 00 00 00  |................|
00000030  00 00 00 00 13                                   |.....|
00000035
com.apple.metadata:kMDItemWhereFroms:
00000000  62 70 6C 69 73 74 30 30 A2 01 02 5F 10 47 68 74  |bplist00..._.Ght|
00000010  74 70 73 3A 2F 2F 64 6C 2E 67 6F 6F 67 6C 65 2E  |tps://dl.google.|
00000020  63 6F 6D 2F 63 68 72 6F 6D 65 2F 6D 61 63 2F 75  |com/chrome/mac/u|
00000030  6E 69 76 65 72 73 61 6C 2F 73 74 61 62 6C 65 2F  |niversal/stable/|
00000040  47 47 52 4F 2F 67 6F 6F 67 6C 65 63 68 72 6F 6D  |GGRO/googlechrom|
00000050  65 2E 64 6D 67 5F 10 17 68 74 74 70 73 3A 2F 2F  |e.dmg_..https://|
00000060  77 77 77 2E 67 6F 6F 67 6C 65 2E 63 6F 6D 2F 08  |www.google.com/.|
00000092
com.apple.quarantine:
00000000  30 31 38 33 3B 36 32 35 66 37 32 36 62 3B 53 61  |0183;625f726b;Sa|
00000010  66 61 72 69 3B 46 37 33 37 42 42 43 33 2D 30 41  |fari;F737BBC3-0A|
00000020  35 38 2D 34 31 44 34 2D 38 46 33 36 2D 30 33 42  |58-41D4-8F36-03B|
00000030  42 33 31 36 36 39 35 39 39                       |B31669599|
00000039`
	xattr, err := parseXattr(dump)
	if err != nil {
		t.Fatal(err)
	}
	if len(xattr) != 4 {
		t.Fatalf("xattr has an incorrect length: %d", len(xattr))
	}
	val := xattr["com.apple.quarantine"]
	if string(val) != "0183;625f726b;Safari;F737BBC3-0A58-41D4-8F36-03BB31669599" {
		t.Fatalf("unexpected xattr value=%#v", string(val))
	}
	val = xattr["com.apple.metadata:kMDItemWhereFroms"]
	if string(val) != "bplist00\xa2\x01\x02_\x10Ghttps://dl.google.com/chrome/mac/universal/stable/GGRO/googlechrome.dmg_\x10\x17https://www.google.com/\b" {
		t.Fatalf("unexpected xattr value=%#v", string(val))
	}
	val = xattr["com.apple.metadata:kMDItemDownloadedDate"]
	if string(val) != "bplist00\xa1\x013A\xc4\a\xd4\xf5\xd0\xe7\xa3\b\n\x00\x00\x00\x00\x00\x00\x01\x01\x00\x00\x00\x00\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x13" {
		t.Fatalf("unexpected xattr value=%#v", string(val))
	}
	val = xattr["com.apple.macl"]
	if string(val) != "\x04\x004Z\r\x8f\x9b\x10H\xfb\x9d\x12\xe2\x11\xc7!\xd3\x17\x04\x00}\x17\xc7\xd7Q\xb6Kİ\xe5\x1aX!S\xddL\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" {
		t.Fatalf("unexpected xattr value=%#v", string(val))
	}
}
