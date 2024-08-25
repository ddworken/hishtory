package lib

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	_ "embed" // for embedding config.sh
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"

	"github.com/araddon/dateparse"
	"github.com/google/uuid"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"
	"iter"
)

//go:embed config.sh
var ConfigShContents string

//go:embed config.zsh
var ConfigZshContents string

//go:embed config.fish
var ConfigFishContents string

var (
	Version   string = "Unknown"
	GitCommit string = "Unknown"
)

// 512KB ought to be enough for any reasonable cmd
// Funnily enough, 256KB actually wasn't enough. See https://github.com/ddworken/hishtory/issues/93
var maxSupportedLineLengthForImport = 512_000

func AddToDbIfNew(db *gorm.DB, entry data.HistoryEntry) {
	tx := db.Where("local_username = ?", entry.LocalUsername)
	tx = tx.Where("hostname = ?", entry.Hostname)
	tx = tx.Where("command = ?", entry.Command)
	tx = tx.Where("current_working_directory = ?", entry.CurrentWorkingDirectory)
	tx = tx.Where("home_directory = ?", entry.HomeDirectory)
	tx = tx.Where("exit_code = ?", entry.ExitCode)
	tx = tx.Where("start_time = ?", entry.StartTime)
	tx = tx.Where("end_time = ?", entry.EndTime)
	var results []data.HistoryEntry
	tx.Limit(1).Find(&results)
	if len(results) == 0 {
		db.Create(normalizeEntryTimezone(entry))
		// TODO: check the error here and bubble it up
	}
}

func getCustomColumnValue(ctx context.Context, header string, entry data.HistoryEntry) (string, error) {
	for _, c := range entry.CustomColumns {
		if strings.EqualFold(c.Name, header) {
			return c.Val, nil
		}
	}
	config := hctx.GetConf(ctx)
	for _, c := range config.CustomColumns {
		if strings.EqualFold(c.ColumnName, header) {
			return "", nil
		}
	}
	return "", fmt.Errorf("failed to find a column matching the column name %#v (is there a typo?)", header)
}

func BuildTableRow(ctx context.Context, columnNames []string, entry data.HistoryEntry, commandRenderer func(string) string) ([]string, error) {
	row := make([]string, 0)
	for _, header := range columnNames {
		switch header {
		case "Hostname", "hostname", "hn":
			row = append(row, entry.Hostname)
		case "CWD", "cwd":
			row = append(row, entry.CurrentWorkingDirectory)
		case "Timestamp", "timestamp", "ts":
			if entry.StartTime.UnixMilli() == 0 {
				row = append(row, "N/A")
			} else {
				row = append(row, entry.StartTime.Local().Format(hctx.GetConf(ctx).TimestampFormat))
			}
		case "Runtime", "runtime", "rt":
			if entry.EndTime.UnixMilli() == 0 {
				// An EndTime of zero means this is a pre-saved entry that never finished
				row = append(row, "N/A")
			} else {
				row = append(row, entry.EndTime.Local().Sub(entry.StartTime.Local()).Round(time.Millisecond).String())
			}
		case "Exit Code", "Exit_Code", "ExitCode", "exitcode", "$?", "EC":
			row = append(row, fmt.Sprintf("%d", entry.ExitCode))
		case "Command", "command", "cmd":
			row = append(row, commandRenderer(entry.Command))
		case "User", "user":
			row = append(row, entry.LocalUsername)
		default:
			customColumnValue, err := getCustomColumnValue(ctx, header, entry)
			if err != nil {
				return nil, err
			}
			row = append(row, customColumnValue)
		}
	}
	return row, nil
}

// Make a regex that matches the non-tokenized bits of the given query
func MakeRegexFromQuery(query string) string {
	tokens := tokenize(strings.TrimSpace(query))
	r := ""
	for _, token := range tokens {
		if !strings.HasPrefix(token, "-") && !containsUnescaped(token, ":") {
			if r != "" {
				r += "|"
			}
			r += fmt.Sprintf("(%s)", regexp.QuoteMeta(token))
		}
	}
	return r
}

