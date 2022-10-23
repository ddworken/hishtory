package hctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sync"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/shared"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	// Needed to use sqlite without CGO
	"github.com/glebarez/sqlite"
)

var (
	hishtoryLogger *log.Logger
	getLoggerOnce  sync.Once
)

// TODO: Can we auto-rotate the log file?

func GetLogger() *log.Logger {
	getLoggerOnce.Do(func() {
		homedir, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Errorf("failed to get user's home directory: %v", err))
		}
		err = MakeHishtoryDir()
		if err != nil {
			panic(err)
		}
		f, err := os.OpenFile(path.Join(homedir, shared.HISHTORY_PATH, "hishtory.log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o660)
		if err != nil {
			panic(fmt.Errorf("failed to open hishtory.log: %v", err))
		}
		// Purposefully not closing the file. Yes, this is a dangling file handle. But hishtory is short lived so this is okay.
		hishtoryLogger = log.New(f, "\n", log.LstdFlags|log.Lshortfile)
	})
	return hishtoryLogger
}

func MakeHishtoryDir() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user's home directory: %v", err)
	}
	err = os.MkdirAll(path.Join(homedir, shared.HISHTORY_PATH), 0o744)
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
	hishtoryLogger := GetLogger()
	newLogger := logger.New(
		hishtoryLogger,
		logger.Config{
			SlowThreshold:             100 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		},
	)
	db, err := gorm.Open(sqlite.Open(path.Join(homedir, shared.HISHTORY_PATH, shared.DB_PATH)), &gorm.Config{SkipDefaultTransaction: true, Logger: newLogger})
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
}

func GetConfig() (ClientConfig, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to retrieve homedir: %v", err)
	}
	data, err := os.ReadFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	if err != nil {
		files, err := ioutil.ReadDir(path.Join(homedir, shared.HISHTORY_PATH))
		if err != nil {
			return ClientConfig{}, fmt.Errorf("failed to read config file (and failed to list too): %v", err)
		}
		filenames := ""
		for _, file := range files {
			filenames += file.Name()
			filenames += ", "
		}
		return ClientConfig{}, fmt.Errorf("failed to read config file (files in ~/.hishtory/: %s): %v", filenames, err)
	}
	var config ClientConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return ClientConfig{}, fmt.Errorf("failed to parse config file: %v", err)
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
	err = os.WriteFile(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH+".tmp"), serializedConfig, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}
	err = os.Rename(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH+".tmp"), path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	if err != nil {
		return fmt.Errorf("failed to replace config file with the rewritten version: %v", err)
	}
	return nil
}

func InitConfig() error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_, err = os.Stat(path.Join(homedir, shared.HISHTORY_PATH, shared.CONFIG_PATH))
	if errors.Is(err, os.ErrNotExist) {
		return SetConfig(ClientConfig{})
	}
	return err
}
