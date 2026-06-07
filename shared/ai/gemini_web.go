package ai

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"

	"github.com/google/uuid"
)

const (
	DefaultGeminiBL    = "boq_assistant-bard-web-server_20260525.09_p0"
	DefaultGeminiModel = shared.DefaultGeminiModel

	geminiRetryAttempts     = 3
	geminiRetryDelaySec     = 2
	geminiRequestTimeoutSec = 180
)

// testOnlyGeminiStreamGenerateURL overrides the StreamGenerate URL in tests.
var testOnlyGeminiStreamGenerateURL string

type geminiModelConfig struct {
	mode  int
	think int
	extra map[int]any
}

var geminiModels = map[string]geminiModelConfig{
	"gemini-3.5-flash":               {mode: 1, think: 4},
	"gemini-3.5-flash-thinking":      {mode: 2, think: 0},
	"gemini-3.1-pro":                 {mode: 3, think: 4},
	"gemini-3.1-pro-enhanced":        {mode: 3, think: 4, extra: map[int]any{31: 2, 80: 3}},
	"gemini-auto":                    {mode: 4, think: 4},
	"gemini-3.5-flash-thinking-lite": {mode: 5, think: 0},
	"gemini-flash-lite":              {mode: 6, think: 4},
}

var (
	geminiCodeBlockRe   = regexp.MustCompile("(?s)```(?:python|javascript|text)\\?code_(?:reference|stdout)&code_event_index=\\d+\\n.*?```\\n?")
	geminiCardContentRe = regexp.MustCompile(`^http://googleusercontent\.com/card_content/\d+\n?$`)
)

func resolveGeminiModel(modelName string) (name string, modeID, thinkMode int, extra map[int]any, err error) {
	thinkOverride := -1
	if idx := strings.LastIndex(modelName, "@think="); idx >= 0 {
		thinkStr := modelName[idx+len("@think="):]
		modelName = modelName[:idx]
		n, parseErr := strconv.Atoi(thinkStr)
		if parseErr != nil {
			return "", 0, 0, nil, fmt.Errorf("invalid think level: %s", thinkStr)
		}
		thinkOverride = n
	}

	cfg, ok := geminiModels[modelName]
	if !ok {
		hctx.GetLogger().Infof("Unknown Gemini model %q, falling back to %s", modelName, DefaultGeminiModel)
		modelName = DefaultGeminiModel
		cfg = geminiModels[DefaultGeminiModel]
	}

	thinkMode = cfg.think
	if thinkOverride >= 0 {
		thinkMode = thinkOverride
	}
	return modelName, cfg.mode, thinkMode, cfg.extra, nil
}

func resolveGeminiModelFromEnv(overriddenModel string) (string, int, int, map[int]any, error) {
	model := DefaultGeminiModel
	if envModel := getEnvWithFallbacks("GEMINI_MODEL", "AI_API_MODEL"); envModel != "" {
		model = envModel
	}
	if overriddenModel != "" {
		model = overriddenModel
	}
	return resolveGeminiModel(model)
}

func geminiPromptFromMessages(msgs []openAiMessage) string {
	if len(msgs) < 2 {
		return ""
	}
	return "[System instruction]: " + msgs[0].Content + "\n\n" + msgs[1].Content
}

func geminiBL() string {
	if bl := strings.TrimSpace(os.Getenv("GEMINI_BL")); bl != "" {
		return bl
	}
	return DefaultGeminiBL
}

func geminiAuthUser() string {
	return strings.TrimSpace(os.Getenv("GEMINI_AUTH_USER"))
}

func geminiAccountPrefix() string {
	authUser := geminiAuthUser()
	if authUser == "" {
		return ""
	}
	return "/u/" + authUser
}

func loadGeminiCookie() (cookieStr, sapisid string) {
	if c := strings.TrimSpace(os.Getenv("GEMINI_COOKIE")); c != "" {
		cookieStr = c
		sapisid = parseSAPISIDFromCookie(c)
		return cookieStr, sapisid
	}
	cookieFile := strings.TrimSpace(os.Getenv("GEMINI_COOKIE_FILE"))
	if cookieFile == "" {
		return "", ""
	}
	content, err := os.ReadFile(cookieFile)
	if err != nil {
		hctx.GetLogger().Warnf("failed to read GEMINI_COOKIE_FILE: %v", err)
		return "", ""
	}
	text := strings.TrimSpace(string(content))
	if strings.HasPrefix(text, "{") {
		var data struct {
			Cookie  string `json:"cookie"`
			SAPISID string `json:"sapisid"`
		}
		if err := json.Unmarshal([]byte(text), &data); err == nil {
			return data.Cookie, data.SAPISID
		}
	}
	return text, parseSAPISIDFromCookie(text)
}

