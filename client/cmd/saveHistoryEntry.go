package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var getTimestampCmd = &cobra.Command{
	Use:    "getTimestamp",
	Hidden: true,
	Short:  "[Internal-only] Returns a timestamp in Unix nanoseconds",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(time.Now().UnixNano())
	},
}

var saveHistoryEntryCmd = &cobra.Command{
	Use:                "saveHistoryEntry",
	Hidden:             true,
	Short:              "[Internal-only] The command used to save history entries",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		lib.CheckFatalError(maybeUploadSkippedHistoryEntries(ctx))
		lib.CheckFatalError(maybeSubmitPendingDeletionRequests(ctx))
		saveHistoryEntry(ctx)
	},
}

var presaveHistoryEntryCmd = &cobra.Command{
	Use:                "presaveHistoryEntry",
	Hidden:             true,
	Short:              "[Internal-only] The command used to pre-save history entries that haven't yet finished running",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		presaveHistoryEntry(ctx)
	},
}

func maybeSubmitPendingDeletionRequests(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	if config.IsOffline {
		return nil
	}
	if len(config.PendingDeletionRequests) == 0 {
		return nil
	}

	// Upload the missing deletion requests
	for _, dr := range config.PendingDeletionRequests {
		err := lib.SendDeletionRequest(ctx, dr)
		if lib.IsOfflineError(ctx, err) {
			// We're still offline, so nothing to do
			return nil
		}
		if err != nil {
			return err
		}
	}

	// Mark down that we sent all of them
	config.PendingDeletionRequests = make([]shared.DeletionRequest, 0)
	return hctx.SetConfig(config)
}

