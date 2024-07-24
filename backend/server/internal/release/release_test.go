package release

import (
	"strings"
	"testing"

	"github.com/ddworken/hishtory/shared/testutils"

	"github.com/stretchr/testify/require"
)

func TestUpdateReleaseVersion(t *testing.T) {
	if !testutils.IsOnline() {
		t.Skip("skipping because we're currently offline")
	}

	// Check that ReleaseVersion hasn't been set yet
	if Version != "UNKNOWN" {
		t.Fatalf("initial ReleaseVersion isn't as expected: %#v", Version)
	}

	// Update it
	err := UpdateReleaseVersion()
	if err != nil {
		t.Fatalf("updateReleaseVersion failed: %v", err)
	}

	// If ReleaseVersion is still unknown, skip because we're getting rate limited
	if Version == "UNKNOWN" {
		t.Skip()
	}
	// Otherwise, check that the new value looks reasonable
	if !strings.HasPrefix(Version, "v0.") {
		t.Fatalf("ReleaseVersion wasn't updated to contain a version: %#v", Version)
	}
}

func TestDecrement(t *testing.T) {
	pv, err := decrementVersion("v0.100")
	require.NoError(t, err)
	require.Equal(t, "v0.99", pv)
}