func CheckFatalError(err error) {
	if err != nil {
		_, filename, line, _ := runtime.Caller(1)
		log.Fatalf("hishtory v0.%s fatal error at %s:%d: %v", Version, filename, line, err)
	}
}

var ZSH_FIRST_COMMAND_BUG_REGEX = regexp.MustCompile(`: \d+:\d;(.*)`)

func stripZshWeirdness(cmd string) string {
	// Zsh has this weird behavior where sometimes commands are saved in the hishtory file
	// with a weird prefix. I've never been able to figure out why this happens, but we
	// can at least strip it.
	matches := ZSH_FIRST_COMMAND_BUG_REGEX.FindStringSubmatch(cmd)
	if len(matches) == 2 {
		return matches[1]
	}
	return cmd
}

var BASH_FIRST_COMMAND_BUG_REGEX = regexp.MustCompile(`^#\d+\s+$`)

func isBashWeirdness(cmd string) bool {
	// Bash has this weird behavior where the it has entries like `#1664342754` in the
	// history file. We want to skip these.
	return BASH_FIRST_COMMAND_BUG_REGEX.MatchString(cmd)
}

func countLinesInFile(filename string) (int, error) {
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := file.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

func countLinesInFiles(filenames ...string) (int, error) {
	total := 0
	for _, f := range filenames {
		l, err := countLinesInFile(f)
		if err != nil {
			return 0, err
		}
		total += l
	}
	return total, nil
}

// The number of entries where if we're importing more than this many entries, the import is likely to be
// slow, and it is then worth displaying a progress bar.
const NUM_IMPORTED_ENTRIES_SLOW int = 20_000

func ImportHistory(ctx context.Context, shouldReadStdin, force bool) (int, error) {
	config := hctx.GetConf(ctx)
	if config.HaveCompletedInitialImport && !force {
		// Don't run an import if we already have run one. This avoids importing the same entry multiple times.
		return 0, nil
	}
	homedir := hctx.GetHome(ctx)
	inputFiles := []string{
		filepath.Join(homedir, ".bash_history"),
		filepath.Join(homedir, ".zsh_history"),
	}
	if histfile := os.Getenv("HISTFILE"); histfile != "" && !slices.Contains(inputFiles, histfile) {
		inputFiles = append(inputFiles, histfile)
	}
	zHistPath := filepath.Join(homedir, ".zhistory")
	if !slices.Contains(inputFiles, zHistPath) {
		inputFiles = append(inputFiles, zHistPath)
	}
	entriesIter := parseFishHistory(homedir)
	for _, file := range inputFiles {
		entriesIter = concatIterators(entriesIter, readFileToIterator(file))
	}
	totalNumEntries, err := countLinesInFiles(inputFiles...)
	if err != nil {
		return 0, fmt.Errorf("failed to count input lines during hishtory import: %w", err)
	}
	if shouldReadStdin {
		extraEntries, err := readStdin()
		if err != nil {
			return 0, fmt.Errorf("failed to read stdin: %w", err)
		}
		entriesIter = concatIterators(entriesIter, Values(extraEntries))
		totalNumEntries += len(extraEntries)
	}
	fishLines, err := countLinesInFile(getFishHistoryPath(homedir))
	if err != nil {
		return 0, fmt.Errorf("failed to count fish history lines during hishtory import: %w", err)
	}
	totalNumEntries += fishLines
	db := hctx.GetDb(ctx)
	currentUser, err := user.Current()
	if err != nil {
		return 0, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return 0, err
	}
	numEntriesImported := 0
	var iteratorError error = nil
	var batch []data.HistoryEntry
	importTimestamp := time.Now().UTC()
	batchSize := 100
	importEntryId := uuid.Must(uuid.NewRandom()).String()
	var bar *progressbar.ProgressBar
	if totalNumEntries > NUM_IMPORTED_ENTRIES_SLOW {
		fmt.Println("Importing existing history entries")
		bar = progressbar.Default(int64(totalNumEntries))
		defer bar.Finish()
	}
	entriesIter(func(cmd string, err error) bool {
		if err != nil {
			iteratorError = err
			return false
		}
		cmd = stripZshWeirdness(cmd)
		if isBashWeirdness(cmd) || strings.HasPrefix(cmd, " ") {
			return true
		}
		// Set the timestamps so that they are monotonically increasing
		startTime := importTimestamp.Add(time.Millisecond * time.Duration(numEntriesImported*2))
		endTime := startTime.Add(time.Millisecond)
		// And set the entryId in a similar way. This isn't critical from a correctness POV, but uuid.NewRandom() is
		// quite slow, so this makes imports considerably faster
		entryId := importEntryId + fmt.Sprintf("%d", numEntriesImported)
		entry := normalizeEntryTimezone(data.HistoryEntry{
			LocalUsername:           currentUser.Name,
			Hostname:                hostname,
			Command:                 cmd,
			CurrentWorkingDirectory: "Unknown",
			HomeDirectory:           homedir,
			ExitCode:                0,
			StartTime:               startTime,
			EndTime:                 endTime,
			DeviceId:                config.DeviceId,
			EntryId:                 entryId,
		})
		batch = append(batch, entry)
		if len(batch) > batchSize {
			err = RetryingDbFunction(func() error {
				if err := db.Create(batch).Error; err != nil {
					return fmt.Errorf("failed to import batch of history entries: %w", err)
				}
				return nil
			})
			if err != nil {
				iteratorError = fmt.Errorf("failed to insert imported history entry: %w", err)
				return false
			}
			batch = make([]data.HistoryEntry, 0)
		}
		numEntriesImported += 1
		if bar != nil {
			_ = bar.Add(1)
			if numEntriesImported > totalNumEntries {
				bar.ChangeMax(-1)
			}
		}
		return true
	})
	if iteratorError != nil {
		return 0, iteratorError
	}
	// Also create any entries remaining in an unfinished batch
	if len(batch) > 0 {
		err = RetryingDbFunction(func() error {
			if err := db.Create(batch).Error; err != nil {
				return fmt.Errorf("failed to import final batch of history entries: %w", err)
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	err = Reupload(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to upload hishtory import: %w", err)
	}
	config.HaveCompletedInitialImport = true
	err = hctx.SetConfig(config)
	if err != nil {
		return 0, fmt.Errorf("failed to mark initial import as completed, this may lead to duplicate history entries: %w", err)
	}
	// Trigger a checkpoint so that these bulk entries are added from the WAL to the main DB
	db.Exec("PRAGMA wal_checkpoint")
	return numEntriesImported, nil
}

func readStdin() ([]string, error) {
	ret := make([]string, 0)
	in := bufio.NewReader(os.Stdin)
	for {
		s, err := in.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break
		}
		s = strings.TrimSpace(s)
		if s != "" {
			ret = append(ret, s)
		}
	}
	return ret, nil
}

func getFishHistoryPath(homedir string) string {
	return filepath.Join(homedir, ".local/share/fish/fish_history")
}

func parseFishHistory(homedir string) iter.Seq2[string, error] {
	lines := readFileToIterator(getFishHistoryPath(homedir))
	return func(yield func(string, error) bool) {
		lines(func(line string, err error) bool {
			if err != nil {
				return yield(line, err)
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- cmd: ") {
				yield(strings.SplitN(line, ": ", 2)[1], nil)
			}
			return true
		})
	}
}

// Concatenate two iterators.
// TODO: Equivalent of the future Go stdlib function iter.Concat2.
func concatIterators(iters ...iter.Seq2[string, error]) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for _, seq := range iters {
			seq(yield)
		}
		return
	}
}

// Convert a slice into an iterator.
// TODO: Equivalent of the future Go stdlib function iter.Values
func Values[Slice ~[]Elem, Elem any](s Slice) iter.Seq2[Elem, error] {
	return func(yield func(Elem, error) bool) {
		for _, v := range s {
			if !yield(v, nil) {
				return
			}
		}
		return
	}
}

func readFileToIterator(path string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		file, err := os.Open(path)
		if err != nil {
			yield("", fmt.Errorf("failed to open file: %w", err))
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, maxSupportedLineLengthForImport)
		scanner.Buffer(buf, maxSupportedLineLengthForImport)
		for scanner.Scan() {
			line := scanner.Text()
			if !yield(line, nil) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield("", fmt.Errorf("scanner.Err()=%w", err))
			return
		}

		return
	}
}

const DefaultServerHostname = "https://api.hishtory.dev"

func GetServerHostname() string {
	if server := os.Getenv("HISHTORY_SERVER"); server != "" {
		return server
	}
	return DefaultServerHostname
}

func ApiGet(ctx context.Context, path string) ([]byte, error) {
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return nil, fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}
	start := time.Now()
	req, err := http.NewRequest("GET", GetServerHostname()+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET: %w", err)
	}
	req.Header.Set("X-Hishtory-Version", "v0."+Version)
	req.Header.Set("X-Hishtory-Device-Id", hctx.GetConf(ctx).DeviceId)
	req.Header.Set("X-Hishtory-User-Id", data.UserId(hctx.GetConf(ctx).UserSecret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to GET %s%s: %w", GetServerHostname(), path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to GET %s%s: status_code=%d", GetServerHostname(), path, resp.StatusCode)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from GET %s%s: %w", GetServerHostname(), path, err)
	}
	duration := time.Since(start)
	hctx.GetLogger().Infof("ApiGet(%#v): %d bytes - %s\n", GetServerHostname()+path, len(respBody), duration.String())
	return respBody, nil
}

