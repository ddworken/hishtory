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
	results, _, err := GetAiSuggestionsViaOpenAiApi("list files in the current directory", "bash", "Linux", 3)
	require.NoError(t, err)
	resultsContainsLs := false
	for _, result := range results {
		if strings.Contains(result, "ls") {
			resultsContainsLs = true
		}
	}
	require.Truef(t, resultsContainsLs, "expected results=%#v to contain ls", results)
}
