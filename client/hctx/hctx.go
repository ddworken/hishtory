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
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	// Needed to use sqlite without CGO
	"github.com/glebarez/sqlite"
)

var (
	hishtoryLogger *logrus.Logger
	getLoggerOnce  sync.Once
)

func GetLogger() *logrus.Logger {
	getLoggerOnce.Do(func() {
		homedir, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Errorf("failed to get user's home directory: %v", err))
		}
		err = MakeHishtoryDir()
		if err != nil {
			panic(err)
		}

		lumberjackLogger := &lumberjack.Logger{
			Filename:   path.Join(homedir, data.HISHTORY_PATH, "hishtory.log"),
			MaxSize:    1, // MB
			MaxBackups: 10,
			MaxAge:     30, // days
		}

		logFormatter := new(logrus.TextFormatter)
		logFormatter.TimestampFormat = time.RFC3339
		logFormatter.FullTimestamp = true

		hishtoryLogger = logrus.New()
		hishtoryLogger.SetFormatter(logFormatter)
		hishtoryLogger.SetLevel(logrus.InfoLevel)
		hishtoryLogger.SetOutput(lumberjackLogger)
	})
	return hishtoryLogger
}

func MakeHishtoryDir() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %v", err)
	}
	err = os.MkdirAll(path.Join(homedir, data.HISHTORY_PATH), 0o744)
	if err != nil {
		return fmt.Errorf("failed to create ~/.hishtory dir: %v", err)
	}
	return nil
}

func OpenLocalSqliteDb() (*gorm.DB, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user's home directory: %v", err)
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
	dbFilePath := path.Join(homedir, data.HISHTORY_PATH, data.DB_PATH)
	dsn := fmt.Sprintf("file:%s?mode=rwc&_journal_mode=WAL", dbFilePath)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true, Logger: newLogger})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DB: %v", err)
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
	return db, nil
}

type hishtoryContextKey string

func MakeContext() *context.Context {
	ctx := context.Background()
	config, err := GetConfig()
	if err != nil {
		panic(fmt.Errorf("failed to retrieve config: %v", err))
	}
	ctx = context.WithValue(ctx, hishtoryContextKey("config"), config)
	db, err := OpenLocalSqliteDb()
	if err != nil {
		panic(fmt.Errorf("failed to open local DB: %v", err))
	}
	ctx = context.WithValue(ctx, hishtoryContextKey("db"), db)
	homedir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("failed to get homedir: %v", err))
	}
	ctx = context.WithValue(ctx, hishtoryContextKey("homedir"), homedir)
	return &ctx
}

func GetConf(ctx *context.Context) ClientConfig {
	v := (*ctx).Value(hishtoryContextKey("config"))
	if v != nil {
		return v.(ClientConfig)
	}
	panic(fmt.Errorf("failed to find config in ctx"))
}

func GetDb(ctx *context.Context) *gorm.DB {
	v := (*ctx).Value(hishtoryContextKey("db"))
	if v != nil {
		return v.(*gorm.DB)
	}
	panic(fmt.Errorf("failed to find db in ctx"))
}

func GetHome(ctx *context.Context) string {
	v := (*ctx).Value(hishtoryContextKey("homedir"))
	if v != nil {
		return v.(string)
	}
	panic(fmt.Errorf("failed to find homedir in ctx"))
}

type ClientConfig struct {
	// The user secret that is used to derive encryption keys for syncing history entries
	UserSecret string `json:"user_secret"`
	// Whether hishtory recording is enabled
	IsEnabled bool `json:"is_enabled"`
	// A device ID used to track which history entry came from which device for remote syncing
	DeviceId string `json:"device_id"`
	// Used for skipping history entries prefixed with a space in bash
	LastSavedHistoryLine string `json:"last_saved_history_line"`
	// Used for uploading history entries that we failed to upload due to a missing network connection
	HaveMissedUploads     bool  `json:"have_missed_uploads"`
	MissedUploadTimestamp int64 `json:"missed_upload_timestamp"`
	// Used for avoiding double imports of .bash_history
	HaveCompletedInitialImport bool `json:"have_completed_initial_import"`
	// Whether control-r bindings are enabled
	ControlRSearchEnabled bool `json:"enable_control_r_search"`
	// The set of columns that the user wants to be displayed
	DisplayedColumns []string `json:"displayed_columns"`
	// Custom columns
	CustomColumns []CustomColumnDefinition `json:"custom_columns"`
	// Whether this is an offline instance of hishtory with no syncing
	IsOffline bool `json:"is_offline"`
	// Whether duplicate commands should be displayed
	FilterDuplicateCommands bool `json:"filter_duplicate_commands"`
	// A format string for the timestamp
	TimestampFormat string `json:"timestamp_format"`
}

type CustomColumnDefinition struct {
	ColumnName    string `json:"column_name"`
	ColumnCommand string `json:"column_command"`
}

func GetConfigContents() ([]byte, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve homedir: %v", err)
	}
	dat, err := os.ReadFile(path.Join(homedir, data.HISHTORY_PATH, data.CONFIG_PATH))
	if err != nil {
		files, err := os.ReadDir(path.Join(homedir, data.HISHTORY_PATH))
		if err != nil {
			return nil, fmt.Errorf("failed to read config file (and failed to list too): %v", err)
		}
		filenames := ""
		for _, file := range files {
			filenames += file.Name()
			filenames += ", "
		}
		return nil, fmt.Errorf("failed to read config file (files in ~/.hishtory/: %s): %v", filenames, err)
	}
	return dat, nil
}

func GetConfig() (ClientConfig, error) {
	data, err := GetConfigContents()
	if err != nil {
		return ClientConfig{}, err
	}
	var config ClientConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to parse config file: %v", err)
	}
	if config.DisplayedColumns == nil || len(config.DisplayedColumns) == 0 {
		config.DisplayedColumns = []string{"Hostname", "CWD", "Timestamp", "Runtime", "Exit Code", "Command"}
	}
	if config.TimestampFormat == "" {
		config.TimestampFormat = "Jan 2 2006 15:04:05 MST"
	}
	return config, nil
}

func SetConfig(config ClientConfig) error {
	serializedConfig, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %v", err)
	}
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to retrieve homedir: %v", err)
	}
	err = MakeHishtoryDir()
	if err != nil {
		return err
	}
	configPath := path.Join(homedir, data.HISHTORY_PATH, data.CONFIG_PATH)
	stagedConfigPath := configPath + ".tmp"
	err = os.WriteFile(stagedConfigPath, serializedConfig, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}
	err = os.Rename(stagedConfigPath, configPath)
	if err != nil {
		return fmt.Errorf("failed to replace config file with the updated version: %v", err)
	}
	return nil
}

func InitConfig() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_, err = os.Stat(path.Join(homedir, data.HISHTORY_PATH, data.CONFIG_PATH))
	if errors.Is(err, os.ErrNotExist) {
		return SetConfig(ClientConfig{})
	}
	return err
}