func ApiPost(ctx context.Context, path, contentType string, reqBody []byte) ([]byte, error) {
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return nil, fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}
	start := time.Now()
	req, err := http.NewRequest("POST", GetServerHostname()+path, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create POST: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Hishtory-Version", "v0."+Version)
	req.Header.Set("X-Hishtory-Device-Id", hctx.GetConf(ctx).DeviceId)
	req.Header.Set("X-Hishtory-User-Id", data.UserId(hctx.GetConf(ctx).UserSecret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to POST %s: %w", GetServerHostname()+path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to POST %s: status_code=%d", GetServerHostname()+path, resp.StatusCode)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from POST %s: %w", GetServerHostname()+path, err)
	}
	duration := time.Since(start)
	hctx.GetLogger().Infof("ApiPost(%#v): %d bytes - %s\n", GetServerHostname()+path, len(respBody), duration.String())
	return respBody, nil
}

func IsOfflineError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "dial tcp: lookup api.hishtory.dev") ||
		strings.Contains(err.Error(), ": no such host") ||
		strings.Contains(err.Error(), "connect: network is unreachable") ||
		strings.Contains(err.Error(), "read: connection reset by peer") ||
		strings.Contains(err.Error(), ": EOF") ||
		strings.Contains(err.Error(), ": status_code=502") ||
		strings.Contains(err.Error(), ": status_code=503") ||
		strings.Contains(err.Error(), ": i/o timeout") ||
		strings.Contains(err.Error(), "connect: operation timed out") ||
		strings.Contains(err.Error(), "net/http: TLS handshake timeout") ||
		strings.Contains(err.Error(), "connect: connection refused") {
		return true
	}
	if !CanReachHishtoryServer(ctx) {
		// If the backend server is down, then treat all errors as offline errors
		return true
	}
	// A truly unexpected error, bubble this up
	return false
}

