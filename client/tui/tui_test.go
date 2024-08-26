package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCalculateWordBoundaries(t *testing.T) {
	require.Equal(t, []int{0, 3}, calculateWordBoundaries("foo"))
	require.Equal(t, []int{0, 3, 7}, calculateWordBoundaries("foo bar"))
	require.Equal(t, []int{0, 3, 7}, calculateWordBoundaries("foo-bar"))
	require.Equal(t, []int{0, 3, 7, 11}, calculateWordBoundaries("foo-bar baz"))
	require.Equal(t, []int{0, 3, 10, 16}, calculateWordBoundaries("foo-- -bar - baz"))
	require.Equal(t, []int{0, 3}, calculateWordBoundaries("foo    "))
}

func TestSanitizeEscapeCodes(t *testing.T) {
	require.Equal(t, "foo", sanitizeEscapeCodes("foo"))
	require.Equal(t, "foo\x1b[31mbar", sanitizeEscapeCodes("foo\x1b[31mbar"))
	require.Equal(t, "", sanitizeEscapeCodes("11;rgb:1c1c/1c1c/1c1c"))
	require.Equal(t, "foo  bar", sanitizeEscapeCodes("foo 11;rgb:1c1c/1c1c/1c1c bar"))
}