func parseSAPISIDFromCookie(cookieStr string) string {
	for _, part := range strings.Split(cookieStr, "; ") {
		if k, v, ok := strings.Cut(part, "="); ok && k == "SAPISID" {
			return v
		}
	}
	return ""
}

func makeSAPISIDHash(sapisid string) string {
	ts := time.Now().Unix()
	h := sha1.Sum([]byte(fmt.Sprintf("%d %s https://gemini.google.com", ts, sapisid)))
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, h)
}

func buildGeminiHeaders() http.Header {
	accountPrefix := geminiAccountPrefix()
	h := http.Header{}
	h.Set("Content-Type", "application/x-www-form-urlencoded")
	h.Set("Origin", "https://gemini.google.com")
	h.Set("Referer", "https://gemini.google.com"+accountPrefix+"/app")
	h.Set("X-Same-Domain", "1")
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if accountPrefix != "" {
		h.Set("X-Goog-AuthUser", geminiAuthUser())
	}
	cookieStr, sapisid := loadGeminiCookie()
	if cookieStr != "" {
		h.Set("Cookie", cookieStr)
	}
	if sapisid != "" {
		h.Set("Authorization", makeSAPISIDHash(sapisid))
	}
	return h
}

func buildGeminiPayload(prompt string, modelID, thinkMode int, extra map[int]any) (string, error) {
	inner := make([]any, 102)
	inner[0] = []any{prompt, 0, nil, nil, nil, nil, 0}
	inner[1] = []any{"en"}
	inner[2] = []any{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[6] = []any{0}
	inner[7] = 1
	inner[10] = 1
	inner[11] = 0
	inner[17] = [][]int{{thinkMode}}
	inner[18] = 0
	inner[27] = 1
	inner[30] = []int{4}
	inner[41] = []int{2}
	inner[53] = 0
	inner[59] = uuid.NewString()
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = modelID
	for k, v := range extra {
		if k >= 0 && k < len(inner) {
			inner[k] = v
		}
	}

	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Gemini inner payload: %w", err)
	}
	outer := []any{nil, string(innerJSON)}
	outerJSON, err := json.Marshal(outer)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Gemini outer payload: %w", err)
	}

	params := url.Values{}
	params.Set("f.req", string(outerJSON))
	if xsrf := strings.TrimSpace(os.Getenv("GEMINI_XSRF_TOKEN")); xsrf != "" {
		params.Set("at", xsrf)
	}
	return params.Encode(), nil
}

func geminiStreamGenerateURL() string {
	if testOnlyGeminiStreamGenerateURL != "" {
		return testOnlyGeminiStreamGenerateURL
	}
	reqid := time.Now().Unix() % 1000000
	accountPrefix := geminiAccountPrefix()
	return fmt.Sprintf(
		"https://gemini.google.com%s/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=%s&hl=en&_reqid=%d&rt=c",
		accountPrefix,
		url.QueryEscape(geminiBL()),
		reqid,
	)
}

func cleanGeminiText(text string) string {
	text = geminiCodeBlockRe.ReplaceAllString(text, "")
	text = geminiCardContentRe.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func extractTextsFromGeminiLine(line string) []string {
	if !strings.Contains(line, `"wrb.fr"`) {
		return nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(line), &arr); err != nil || len(arr) == 0 {
		return nil
	}
	row, ok := arr[0].([]any)
	if !ok || len(row) < 3 {
		return nil
	}
	innerStr, ok := row[2].(string)
	if !ok || innerStr == "" {
		return nil
	}
	var inner []any
	if err := json.Unmarshal([]byte(innerStr), &inner); err != nil || len(inner) <= 4 {
		return nil
	}
	parts, ok := inner[4].([]any)
	if !ok {
		return nil
	}
	var texts []string
	for _, part := range parts {
		partList, ok := part.([]any)
		if !ok || len(partList) < 2 {
			continue
		}
		textList, ok := partList[1].([]any)
		if !ok {
			continue
		}
		for _, t := range textList {
			if s, ok := t.(string); ok && s != "" {
				texts = append(texts, s)
			}
		}
	}
	return texts
}

func extractGeminiResponseText(raw string) string {
	lastText := ""
	for _, line := range strings.Split(raw, "\n") {
		for _, t := range extractTextsFromGeminiLine(line) {
			if len(t) > len(lastText) {
				lastText = t
			}
		}
	}
	return cleanGeminiText(lastText)
}

func makeSingleGeminiCall(prompt string, modelID, thinkMode int, extra map[int]any) (string, error) {
	body, err := buildGeminiPayload(prompt, modelID, thinkMode, extra)
	if err != nil {
		return "", err
	}
	targetURL := geminiStreamGenerateURL()

	var lastErr error
	for attempt := 0; attempt < geminiRetryAttempts; attempt++ {
		req, err := http.NewRequest(http.MethodPost, targetURL, strings.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("failed to create Gemini request: %w", err)
		}
		req.Header = buildGeminiHeaders()

		client := lib.GetHttpClient()
		client.Timeout = geminiRequestTimeoutSec * time.Second
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to query Gemini web API: %w", err)
			if attempt < geminiRetryAttempts-1 {
				hctx.GetLogger().Infof("Gemini retry %d/%d: %v", attempt+1, geminiRetryAttempts, err)
				time.Sleep(geminiRetryDelaySec * time.Second)
				continue
			}
			return "", lastErr
		}

		bodyText, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read Gemini response: %w", readErr)
			if attempt < geminiRetryAttempts-1 {
				time.Sleep(geminiRetryDelaySec * time.Second)
				continue
			}
			return "", lastErr
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("gemini web API returned status %d, body=%#v", resp.StatusCode, string(bodyText))
			if attempt < geminiRetryAttempts-1 {
				time.Sleep(geminiRetryDelaySec * time.Second)
				continue
			}
			return "", lastErr
		}

		text := extractGeminiResponseText(string(bodyText))
		if text == "" {
			lastErr = fmt.Errorf("gemini web API returned empty response, body=%#v", string(bodyText))
			if attempt < geminiRetryAttempts-1 {
				time.Sleep(geminiRetryDelaySec * time.Second)
				continue
			}
			return "", lastErr
		}
		return NormalizeAISuggestion(text), nil
	}
	return "", lastErr
}