func CanReachHishtoryServer(ctx context.Context) bool {
	_, err := ApiGet(ctx, "/api/v1/ping")
	return err == nil
}

func normalizeEntryTimezone(entry data.HistoryEntry) data.HistoryEntry {
	entry.StartTime = entry.StartTime.UTC()
	entry.EndTime = entry.EndTime.UTC()
	return entry
}

const SQLITE_LOCKED_ERR_MSG = "database is locked ("

func RetryingDbFunction(dbFunc func() error) error {
	var err error = nil
	i := 0
	for i = 0; i < 10; i++ {
		err = dbFunc()
		if err == nil {
			return nil
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, SQLITE_LOCKED_ERR_MSG) {
			time.Sleep(time.Duration(i*rand.Intn(100)) * time.Millisecond)
			continue
		}
		if strings.Contains(errMsg, "UNIQUE constraint failed: history_entries.") {
			return nil
		}
		return fmt.Errorf("unrecoverable sqlite error: %w", err)
	}
	return fmt.Errorf("failed to execute DB transaction even with %d retries: %w", i, err)
}

func RetryingDbFunctionWithResult[T any](dbFunc func() (T, error)) (T, error) {
	var t T
	var err error = nil
	i := 0
	for i = 0; i < 10; i++ {
		t, err = dbFunc()
		if err == nil {
			return t, nil
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, SQLITE_LOCKED_ERR_MSG) {
			time.Sleep(time.Duration(i*rand.Intn(100)) * time.Millisecond)
			continue
		}
		return t, fmt.Errorf("unrecoverable sqlite error: %w", err)
	}
	return t, fmt.Errorf("failed to execute DB transaction even with %d retries: %w", i, err)
}