func maybeUploadSkippedHistoryEntries(ctx context.Context) error {
	config := hctx.GetConf(ctx)
	if !config.HaveMissedUploads {
		return nil
	}
	if config.IsOffline {
		return nil
	}

	// Upload the missing entries
	db := hctx.GetDb(ctx)
	query := fmt.Sprintf("after:%s", time.Unix(config.MissedUploadTimestamp, 0).Format("2006-01-02"))
	entries, err := lib.Search(ctx, db, query, 0)
	if err != nil {
		return fmt.Errorf("failed to retrieve history entries that haven't been uploaded yet: %w", err)
	}
	hctx.GetLogger().Infof("Uploading %d history entries that previously failed to upload (query=%#v)\n", len(entries), query)
	jsonValue, err := lib.EncryptAndMarshal(config, entries)
	if err != nil {
		return err
	}
	_, err = lib.ApiPost(ctx, "/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
	if err != nil {
		// Failed to upload the history entry, so we must still be offline. So just return nil and we'll try again later.
		return nil
	}

	// Mark down that we persisted it
	config.HaveMissedUploads = false
	config.MissedUploadTimestamp = 0
	err = hctx.SetConfig(config)
	if err != nil {
		return fmt.Errorf("failed to mark a history entry as uploaded: %w", err)
	}
	return nil
}

func handlePotentialUploadFailure(ctx context.Context, err error, config *hctx.ClientConfig, entryTimestamp time.Time) {
	if err != nil {
		if lib.IsOfflineError(ctx, err) {
			hctx.GetLogger().Infof("Failed to remotely persist hishtory entry because we failed to connect to the remote server! This is likely because the device is offline, but also could be because the remote server is having reliability issues. Original error: %v", err)
			if !config.HaveMissedUploads {
				config.HaveMissedUploads = true
				// Set MissedUploadTimestamp to `entry timestamp - 1` so that the current entry will get
				// uploaded once network access is regained.
				config.MissedUploadTimestamp = entryTimestamp.UTC().Unix() - 1
				lib.CheckFatalError(hctx.SetConfig(config))
			}
		} else {
			lib.CheckFatalError(err)
		}
	}
}

func presaveHistoryEntry(ctx context.Context) {
	config := hctx.GetConf(ctx)
	if !config.IsEnabled {
		return
	}
	if !config.BetaMode {
		return
	}

	// Build the basic entry with metadata retrieved from runtime
	entry, err := buildPreArgsHistoryEntry(ctx)
	lib.CheckFatalError(err)
	if entry == nil {
		return
	}

	// Augment it with os.Args
	shellName := os.Args[2]
	cmd, err := extractCommandFromArg(ctx, shellName, os.Args[3] /* isPresave = */, true)
	lib.CheckFatalError(err)
	entry.Command = cmd
	if strings.HasPrefix(entry.Command, " ") || strings.TrimSpace(entry.Command) == "" {
		// Don't save commands that start with a space
		return
	}
	entry.StartTime = parseCrossPlatformTime(os.Args[4])
	entry.EndTime = time.Unix(0, 0).UTC()

	// And persist it locally.
	db := hctx.GetDb(ctx)
	err = lib.ReliableDbCreate(db, *entry)
	lib.CheckFatalError(err)
	db.Commit()

	// And persist it remotely
	if !config.IsOffline {
		jsonValue, err := lib.EncryptAndMarshal(config, []*data.HistoryEntry{entry})
		lib.CheckFatalError(err)
		_, err = lib.ApiPost(ctx, "/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
		handlePotentialUploadFailure(ctx, err, config, entry.StartTime)
	}
}

func saveHistoryEntry(ctx context.Context) {
	config := hctx.GetConf(ctx)
	if !config.IsEnabled {
		hctx.GetLogger().Infof("Skipping saving a history entry because hishtory is disabled\n")
		return
	}
	entry, err := buildHistoryEntry(ctx, os.Args)
	lib.CheckFatalError(err)
	if entry == nil {
		hctx.GetLogger().Infof("Skipping saving a history entry because we did not build a history entry (was the command prefixed with a space and/or empty?)\n")
		return
	}
	db := hctx.GetDb(ctx)

	// Drop any entries from pre-saving since they're no longer needed
	if config.BetaMode {
		lib.CheckFatalError(deletePresavedEntries(ctx, entry, false))
	}

	// Persist it locally
	err = lib.ReliableDbCreate(db, *entry)
	lib.CheckFatalError(err)

	// Persist it remotely
	if !config.IsOffline {
		jsonValue, err := lib.EncryptAndMarshal(config, []*data.HistoryEntry{entry})
		lib.CheckFatalError(err)
		w, err := lib.ApiPost(ctx, "/api/v1/submit?source_device_id="+config.DeviceId, "application/json", jsonValue)
		handlePotentialUploadFailure(ctx, err, config, entry.StartTime)
		if err == nil {
			submitResponse := shared.SubmitResponse{}
			err := json.Unmarshal(w, &submitResponse)
			if err != nil {
				lib.CheckFatalError(fmt.Errorf("failed to deserialize response from /api/v1/submit: %w", err))
			}
			lib.CheckFatalError(lib.HandleDeletionRequests(ctx, submitResponse.DeletionRequests))
			lib.CheckFatalError(handleDumpRequests(ctx, submitResponse.DumpRequests))
		}
	}

	if config.BetaMode {
		db.Commit()
	}
}

func deletePresavedEntries(ctx context.Context, entry *data.HistoryEntry, isRetry bool) error {
	db := hctx.GetDb(ctx)

	// Create the query to find the presaved entries
	query := "cwd:" + entry.CurrentWorkingDirectory
	query += " start_time:" + strconv.FormatInt(entry.StartTime.Unix(), 10)
	query += " end_time:1970/01/01_00:00:00_+00:00"
	matchingEntryQuery, err := lib.MakeWhereQueryFromSearch(ctx, db, query)
	if err != nil {
		return fmt.Errorf("failed to query for pre-saved history entry: %w", err)
	}
	matchingEntryQuery = matchingEntryQuery.Where("command = ?", entry.Command).Where("device_id = ?", entry.DeviceId).Session(&gorm.Session{})

	// Get the presaved entry since we need it for doing remote deletes
	presavedEntry, err := lib.RetryingDbFunctionWithResult(func() (data.HistoryEntry, error) {
		var presavedEntry data.HistoryEntry
		res := matchingEntryQuery.Find(&presavedEntry)
		if res.Error != nil {
			return presavedEntry, fmt.Errorf("failed to search for presaved entry for cmd=%#v: %w", entry.Command, res.Error)
		}
		return presavedEntry, nil
	})
	if err != nil {
		return err
	}
	if reflect.ValueOf(presavedEntry).IsZero() {
		// Presaved entry is zero, aka there is no presaved entry. This can happen either due to:
		//
		// 1. A failure in presaving, or this feature was just enabled (in which case there is nothing to do here)
		// 2. A race condition where presaving hasn't finished, but we're looking for the entry here
		//
		// We want to ensure this isn't case #2. There isn't a great way to do this, but we can just retry
		// this function after a short delay. If it still is empty, then we assume we are in case #1.
		if isRetry {
			// Already retried, assume we're in case #1
			hctx.GetLogger().Infof("failed to find presaved entry even with retry, skipping delete")
			return nil
		} else {
			time.Sleep(500 * time.Millisecond)
			return deletePresavedEntries(ctx, entry, true)
		}
	}

	// Delete presaved entries locally
	deletePresavedEntryFunc := func() error {
		res := matchingEntryQuery.Delete(&data.HistoryEntry{})
		if res.Error != nil {
			return fmt.Errorf("failed to delete pre-saved history entry (expected command=%#v): %w", entry.Command, res.Error)
		}
		return nil
	}
	err = lib.RetryingDbFunction(deletePresavedEntryFunc)
	if err != nil {
		return err
	}

	// And delete it remotely
	config := hctx.GetConf(ctx)
	if !config.IsOffline {
		var deletionRequest shared.DeletionRequest
		deletionRequest.SendTime = time.Now()
		deletionRequest.UserId = data.UserId(config.UserSecret)
		deletionRequest.Messages.Ids = append(deletionRequest.Messages.Ids,
			// Note that we aren't specifying an EndTime here since pre-saved entries don't have an EndTime
			shared.MessageIdentifier{DeviceId: presavedEntry.DeviceId, EntryId: presavedEntry.EntryId},
		)
		err = lib.SendDeletionRequest(ctx, deletionRequest)
		if lib.IsOfflineError(ctx, err) {
			// Cache the deletion request to send once the client comes back online
			config.PendingDeletionRequests = append(config.PendingDeletionRequests, deletionRequest)
			return hctx.SetConfig(config)
		}
		return err
	}
	return nil
}

func handleDumpRequests(ctx context.Context, dumpRequests []*shared.DumpRequest) error {
	db := hctx.GetDb(ctx)
	config := hctx.GetConf(ctx)
	if len(dumpRequests) > 0 {
		lib.CheckFatalError(lib.RetrieveAdditionalEntriesFromRemote(ctx, "newclient"))
		entries, err := lib.Search(ctx, db, "", 0)
		lib.CheckFatalError(err)
		var encEntries []*shared.EncHistoryEntry
		for _, entry := range entries {
			enc, err := data.EncryptHistoryEntry(config.UserSecret, *entry)
			lib.CheckFatalError(err)
			encEntries = append(encEntries, &enc)
		}
		reqBody, err := json.Marshal(encEntries)
		lib.CheckFatalError(err)
		for _, dumpRequest := range dumpRequests {
			if !config.IsOffline {
				// TODO: Test whether this fails if the data is extremely large? It may need to be chunked
				_, err := lib.ApiPost(ctx, "/api/v1/submit-dump?user_id="+dumpRequest.UserId+"&requesting_device_id="+dumpRequest.RequestingDeviceId+"&source_device_id="+config.DeviceId, "application/json", reqBody)
				lib.CheckFatalError(err)
			}
		}
	}
	return nil
}

func buildPreArgsHistoryEntry(ctx context.Context) (*data.HistoryEntry, error) {
	var entry data.HistoryEntry

	// user
	user, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %w", err)
	}
	entry.LocalUsername = user.Username

	// cwd and homedir
	cwd, homedir, err := getCwd(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %w", err)
	}
	entry.CurrentWorkingDirectory = cwd
	entry.HomeDirectory = homedir

	// hostname
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %w", err)
	}
	entry.Hostname = hostname

	// device ID
	config := hctx.GetConf(ctx)
	entry.DeviceId = config.DeviceId

	// entry ID
	entry.EntryId = uuid.Must(uuid.NewRandom()).String()

	// custom columns
	cc, err := buildCustomColumns(ctx)
	if err != nil {
		return nil, err
	}
	entry.CustomColumns = cc

	return &entry, nil
}