func getMultipleGeminiCompletions(query, shellName, osName, overriddenModel string, numberCompletions int) ([]string, OpenAiUsage, error) {
	hctx.GetLogger().Infof("Making %d parallel Gemini web API calls for multiple completions", numberCompletions)
	msgs := buildShellAssistantMessages(query, shellName, osName)
	prompt := geminiPromptFromMessages(msgs)
	_, modelID, thinkMode, extra, err := resolveGeminiModelFromEnv(overriddenModel)
	if err != nil {
		return nil, OpenAiUsage{}, err
	}

	indices := make([]int, numberCompletions)
	for i := 0; i < numberCompletions; i++ {
		indices[i] = i
	}

	results, err := shared.ParallelMap(indices, func(index int) (apiCallResult, error) {
		text, err := makeSingleGeminiCall(prompt, modelID, thinkMode, extra)
		if err != nil {
			return apiCallResult{}, fmt.Errorf("failed on completion %d/%d: %w", index+1, numberCompletions, err)
		}
		return apiCallResult{results: []string{text}, usage: OpenAiUsage{}}, nil
	})
	if err != nil {
		return nil, OpenAiUsage{}, err
	}

	allResults := make([]string, 0, numberCompletions)
	for _, result := range results {
		for _, r := range result.results {
			norm := NormalizeAISuggestion(r)
			if norm == "" {
				continue
			}
			if !containsString(allResults, norm) {
				allResults = append(allResults, norm)
			}
		}
	}
	hctx.GetLogger().Infof("For Gemini query=%#v with %d parallel completions ==> %#v", query, numberCompletions, allResults)
	return allResults, OpenAiUsage{}, nil
}

func containsString(ss []string, s string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}

// GetAiSuggestionsViaGeminiWeb calls Gemini's web StreamGenerate endpoint (no API key required).
func GetAiSuggestionsViaGeminiWeb(query, shellName, osName, overriddenModel string, numberCompletions int) ([]string, OpenAiUsage, error) {
	if results := TestOnlyOverrideAiSuggestions[query]; len(results) > 0 {
		return NormalizeSuggestionSlice(results), OpenAiUsage{}, nil
	}

	hctx.GetLogger().Infof("Running AI query via Gemini web for %#v", query)

	if envNumberCompletions := getEnvWithFallbacks("AI_API_NUMBER_COMPLETIONS", "OPENAI_API_NUMBER_COMPLETIONS"); envNumberCompletions != "" {
		n, err := strconv.Atoi(envNumberCompletions)
		if err == nil {
			numberCompletions = n
		}
	}

	if numberCompletions > 1 {
		return getMultipleGeminiCompletions(query, shellName, osName, overriddenModel, numberCompletions)
	}

	msgs := buildShellAssistantMessages(query, shellName, osName)
	prompt := geminiPromptFromMessages(msgs)
	_, modelID, thinkMode, extra, err := resolveGeminiModelFromEnv(overriddenModel)
	if err != nil {
		return nil, OpenAiUsage{}, err
	}

	text, err := makeSingleGeminiCall(prompt, modelID, thinkMode, extra)
	if err != nil {
		return nil, OpenAiUsage{}, err
	}
	hctx.GetLogger().Infof("For Gemini query=%#v ==> %#v", query, text)
	if norm := NormalizeAISuggestion(text); norm != "" {
		return []string{norm}, OpenAiUsage{}, nil
	}
	return nil, OpenAiUsage{}, fmt.Errorf("gemini returned only whitespace or punctuation after normalization")
}