func ReliableDbCreate(db *gorm.DB, entry data.HistoryEntry) error {
	entry = normalizeEntryTimezone(entry)
	return RetryingDbFunction(func() error {
		return db.Create(entry).Error
	})
}

func EncryptAndMarshal(config *hctx.ClientConfig, entries []*data.HistoryEntry) ([]byte, error) {
	var encEntries []shared.EncHistoryEntry
	for _, entry := range entries {
		encEntry, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt history entry: %w", err)
		}
		encEntry.DeviceId = config.DeviceId
		encEntries = append(encEntries, encEntry)
	}
	jsonValue, err := json.Marshal(encEntries)
	if err != nil {
		return jsonValue, fmt.Errorf("failed to marshal encrypted history entry: %w", err)
	}
	return jsonValue, nil
}

func Reupload(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	entries, err := Search(ctx, hctx.GetDb(ctx), "", 0)
	if err != nil {
		return fmt.Errorf("failed to reupload due to failed search: %w", err)
	}
	var bar *progressbar.ProgressBar
	if len(entries) > NUM_IMPORTED_ENTRIES_SLOW {
		fmt.Println("Persisting history entries")
		bar = progressbar.Default(int64(len(entries)))
		defer bar.Finish()
	}
	chunkSize := 500
	chunks := shared.Chunks(entries, chunkSize)
	return shared.ForEach(chunks, 10, func(chunk []*data.HistoryEntry) error {
		jsonValue, err := EncryptAndMarshal(config, chunk)
		if err != nil {
			return fmt.Errorf("failed to reupload due to failed encryption: %w", err)
		}
		_, err = ApiPost(ctx, "/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
		if err != nil {
			return fmt.Errorf("failed to reupload due to failed POST: %w", err)
		}
		if bar != nil {
			_ = bar.Add(chunkSize)
		}
		return nil
	})
}

func RetrieveAdditionalEntriesFromRemote(ctx context.Context, queryReason string) error {
	db := hctx.GetDb(ctx)
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	respBody, err := ApiGet(ctx, "/api/v1/query?device_id="+config.DeviceId+"&user_id="+data.UserId(config.UserSecret)+"&queryReason="+queryReason)
	if IsOfflineError(ctx, err) {
		return nil
	}
	if err != nil {
		return err
	}
	var retrievedEntries []*shared.EncHistoryEntry
	err = json.Unmarshal(respBody, &retrievedEntries)
	if err != nil {
		return fmt.Errorf("failed to load JSON response: %w", err)
	}
	for _, entry := range retrievedEntries {
		decEntry, err := data.DecryptHistoryEntry(config.UserSecret, *entry)
		if err != nil {
			return fmt.Errorf("failed to decrypt history entry from server: %w", err)
		}
		AddToDbIfNew(db, decEntry)
	}
	return ProcessDeletionRequests(ctx)
}

func ProcessDeletionRequests(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	resp, err := ApiGet(ctx, "/api/v1/get-deletion-requests?user_id="+data.UserId(config.UserSecret)+"&device_id="+config.DeviceId)
	if IsOfflineError(ctx, err) {
		return nil
	}
	if err != nil {
		return err
	}
	var deletionRequests []*shared.DeletionRequest
	err = json.Unmarshal(resp, &deletionRequests)
	if err != nil {
		return err
	}
	return HandleDeletionRequests(ctx, deletionRequests)
}

func HandleDeletionRequests(ctx context.Context, deletionRequests []*shared.DeletionRequest) error {
	db := hctx.GetDb(ctx)
	for _, request := range deletionRequests {
		for _, entry := range request.Messages.Ids {
			err := RetryingDbFunction(func() error {
				// Note that entry.EndTime is not always present (for pre-saved entries). And likewise,
				// entry.EntryId is not always present for older entries. So we just check that one of them matches.
				tx := db.Where("device_id = ? AND (end_time = ? OR entry_id = ?)", entry.DeviceId, entry.EndTime, entry.EntryId)
				return tx.Delete(&data.HistoryEntry{}).Error
			})
			if err != nil {
				return fmt.Errorf("DB error when deleting entries: %w", err)
			}
		}
	}
	return nil
}

func GetBanner(ctx context.Context) ([]byte, error) {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return []byte{}, nil
	}
	url := "/api/v1/banner?commit_hash=" + GitCommit + "&user_id=" + data.UserId(config.UserSecret) + "&device_id=" + config.DeviceId + "&version=" + Version + "&forced_banner=" + os.Getenv("FORCED_BANNER")
	return ApiGet(ctx, url)
}