func buildHistoryEntry(ctx context.Context, args []string) (*data.HistoryEntry, error) {
	if len(args) < 6 {
		hctx.GetLogger().Warnf("buildHistoryEntry called with args=%#v, which has too few entries! This can happen in specific edge cases for newly opened terminals and is likely not a problem.", args)
		return nil, nil
	}
	shell := args[2]

	entry, err := buildPreArgsHistoryEntry(ctx)
	if err != nil {
		return nil, err
	}

	// exitCode
	exitCode, err := strconv.Atoi(args[3])
	if err != nil {
		return nil, fmt.Errorf("failed to build history entry: %w", err)
	}
	entry.ExitCode = exitCode

	// start time
	entry.StartTime = parseCrossPlatformTime(args[5])

	// end time
	entry.EndTime = time.Now().UTC()

	// command
	cmd, err := extractCommandFromArg(ctx, shell, args[4] /* isPresave = */, false)
	if err != nil {
		return nil, err
	}
	entry.Command = cmd
	if strings.TrimSpace(entry.Command) == "" {
		// Skip recording empty commands where the user just hits enter in their terminal
		return nil, nil
	}

	return entry, nil
}

func extractCommandFromArg(ctx context.Context, shell, arg string, isPresave bool) (string, error) {
	if shell == "bash" {
		cmd, err := getLastCommand(arg)
		if err != nil {
			return "", fmt.Errorf("failed to build history entry: %w", err)
		}
		shouldBeSkipped, err := shouldSkipHiddenCommand(ctx, arg, isPresave)
		if err != nil {
			return "", fmt.Errorf("failed to check if command was hidden: %w", err)
		}
		if shouldBeSkipped || strings.HasPrefix(cmd, " ") {
			// Don't save commands that start with a space
			return "", nil
		}
		cmd, err = maybeSkipBashHistTimePrefix(cmd)
		if err != nil {
			return "", err
		}
		return cmd, nil
	} else if shell == "zsh" || shell == "fish" {
		cmd := trimTrailingWhitespace(arg)
		if strings.HasPrefix(cmd, " ") {
			// Don't save commands that start with a space
			return "", nil
		}
		return cmd, nil
	} else {
		return "", fmt.Errorf("tried to save a hishtory entry from an unsupported shell=%#v", shell)
	}

}

