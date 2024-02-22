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
