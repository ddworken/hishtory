package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ddworken/hishtory/shared"

	"github.com/stretchr/testify/require"
)

// A basic sanity test that our integration with the OpenAI API is correct and is returning reasonable results (at least for a very basic query)
func TestLiveOpenAiApi(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" || !strings.HasPrefix(apiKey, "sk-") {
		if os.Getenv("GITHUB_ACTIONS") != "" {
			t.Fatal("OPENAI_API_KEY must be set in GitHub Actions")
		}
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
		if os.Getenv("GITHUB_ACTIONS") != "" {
			t.Fatal("ANTHROPIC_API_KEY must be set in GitHub Actions")
		}
		t.Skip("Skipping test since ANTHROPIC_API_KEY is not set or invalid")
	}
	// Test multiple completions - Claude doesn't support n>1 natively, so we make multiple API calls
	results, usage, err := GetAiSuggestionsViaOpenAiApi("https://api.anthropic.com/v1/chat/completions", "list files in the current directory", "bash", "Linux", "claude-haiku-4-5-20251001", 3)
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

func TestNormalizeAISuggestion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"plain", "ls -la", "ls -la"},
		{"spaces", "  ls -la  ", "ls -la"},
		{"backticks", "`ls -la`", "ls -la"},
		{"fence_only", "```ls -la```", "ls -la"},
		{"comma", "`ls -la`,", "ls -la"},
		{"unbalanced_trailing_single", "ls -la'", "ls -la'"},
		{"unbalanced_trailing_double", `ls -la"`, `ls -la"`},
		{"balanced_single", "'ls -la'", "ls -la"},
		{"balanced_double", `"ls -la"`, "ls -la"},
		{"nested_double_in_command", `echo "hi"`, `echo "hi"`},
		{"commas", "ls -la,,", "ls -la"},
		{"semicolon", "ls -la;\n", "ls -la"},
		{"fence_multiline", "\n```\nfind .\n```\n", "find ."},
		{"bash_lang", "```bash\nls -la\n```", "ls -la"},
		{"sh_lang", "```sh\ndir\n```", "dir"},
		{"shell_lang", "```shell\npwd\n```", "pwd"},
		{"bash_crlf", "```bash\r\nls\r\n```", "ls"},
		{"zsh_lang", "```zsh\necho ok\n```", "echo ok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeAISuggestion(tc.in)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNormalizeSuggestionSlice_dedupesAfterNormalize(t *testing.T) {
	t.Parallel()
	got := NormalizeSuggestionSlice([]string{"`ls`", "ls", "ls,"})
	require.Equal(t, []string{"ls"}, got)
}

func TestOllamaGenerateNonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var body ollamaGenerateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, shared.DefaultOllamaModel, body.Model)
		require.False(t, body.Stream)
		require.Contains(t, body.Prompt, "list files")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"` + shared.DefaultOllamaModel + `","response":"ls -la","done":true}`))
	}))
	defer srv.Close()

	results, usage, err := GetAiSuggestionsViaOllama(srv.URL, "list files in the current directory", "bash", "Linux", "", 1)
	require.NoError(t, err)
	require.Equal(t, []string{"ls -la"}, results)
	require.Equal(t, OpenAiUsage{}, usage)
}

func TestOllamaGenerateParallelCompletions(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":"ls","done":true}`))
	}))
	defer srv.Close()

	results, _, err := GetAiSuggestionsViaOllama(srv.URL, "list files", "bash", "Linux", "", 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	require.Equal(t, 3, n)
}

// TestLiveOllamaGenerate calls a real local Ollama daemon. Enable with:
//
//	OLLAMA_LIVE_TEST=1 go test ./shared/ai/... -run TestLiveOllamaGenerate -count=1
//
// Optional: OLLAMA_TEST_ENDPOINT (default http://localhost:11434/api/generate), OLLAMA_LIVE_MODEL (model name override).
// Ensure the model is pulled first, e.g. ollama pull eslider/bonsai-1.7b
func TestLiveOllamaGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Ollama test in -short mode")
	}
	if os.Getenv("OLLAMA_LIVE_TEST") != "1" {
		t.Skip(`Set OLLAMA_LIVE_TEST=1 to run against local Ollama (see test comment)`)
	}

	endpoint := strings.TrimSpace(os.Getenv("OLLAMA_TEST_ENDPOINT"))
	if endpoint == "" {
		endpoint = DefaultOllamaEndpoint
	}

	u, err := url.Parse(endpoint)
	require.NoError(t, err, "OLLAMA_TEST_ENDPOINT / default must be a valid URL")
	probeURL := u.Scheme + "://" + u.Host + "/api/tags"
	probeResp, probeErr := (&http.Client{Timeout: 2 * time.Second}).Get(probeURL)
	if probeErr != nil {
		t.Skipf("Ollama does not appear reachable at %s://%s: %v", u.Scheme, u.Host, probeErr)
	}
	_ = probeResp.Body.Close()
	if probeResp.StatusCode != http.StatusOK {
		t.Skipf("Ollama GET %s returned %d", probeURL, probeResp.StatusCode)
	}

	modelOverride := strings.TrimSpace(os.Getenv("OLLAMA_LIVE_MODEL"))
	results, _, err := GetAiSuggestionsViaOllama(endpoint, "list files in the current directory", "bash", "Linux", modelOverride, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1, "expected at least one suggestion")
	combined := strings.ToLower(strings.TrimSpace(results[0]))
	containsListingHint := strings.Contains(combined, "ls") ||
		strings.Contains(combined, "dir") ||
		strings.Contains(combined, "find")
	require.Truef(t, containsListingHint, "expected a directory-listing style reply, got %#v", results[0])
}
