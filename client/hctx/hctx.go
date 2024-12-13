package hctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/tui/keybindings"
	"github.com/ddworken/hishtory/shared"

	// Needed to use sqlite without CGO
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	hishtoryLogger *logrus.Logger
	getLoggerOnce  sync.Once
)

func GetLogger() *logrus.Logger {
	getLoggerOnce.Do(func() {
		homedir, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Errorf("failed to get user's home directory: %w", err))
		}
		err = MakeHishtoryDir()
		if err != nil {
			panic(err)
		}

		lumberjackLogger := &lumberjack.Logger{
			Filename:   path.Join(homedir, data.GetHishtoryPath(), "hishtory.log"),
			MaxSize:    1, // MB
			MaxBackups: 1,
			MaxAge:     30, // days
		}

		logFormatter := new(logrus.TextFormatter)
		logFormatter.TimestampFormat = time.RFC3339
		logFormatter.FullTimestamp = true

		hishtoryLogger = logrus.New()
		hishtoryLogger.SetFormatter(logFormatter)
		hishtoryLogger.SetOutput(lumberjackLogger)

		// Configure the log level from the config file, if the config file exists
		hishtoryLogger.SetLevel(logrus.InfoLevel)
		cfg, err := GetConfig()
		if err == nil {
			hishtoryLogger.SetLevel(cfg.LogLevel)
		}
	})
	return hishtoryLogger
}

func MakeHishtoryDir() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %w", err)
	}
	err = os.MkdirAll(path.Join(homedir, data.GetHishtoryPath()), 0o744)
	if err != nil {
		return fmt.Errorf("failed to create ~/%s dir: %w", data.GetHishtoryPath(), err)
	}
	return nil
}

func OpenLocalSqliteDb() (*gorm.DB, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user's home directory: %w", err)
	}
	err = MakeHishtoryDir()
	if err != nil {
		return nil, err
	}
	newLogger := logger.New(
		GetLogger().WithField("fromSQL", true),
		logger.Config{
			SlowThreshold:             100 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		},
	)
	dbFilePath := path.Join(homedir, data.GetHishtoryPath(), data.DB_PATH)
	dsn := fmt.Sprintf("file:%s?mode=rwc&_journal_mode=WAL", dbFilePath)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true, Logger: newLogger})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %w", err)
	}
	tx, err := db.DB()
	if err != nil {
		return nil, err
	}
	err = tx.Ping()
	if err != nil {
		return nil, err
	}
	db.AutoMigrate(&data.HistoryEntry{})
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("CREATE INDEX IF NOT EXISTS start_time_index ON history_entries(start_time)")
	db.Exec("CREATE INDEX IF NOT EXISTS end_time_index ON history_entries(end_time)")
	db.Exec("CREATE INDEX IF NOT EXISTS entry_id_index ON history_entries(entry_id)")
	return db, nil
}

type hishtoryContextKey string

const (
	ConfigCtxKey  hishtoryContextKey = "config"
	DbCtxKey      hishtoryContextKey = "db"
	HomedirCtxKey hishtoryContextKey = "homedir"
)

func MakeContext() context.Context {
	ctx := context.Background()
	config, err := GetConfig()
	if err != nil {
		panic(fmt.Errorf("failed to retrieve config: %w", err))
	}
	ctx = context.WithValue(ctx, ConfigCtxKey, &config)
	db, err := OpenLocalSqliteDb()
	if err != nil {
		panic(fmt.Errorf("failed to open local DB: %w", err))
	}
	ctx = context.WithValue(ctx, DbCtxKey, db)
	homedir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("failed to get homedir: %w", err))
	}
	ctx = context.WithValue(ctx, HomedirCtxKey, homedir)
	return ctx
}

func GetConf(ctx context.Context) *ClientConfig {
	v := ctx.Value(ConfigCtxKey)
	if v != nil {
		return (v.(*ClientConfig))
	}
	panic(fmt.Errorf("failed to find config in ctx"))
}

func GetDb(ctx context.Context) *gorm.DB {
	v := ctx.Value(DbCtxKey)
	if v != nil {
		return v.(*gorm.DB)
	}
	panic(fmt.Errorf("failed to find db in ctx"))
}

func GetHome(ctx context.Context) string {
	v := ctx.Value(HomedirCtxKey)
	if v != nil {
		return v.(string)
	}
	panic(fmt.Errorf("failed to find homedir in ctx"))
}