func parseTimeGenerously(input string) (time.Time, error) {
	input = strings.ReplaceAll(input, "_", " ")
	return dateparse.ParseLocal(input)
}

// A wrapper around tx.Where(...) that filters out nil-values
func where(tx *gorm.DB, s string, v1, v2 any) *gorm.DB {
	if v1 == nil && v2 == nil {
		return tx.Where(s)
	}
	if v1 != nil && v2 == nil {
		return tx.Where(s, v1)
	}
	if v1 != nil && v2 != nil {
		return tx.Where(s, v1, v2)
	}
	panic(fmt.Sprintf("Impossible state: v1=%#v, v2=%#v", v1, v2))
}

func MakeWhereQueryFromSearch(ctx context.Context, db *gorm.DB, query string) (*gorm.DB, error) {
	tokens := tokenize(query)
	tx := db.Model(&data.HistoryEntry{}).Where("true")
	for _, token := range tokens {
		if strings.HasPrefix(token, "-") {
			if token == "-" {
				// The entire token is a -, just ignore this token. Otherwise we end up
				// interpreting "-" as exluding literally all results which is pretty useless.
				continue
			}
			if containsUnescaped(token, ":") {
				query, v1, v2, err := parseAtomizedToken(ctx, token[1:])
				if err != nil {
					return nil, err
				}
				tx = where(tx, "NOT "+query, v1, v2)
			} else {
				query, v1, v2, v3, err := parseNonAtomizedToken(token[1:])
				if err != nil {
					return nil, err
				}
				tx = tx.Where("NOT "+query, v1, v2, v3)
			}
		} else if containsUnescaped(token, ":") {
			query, v1, v2, err := parseAtomizedToken(ctx, token)
			if err != nil {
				return nil, err
			}
			tx = where(tx, query, v1, v2)
		} else {
			query, v1, v2, v3, err := parseNonAtomizedToken(token)
			if err != nil {
				return nil, err
			}
			tx = tx.Where(query, v1, v2, v3)
		}
	}
	return tx, nil
}

func Search(ctx context.Context, db *gorm.DB, query string, limit int) ([]*data.HistoryEntry, error) {
	return retryingSearch(ctx, db, query, limit, 0)
}

const SEARCH_RETRY_COUNT = 3

func retryingSearch(ctx context.Context, db *gorm.DB, query string, limit, currentRetryNum int) ([]*data.HistoryEntry, error) {
	if ctx == nil && query != "" {
		return nil, fmt.Errorf("lib.Search called with a nil context and a non-empty query (this should never happen)")
	}

	tx, err := MakeWhereQueryFromSearch(ctx, db, query)
	if err != nil {
		return nil, err
	}
	if hctx.GetConf(ctx).EnablePresaving {
		// Sort by StartTime when presaving is enabled, since presaved entries may not have an end time
		tx = tx.Order("start_time DESC")
	} else {
		tx = tx.Order("end_time DESC")
	}
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	var historyEntries []*data.HistoryEntry
	result := tx.Find(&historyEntries)
	if result.Error != nil {
		if strings.Contains(result.Error.Error(), SQLITE_LOCKED_ERR_MSG) && currentRetryNum < SEARCH_RETRY_COUNT {
			hctx.GetLogger().Infof("Ignoring err=%v and retrying search query, cnt=%d", result.Error, currentRetryNum)
			time.Sleep(time.Duration(currentRetryNum*rand.Intn(50)) * time.Millisecond)
			return retryingSearch(ctx, db, query, limit, currentRetryNum+1)
		}
		return nil, fmt.Errorf("DB query error: %w", result.Error)
	}
	return historyEntries, nil
}