func trimTrailingWhitespace(s string) string {
	return strings.TrimSuffix(strings.TrimSuffix(s, "\n"), " ")
}

func buildCustomColumns(ctx context.Context) (data.CustomColumns, error) {
	ccs := data.CustomColumns{}
	config := hctx.GetConf(ctx)
	for _, cc := range config.CustomColumns {
		cmd := exec.Command("bash", "-c", cc.ColumnCommand)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Start()
		if err != nil {
			return nil, fmt.Errorf("failed to execute custom command named %v (stdout=%#v, stderr=%#v)", cc.ColumnName, stdout.String(), stderr.String())
		}
		err = cmd.Wait()
		if err != nil {
			// Log a warning, but don't crash. This way commands can exit with a different status and still work.
			hctx.GetLogger().Warnf("failed to execute custom command named %v (stdout=%#v, stderr=%#v)", cc.ColumnName, stdout.String(), stderr.String())
		}
		ccv := data.CustomColumn{
			Name: cc.ColumnName,
			Val:  strings.TrimSpace(stdout.String()),
		}
		ccs = append(ccs, ccv)
	}
	return ccs, nil
}

func buildRegexFromTimeFormat(timeFormat string) string {
	expectedRegex := ""
	lastCharWasPercent := false
	for _, char := range timeFormat {
		if lastCharWasPercent {
			if char == '%' {
				expectedRegex += regexp.QuoteMeta(string(char))
				lastCharWasPercent = false
				continue
			} else if char == 't' {
				expectedRegex += "\t"
			} else if char == 'F' {
				expectedRegex += buildRegexFromTimeFormat("%Y-%m-%d")
			} else if char == 'Y' {
				expectedRegex += "[0-9]{4}"
			} else if char == 'G' {
				expectedRegex += "[0-9]{4}"
			} else if char == 'g' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'C' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'u' || char == 'w' {
				expectedRegex += "[0-9]"
			} else if char == 'm' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'd' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'D' {
				expectedRegex += buildRegexFromTimeFormat("%m/%d/%y")
			} else if char == 'T' {
				expectedRegex += buildRegexFromTimeFormat("%H:%M:%S")
			} else if char == 'H' || char == 'I' || char == 'U' || char == 'V' || char == 'W' || char == 'y' || char == 'Y' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'M' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'j' {
				expectedRegex += "[0-9]{3}"
			} else if char == 'S' || char == 'm' {
				expectedRegex += "[0-9]{2}"
			} else if char == 'c' {
				// Note: Specific to the POSIX locale
				expectedRegex += buildRegexFromTimeFormat("%a %b %e %H:%M:%S %Y")
			} else if char == 'a' {
				// Note: Specific to the POSIX locale
				expectedRegex += "(Sun|Mon|Tue|Wed|Thu|Fri|Sat)"
			} else if char == 'b' || char == 'h' {
				// Note: Specific to the POSIX locale
				expectedRegex += "(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)"
			} else if char == 'e' || char == 'k' || char == 'l' {
				expectedRegex += "[0-9 ]{2}"
			} else if char == 'n' {
				expectedRegex += "\n"
			} else if char == 'p' {
				expectedRegex += "(AM|PM)"
			} else if char == 'P' {
				expectedRegex += "(am|pm)"
			} else if char == 's' {
				expectedRegex += "\\d+"
			} else if char == 'z' {
				expectedRegex += "[+-][0-9]{4}"
			} else if char == 'r' {
				expectedRegex += buildRegexFromTimeFormat("%I:%M:%S %p")
			} else if char == 'R' {
				expectedRegex += buildRegexFromTimeFormat("%H:%M")
			} else if char == 'x' {
				expectedRegex += buildRegexFromTimeFormat("%m/%d/%y")
			} else if char == 'X' {
				expectedRegex += buildRegexFromTimeFormat("%H:%M:%S")
			} else {
				panic(fmt.Sprintf("buildRegexFromTimeFormat doesn't support %%%v, please open a bug against github.com/ddworken/hishtory", string(char)))
			}
		} else if char != '%' {
			expectedRegex += regexp.QuoteMeta(string(char))
		}
		lastCharWasPercent = false
		if char == '%' {
			lastCharWasPercent = true
		}
	}
	return expectedRegex
}

