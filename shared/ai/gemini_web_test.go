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

	"github.com/stretchr/testify/require"
)

func buildMockGeminiResponse(text string) string {
	inner := []any{nil, nil, nil, nil, []any{[]any{nil, []any{text}}}}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		panic(err)
	}
	outer := []any{[]any{"wrb.fr", nil, string(innerJSON)}}
	line, err := json.Marshal(outer)
	if err != nil {
		panic(err)
	}
	return string(line) + "\n"
}

func TestExtractGeminiResponseText(t *testing.T) {
	t.Parallel()
	raw := buildMockGeminiResponse("ls -la")
	require.Equal(t, "ls -la", extractGeminiResponseText(raw))

	raw = buildMockGeminiResponse("find . -size +1M")
	require.Equal(t, "find . -size +1M", extractGeminiResponseText(raw))
	require.Equal(t, "", extractGeminiResponseText("not a gemini response"))
}

func TestBuildGeminiPayload(t *testing.T) {
	t.Parallel()
	body, err := buildGeminiPayload("hello", 1, 4, nil)
	require.NoError(t, err)
	values, err := url.ParseQuery(body)
	require.NoError(t, err)
	require.Contains(t, values.Get("f.req"), "hello")
}

func TestResolveGeminiModel(t *testing.T) {
	t.Parallel()
	name, mode, think, extra, err := resolveGeminiModel("gemini-3.5-flash")
	require.NoError(t, err)
	require.Equal(t, "gemini-3.5-flash", name)
	require.Equal(t, 1, mode)
	require.Equal(t, 4, think)
	require.Nil(t, extra)

	_, mode, think, _, err = resolveGeminiModel("gemini-3.5-flash-thinking@think=2")
	require.NoError(t, err)
	require.Equal(t, 2, mode)
	require.Equal(t, 2, think)

	_, _, _, _, err = resolveGeminiModel("bad@think=foo")
	require.Error(t, err)
}

func TestGetAiSuggestionsViaGeminiWebMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Contains(t, r.Form.Get("f.req"), "list files")
		_, _ = w.Write([]byte(buildMockGeminiResponse("ls -la")))
	}))
	defer srv.Close()

	oldURL := testOnlyGeminiStreamGenerateURL
	testOnlyGeminiStreamGenerateURL = srv.URL
	defer func() { testOnlyGeminiStreamGenerateURL = oldURL }()

	results, usage, err := GetAiSuggestionsViaGeminiWeb("list files in the current directory", "bash", "Linux", "", 1)
	require.NoError(t, err)
	require.Equal(t, []string{"ls -la"}, results)
	require.Equal(t, OpenAiUsage{}, usage)
}

func TestGetAiSuggestionsViaGeminiWebParallel(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = w.Write([]byte(buildMockGeminiResponse("ls")))
	}))
	defer srv.Close()

	oldURL := testOnlyGeminiStreamGenerateURL
	testOnlyGeminiStreamGenerateURL = srv.URL
	defer func() { testOnlyGeminiStreamGenerateURL = oldURL }()

	results, _, err := GetAiSuggestionsViaGeminiWeb("list files", "bash", "Linux", "", 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	require.Equal(t, 3, n)
}

func TestLiveGeminiWeb(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Gemini test in -short mode")
	}
	if os.Getenv("GEMINI_LIVE_TEST") != "1" {
		t.Skip(`Set GEMINI_LIVE_TEST=1 to run against Gemini web (see shared/ai/ai_test.go)`)
	}

	probeClient := &http.Client{Timeout: 2 * time.Second}
	probeResp, probeErr := probeClient.Head("https://gemini.google.com")
	if probeErr != nil {
		t.Skipf("gemini.google.com does not appear reachable: %v", probeErr)
	}
	_ = probeResp.Body.Close()

	results, _, err := GetAiSuggestionsViaGeminiWeb("list files in the current directory", "bash", "Linux", "", 1)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	found := false
	for _, result := range results {
		if strings.Contains(result, "ls") {
			found = true
		}
	}
	require.Truef(t, found, "expected results=%#v to contain ls", results)
}