func parseNonAtomizedToken(token string) (string, any, any, any, error) {
	wildcardedToken := "%" + unescape(token) + "%"
	return "(command LIKE ? OR hostname LIKE ? OR current_working_directory LIKE ?)", wildcardedToken, wildcardedToken, wildcardedToken, nil
}

func parseAtomizedToken(ctx context.Context, token string) (string, any, any, error) {
	splitToken := splitEscaped(token, ':', 2)
	field := unescape(splitToken[0])
	val := unescape(splitToken[1])
	switch field {
	case "user":
		return "(local_username = ?)", val, nil, nil
	case "host":
		fallthrough
	case "hostname":
		return "(instr(hostname, ?) > 0)", val, nil, nil
	case "cwd":
		return "(instr(current_working_directory, ?) > 0 OR instr(REPLACE(current_working_directory, '~/', home_directory), ?) > 0)", strings.TrimSuffix(val, "/"), strings.TrimSuffix(val, "/"), nil
	case "exit_code":
		return "(exit_code = ?)", val, nil, nil
	case "before":
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse before:%s as a timestamp: %w", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) < ?)", t.Unix(), nil, nil
	case "after":
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse after:%s as a timestamp: %w", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) > ?)", t.Unix(), nil, nil
	case "start_time":
		// Note that this atom probably isn't useful for interactive usage since it does exact matching, but we use it
		// internally for pre-saving history entries.
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse start_time:%s as a timestamp: %w", val, err)
		}
		return "(CAST(strftime(\"%s\",start_time) AS INTEGER) = ?)", strconv.FormatInt(t.Unix(), 10), nil, nil
	case "end_time":
		// Note that this atom probably isn't useful for interactive usage since it does exact matching, but we use it
		// internally for pre-saving history entries.
		t, err := parseTimeGenerously(val)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse end_time:%s as a timestamp: %w", val, err)
		}
		return "(CAST(strftime(\"%s\",end_time) AS INTEGER) = ?)", strconv.FormatInt(t.Unix(), 10), nil, nil
	case "command":
		return "(instr(command, ?) > 0)", val, nil, nil
	default:
		knownCustomColumns := make([]string, 0)
		// Get custom columns that are defined on this machine
		conf := hctx.GetConf(ctx)
		for _, c := range conf.CustomColumns {
			knownCustomColumns = append(knownCustomColumns, c.ColumnName)
		}
		// Also get all ones that are in the DB
		names, err := getAllCustomColumnNames(ctx)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to get custom column names from the DB: %w", err)
		}
		knownCustomColumns = append(knownCustomColumns, names...)
		// Check if the atom is for a custom column that exists and if it isn't, return an error
		isCustomColumn := false
		for _, ccName := range knownCustomColumns {
			if ccName == field {
				isCustomColumn = true
			}
		}
		if !isCustomColumn {
			return "", nil, nil, fmt.Errorf("search query contains unknown search atom '%s' that doesn't match any column names", field)
		}
		// Build the where clause for the custom column
		return "EXISTS (SELECT 1 FROM json_each(custom_columns) WHERE json_extract(value, '$.name') = ? and instr(json_extract(value, '$.value'), ?) > 0)", field, val, nil
	}
}

