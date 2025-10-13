package ai

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A basic sanity test that our integration with the OpenAI API is correct and is returning reasonable results (at least for a very basic query)
func TestLiveOpenAiApi(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" || !strings.HasPrefix(apiKey, "sk-") {
		t.Skip("Skipping test since OPENAI_API_KEY is not set or invalid")
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
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || !strings.HasPrefix(apiKey, "sk-ant-") {
		t.Skip("Skipping test since ANTHROPIC_API_KEY is not set or invalid")
	}
	// Test multiple completions - Claude doesn't support n>1 natively, so we make multiple API calls
	results, usage, err := GetAiSuggestionsViaOpenAiApi("https://api.anthropic.com/v1/chat/completions", "list files in the current directory", "bash", "Linux", "claude-sonnet-4-5", 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1, "expected at least 1 result")
	resultsContainsLs := false
	for _, result := range results {
		if strings.Contains(result, "ls") {
			resultsContainsLs = true
		}
	}
	require.Truef(t, resultsContainsLs, "expected results=%#v to contain ls", results)
	// Verify usage stats were aggregated (should have tokens from 3 API calls)
	require.Greater(t, usage.TotalTokens, 0, "expected non-zero token usage")
}
