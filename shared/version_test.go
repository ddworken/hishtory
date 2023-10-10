package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseVersionString(t *testing.T) {
	p, err := ParseVersionString("v0.200")
	require.NoError(t, err)
	require.Equal(t, ParsedVersion{MajorVersion: 0, MinorVersion: 200}, p)
	p, err = ParseVersionString("v1.200")
	require.NoError(t, err)
	require.Equal(t, ParsedVersion{MajorVersion: 1, MinorVersion: 200}, p)
	p, err = ParseVersionString("v1.0")
	require.NoError(t, err)
	require.Equal(t, ParsedVersion{MajorVersion: 1, MinorVersion: 0}, p)
	p, err = ParseVersionString("v0.216")
	require.NoError(t, err)
	require.Equal(t, ParsedVersion{MajorVersion: 0, MinorVersion: 216}, p)
	p, err = ParseVersionString("v123.456")
	require.NoError(t, err)
	require.Equal(t, ParsedVersion{MajorVersion: 123, MinorVersion: 456}, p)
}

func TestVersionLessThan(t *testing.T) {
	require.False(t, ParsedVersion{0, 200}.LessThan(ParsedVersion{0, 200}))
	require.False(t, ParsedVersion{1, 200}.LessThan(ParsedVersion{1, 200}))
	require.False(t, ParsedVersion{0, 201}.LessThan(ParsedVersion{0, 200}))
	require.False(t, ParsedVersion{1, 0}.LessThan(ParsedVersion{0, 200}))
	require.True(t, ParsedVersion{0, 199}.LessThan(ParsedVersion{0, 200}))
	require.True(t, ParsedVersion{0, 200}.LessThan(ParsedVersion{0, 205}))
	require.True(t, ParsedVersion{1, 200}.LessThan(ParsedVersion{1, 205}))
	require.True(t, ParsedVersion{0, 200}.LessThan(ParsedVersion{1, 1}))
}

func TestVersionGreaterThan(t *testing.T) {
	require.False(t, ParsedVersion{0, 200}.GreaterThan(ParsedVersion{0, 200}))
	require.False(t, ParsedVersion{1, 200}.GreaterThan(ParsedVersion{1, 200}))
	require.True(t, ParsedVersion{0, 201}.GreaterThan(ParsedVersion{0, 200}))
	require.True(t, ParsedVersion{1, 0}.GreaterThan(ParsedVersion{0, 200}))
	require.True(t, ParsedVersion{1, 1}.GreaterThan(ParsedVersion{1, 0}))
	require.False(t, ParsedVersion{0, 199}.GreaterThan(ParsedVersion{0, 200}))
	require.False(t, ParsedVersion{0, 200}.GreaterThan(ParsedVersion{0, 205}))
	require.False(t, ParsedVersion{1, 200}.GreaterThan(ParsedVersion{1, 205}))
	require.False(t, ParsedVersion{0, 200}.GreaterThan(ParsedVersion{1, 1}))
}
