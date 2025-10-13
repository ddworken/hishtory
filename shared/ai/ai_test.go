package ai

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A basic sanity test that our integration with the OpenAI API is correct and is returning reasonable results (at least for a very basic query)
func TestLiveOpenAiApi(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping test since OPENAI_API_KEY is not set")
	}
	results, _, err := GetAiSuggestionsViaOpenAiApi("https://api.openai.com/v1/chat/completions", "list files in the current directory", "bash", "Linux", "", 3)
	require.NoError(t, err)
	resultsContainsLs := false
	for _, result := range results {
		if strings.Contains(result, "ls") {
			resultsContainsLs = true
		}
	}
	require.Truef(t, resultsContainsLs, "expected results=%#v to contain ls", results)
}

// A basic sanity test that our integration with the Claude API is correct and is returning reasonable results (at least for a very basic query)
func TestLiveClaudeApi(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping test since ANTHROPIC_API_KEY is not set")
	}
	// Claude's OpenAI-compatible endpoint only supports n=1
	// Explicitly specify a Claude model
	results, _, err := GetAiSuggestionsViaOpenAiApi("https://api.anthropic.com/v1/chat/completions", "list files in the current directory", "bash", "Linux", "claude-sonnet-4-5", 1)
	require.NoError(t, err)
	resultsContainsLs := false
	for _, result := range results {
		if strings.Contains(result, "ls") {
			resultsContainsLs = true
		}
	}
	require.Truef(t, resultsContainsLs, "expected results=%#v to contain ls", results)
}