type ClientConfig struct {
	// The user secret that is used to derive encryption keys for syncing history entries
	UserSecret string `json:"user_secret" yaml:"-"`
	// Whether hishtory recording is enabled
	IsEnabled bool `json:"is_enabled" yaml:"-"`
	// A device ID used to track which history entry came from which device for remote syncing
	DeviceId string `json:"device_id" yaml:"-"`
	// Used for skipping history entries prefixed with a space in bash
	LastPreSavedHistoryLine string `json:"last_presaved_history_line" yaml:"-"`
	// Used for skipping history entries prefixed with a space in bash
	LastSavedHistoryLine string `json:"last_saved_history_line" yaml:"-"`
	// Used for uploading history entries that we failed to upload due to a missing network connection
	HaveMissedUploads     bool  `json:"have_missed_uploads" yaml:"-"`
	MissedUploadTimestamp int64 `json:"missed_upload_timestamp" yaml:"-"`
	// Used for uploading deletion requests that we failed to upload due to a missed network connection
	// Note that this is only applicable for deleting pre-saved entries. For interactive deletion, we just
	// show the user an error message if they're offline.
	PendingDeletionRequests []shared.DeletionRequest `json:"pending_deletion_requests" yaml:"-"`
	// Used for avoiding double imports of .bash_history
	HaveCompletedInitialImport bool `json:"have_completed_initial_import" yaml:"-"`
	// Whether control-r bindings are enabled
	ControlRSearchEnabled bool `json:"enable_control_r_search"`
	// The set of columns that the user wants to be displayed
	DisplayedColumns []string `json:"displayed_columns"`
	// Custom columns
	CustomColumns []CustomColumnDefinition `json:"custom_columns"`
	// Whether to force enable a compact mode for the TUI
	ForceCompactMode bool `json:"force_compact_mode"`
	// Whether this is an offline instance of hishtory with no syncing
	IsOffline bool `json:"is_offline"`
	// Whether duplicate commands should be displayed
	FilterDuplicateCommands bool `json:"filter_duplicate_commands"`
	// A format string for the timestamp
	TimestampFormat string `json:"timestamp_format"`
	// Beta mode, enables unspecified additional beta features
	// Currently: This enables pre-saving of history entries to better handle long-running commands
	BetaMode bool `json:"beta_mode"`
	// Whether to highlight matches in search results
	HighlightMatches bool `json:"highlight_matches"`
	// Whether to enable AI completion
	AiCompletion bool `json:"ai_completion"`
	// Whether to enable presaving
	EnablePresaving bool `json:"enable_presaving"`
	// The current color scheme for the TUI
	ColorScheme ColorScheme `json:"color_scheme"`
	// A default filter that will be applied to all search queries
	DefaultFilter string `json:"default_filter"`
	// The endpoint to use for AI suggestions
	AiCompletionEndpoint string `json:"ai_completion_endpoint"`
	// Custom key bindings for the TUI
	KeyBindings keybindings.SerializableKeyMap `json:"key_bindings"`
	// The log level for hishtory (e.g., "debug", "info", "warn", "error")
	LogLevel logrus.Level `json:"log_level"`
	// Whether the TUI should render in full-screen mode
	FullScreenRendering bool `json:"full_screen_rendering"`
}

type ColorScheme struct {
	SelectedText       string
	SelectedBackground string
	BorderColor        string
}

type CustomColumnDefinition struct {
	ColumnName    string `json:"column_name"`
	ColumnCommand string `json:"column_command"`
}

func GetConfigContents() ([]byte, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve homedir: %w", err)
	}
	dat, err := os.ReadFile(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH))
	if err != nil {
		files, err := os.ReadDir(path.Join(homedir, data.GetHishtoryPath()))
		if err != nil {
			return nil, fmt.Errorf("failed to read config file (and failed to list too): %w", err)
		}
		filenames := ""
		for _, file := range files {
			filenames += file.Name()
			filenames += ", "
		}
		return nil, fmt.Errorf("failed to read config file (files in HISHTORY_PATH: %s): %w", filenames, err)
	}
	return dat, nil
}

func GetDefaultColorScheme() ColorScheme {
	return ColorScheme{
		SelectedBackground: "#3300ff",
		SelectedText:       "#ffff99",
		BorderColor:        "#585858",
	}
}

func GetConfig() (ClientConfig, error) {
	data, err := GetConfigContents()
	if err != nil {
		return ClientConfig{}, err
	}
	var config ClientConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to parse config file: %w", err)
	}
	config.KeyBindings = config.KeyBindings.WithDefaults()
	if len(config.DisplayedColumns) == 0 {
		config.DisplayedColumns = []string{"Hostname", "CWD", "Timestamp", "Runtime", "Exit Code", "Command"}
	}
	if config.TimestampFormat == "" {
		config.TimestampFormat = "Jan 2 2006 15:04:05 MST"
	}
	if config.ColorScheme.SelectedBackground == "" {
		config.ColorScheme.SelectedBackground = GetDefaultColorScheme().SelectedBackground
	}
	if config.ColorScheme.SelectedText == "" {
		config.ColorScheme.SelectedText = GetDefaultColorScheme().SelectedText
	}
	if config.ColorScheme.BorderColor == "" {
		config.ColorScheme.BorderColor = GetDefaultColorScheme().BorderColor
	}
	if config.AiCompletionEndpoint == "" {
		config.AiCompletionEndpoint = "https://api.openai.com/v1/chat/completions"
	}
	if config.LogLevel == logrus.Level(0) {
		config.LogLevel = logrus.InfoLevel
	}
	return config, nil
}

func SetConfig(config *ClientConfig) error {
	serializedConfig, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to retrieve homedir: %w", err)
	}
	err = MakeHishtoryDir()
	if err != nil {
		return err
	}
	configPath := path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH)
	stagedConfigPath := configPath + ".tmp-" + uuid.Must(uuid.NewRandom()).String()
	err = os.WriteFile(stagedConfigPath, serializedConfig, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	err = os.Rename(stagedConfigPath, configPath)
	if err != nil {
		return fmt.Errorf("failed to replace config file with the updated version: %w", err)
	}
	return nil
}

func InitConfig() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_, err = os.Stat(path.Join(homedir, data.GetHishtoryPath(), data.CONFIG_PATH))
	if errors.Is(err, os.ErrNotExist) {
		return SetConfig(&ClientConfig{})
	}
	return err
}
