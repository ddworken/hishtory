package cmd

import (
	"os"
	"path"
	"testing"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared/testutils"

	"github.com/stretchr/testify/require"
)

func TestSetup(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()

	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	if _, err := os.Stat(path.Join(data.GetHishtoryPath(), data.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	require.NoError(t, setup("", false))
	if _, err := os.Stat(path.Join(data.GetHishtoryPath(), data.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(data.GetHishtoryPath(), data.CONFIG_PATH))
	require.NoError(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
	config := hctx.GetConf(hctx.MakeContext())
	if config.IsOffline != false {
		t.Fatalf("hishtory config should have been offline")
	}
}

func TestSetupOffline(t *testing.T) {
	defer testutils.BackupAndRestore(t)()
	defer testutils.RunTestServer()()

	homedir, err := os.UserHomeDir()
	require.NoError(t, err)
	if _, err := os.Stat(path.Join(data.GetHishtoryPath(), data.CONFIG_PATH)); err == nil {
		t.Fatalf("hishtory secret file already exists!")
	}
	require.NoError(t, setup("", true))
	if _, err := os.Stat(path.Join(data.GetHishtoryPath(), data.CONFIG_PATH)); err != nil {
		t.Fatalf("hishtory secret file does not exist after Setup()!")
	}
	data, err := os.ReadFile(path.Join(data.GetHishtoryPath(), data.CONFIG_PATH))
	require.NoError(t, err)
	if len(data) < 10 {
		t.Fatalf("hishtory secret has unexpected length: %d", len(data))
	}
	config := hctx.GetConf(hctx.MakeContext())
	if config.IsOffline != true {
		t.Fatalf("hishtory config should have been offline, actual=%#v", string(data))
	}
}