func getAllCustomColumnNames(ctx context.Context) ([]string, error) {
	db := hctx.GetDb(ctx)
	rows, err := RetryingDbFunctionWithResult(func() (*sql.Rows, error) {
		query := `
		SELECT DISTINCT json_extract(value, '$.name') as cc_name
		FROM history_entries 
		JOIN json_each(custom_columns)
		WHERE value IS NOT NULL`
		return db.Raw(query).Rows()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query for list of custom columns: %w", err)
	}
	ccNames := make([]string, 0)
	for rows.Next() {
		var ccName string
		err = rows.Scan(&ccName)
		if err != nil {
			return nil, err
		}
		ccNames = append(ccNames, ccName)
	}
	return ccNames, nil
}

func tokenize(query string) []string {
	if query == "" {
		return []string{}
	}
	return splitEscaped(query, ' ', -1)
}

// TODO: Maybe add support for searching for the backslash character itself?
func splitEscaped(query string, separator rune, maxSplit int) []string {
	var token []rune
	var tokens []string
	splits := 1
	runeQuery := []rune(query)
	isInDoubleQuotedString := false
	isInSingleQuotedString := false
	for i := 0; i < len(runeQuery); i++ {
		if (maxSplit < 0 || splits < maxSplit) && runeQuery[i] == separator && !isInSingleQuotedString && !isInDoubleQuotedString {
			tokens = append(tokens, string(token))
			token = token[:0]
			splits++
		} else if runeQuery[i] == '\\' && i+1 < len(runeQuery) {
			if runeQuery[i+1] == '-' || runeQuery[i+1] == ':' || runeQuery[i+1] == '\\' {
				// Note that we need to keep the backslash before the dash to support searches like `ls \-Slah`.
				// And we need it before the colon so that we can search for things like `foo\:bar`
				// And we need it before the backslash so that we can search for literal backslashes.
				token = append(token, runeQuery[i])
			}
			i++
			token = append(token, runeQuery[i])
		} else if runeQuery[i] == '"' && !isInSingleQuotedString && !heuristicIgnoreUnclosedQuote(isInDoubleQuotedString, '"', runeQuery, i) {
			isInDoubleQuotedString = !isInDoubleQuotedString
		} else if runeQuery[i] == '\'' && !isInDoubleQuotedString && !heuristicIgnoreUnclosedQuote(isInSingleQuotedString, '\'', runeQuery, i) {
			isInSingleQuotedString = !isInSingleQuotedString
		} else {
			if (isInSingleQuotedString || isInDoubleQuotedString) && separator == ' ' && runeQuery[i] == ':' {
				token = append(token, '\\')
			}
			token = append(token, runeQuery[i])
		}
	}
	tokens = append(tokens, string(token))
	return tokens
}

func heuristicIgnoreUnclosedQuote(isCurrentlyInQuotedString bool, quoteType rune, query []rune, idx int) bool {
	if isCurrentlyInQuotedString {
		// We're already in a quoted string, so the heuristic doesn't apply
		return false
	}
	idx++
	for idx < len(query) {
		if query[idx] == quoteType {
			// There is a close quote, so the heuristic doesn't apply
			return false
		}
		idx++
	}
	// There is no unclosed quote, so we apply the heuristic and ignore the single quote
	return true
}

func containsUnescaped(query, token string) bool {
	runeQuery := []rune(query)
	for i := 0; i < len(runeQuery); i++ {
		if runeQuery[i] == '\\' && i+1 < len(runeQuery) {
			i++
		} else if string(runeQuery[i:i+len(token)]) == token {
			return true
		}
	}
	return false
}

func unescape(query string) string {
	runeQuery := []rune(query)
	var newQuery []rune
	for i := 0; i < len(runeQuery); i++ {
		if runeQuery[i] == '\\' {
			i++
		}
		if i < len(runeQuery) {
			newQuery = append(newQuery, runeQuery[i])
		}
	}
	return string(newQuery)
}

func SendDeletionRequest(ctx context.Context, deletionRequest shared.DeletionRequest) error {
	data, err := json.Marshal(deletionRequest)
	if err != nil {
		return err
	}
	_, err = ApiPost(ctx, "/api/v1/add-deletion-request", "application/json", data)
	if err != nil {
		return fmt.Errorf("failed to send deletion request to backend service, this may cause commands to not get deleted on other instances of hishtory: %w", err)
	}
	return nil
}
