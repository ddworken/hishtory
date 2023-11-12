package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAiSuggestion(t *testing.T) {
	suggestions, err := ai.GetAiSuggestions("list files by size")
	require.NoError(t, err)
	for _, suggestion := range suggestions {
		if strings.Contains(suggestion, "ls") {
			return
		}
	}
	t.Fatalf("none of the AI suggestions %#v contain 'ls' which is suspicious", suggestions)
}
