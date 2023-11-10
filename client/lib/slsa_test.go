package lib

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckForDowngrade(t *testing.T) {
	require.NoError(t, checkForDowngrade("v0.100", "v0.100"))
	require.NoError(t, checkForDowngrade("v0.100", "v0.101"))
	require.NoError(t, checkForDowngrade("v0.100", "v0.200"))
	require.NoError(t, checkForDowngrade("v0.100", "v1.0"))
	require.NoError(t, checkForDowngrade("v0.1", "v1.0"))
	require.NoError(t, checkForDowngrade("v1.0", "v1.1"))
	require.Equal(t, "failed to update because the new version (\"v0.99\") is a downgrade compared to the current version (\"v0.100\")",
		checkForDowngrade("v0.100", "v0.99").Error())
	require.Equal(t, "failed to update because the new version (\"v0.10\") is a downgrade compared to the current version (\"v0.100\")",
		checkForDowngrade("v0.100", "v0.10").Error())
	require.Equal(t, "failed to update because the new version (\"v0.100\") is a downgrade compared to the current version (\"v1.0\")",
		checkForDowngrade("v1.0", "v0.100").Error())
}