func maybeSkipBashHistTimePrefix(cmdLine string) (string, error) {
	format := os.Getenv("HISTTIMEFORMAT")
	if format == "" {
		return cmdLine, nil
	}
	re, err := regexp.Compile("^" + buildRegexFromTimeFormat(format))
	if err != nil {
		return "", fmt.Errorf("failed to parse regex for HISTTIMEFORMAT variable: %w", err)
	}
	return re.ReplaceAllLiteralString(cmdLine, ""), nil
}

func parseCrossPlatformTime(data string) time.Time {
	data = strings.TrimSuffix(data, "N") // Trim the N suffix that is present on MacOS where the date CLI doesn't support %N
	startTime, err := strconv.ParseInt(data, 10, 64)
	lib.CheckFatalError(err)
	if len(data) >= 18 {
		return time.Unix(0, startTime).UTC()
	} else {
		return time.Unix(startTime, 0).UTC()
	}

}

func getLastCommand(history string) (string, error) {
	split := strings.SplitN(strings.TrimSpace(history), " ", 2)
	if len(split) <= 1 {
		return "", fmt.Errorf("got unexpected bash history line: %#v, please open a bug at github.com/ddworken/hishtory", history)
	}
	split = strings.SplitN(split[1], " ", 2)
	if len(split) <= 1 {
		return "", fmt.Errorf("got unexpected bash history line: %#v, please open a bug at github.com/ddworken/hishtory", history)
	}
	return split[1], nil
}

func shouldSkipHiddenCommand(ctx context.Context, historyLine string, isPresave bool) (bool, error) {
	config := hctx.GetConf(ctx)
	if isPresave && config.LastPreSavedHistoryLine == historyLine {
		return true, nil
	}
	if !isPresave && config.LastSavedHistoryLine == historyLine {
		return true, nil
	}
	if isPresave {
		config.LastPreSavedHistoryLine = historyLine
	} else {
		config.LastSavedHistoryLine = historyLine
	}
	err := hctx.SetConfig(config)
	if err != nil {
		return false, err
	}
	return false, nil
}

func getCwd(ctx context.Context) (string, string, error) {
	cwd, err := getCwdWithoutSubstitution()
	if err != nil {
		return "", "", fmt.Errorf("failed to get cwd for last command: %w", err)
	}
	homedir := hctx.GetHome(ctx)
	if cwd == homedir {
		return "~/", homedir, nil
	}
	if strings.HasPrefix(cwd, homedir) {
		return strings.Replace(cwd, homedir, "~", 1), homedir, nil
	}
	return cwd, homedir, nil
}

func getCwdWithoutSubstitution() (string, error) {
	cwd, err := os.Getwd()
	if err == nil {
		return cwd, nil
	}
	// Fall back to the syscall to see if that works, as an attempt to
	// fix github.com/ddworken/hishtory/issues/69
	if syscall.ImplementsGetwd {
		cwd, err = syscall.Getwd()
		if err == nil {
			return cwd, nil
		}
	}
	return "", err
}

func init() {
	rootCmd.AddCommand(saveHistoryEntryCmd)
	rootCmd.AddCommand(presaveHistoryEntryCmd)
	rootCmd.AddCommand(getTimestampCmd)
}
