package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"

	"golang.org/x/exp/slices"
)

const (
	DefaultOpenAiEndpoint = "https://api.openai.com/v1/chat/completions"
	DefaultClaudeEndpoint = "https://api.anthropic.com/v1/chat/completions"
	// DefaultOllamaEndpoint is the default URL for Ollama's generate API.
	DefaultOllamaEndpoint = shared.DefaultOllamaGenerateEndpoint
)

type AiProvider string

const (
	ProviderOpenAI    AiProvider = "openai"
	ProviderAnthropic AiProvider = "anthropic"
)

type openAiRequest struct {
	Model             string          `json:"model"`
	Messages          []openAiMessage `json:"messages"`
	NumberCompletions int             `json:"n"`
}

type openAiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAiResponse struct {
	Id      string         `json:"id"`
	Object  string         `json:"object"`
	Created int            `json:"created"`
	Model   string         `json:"model"`
	Usage   OpenAiUsage    `json:"usage"`
	Choices []openAiChoice `json:"choices"`
}

type openAiChoice struct {
	Index        int           `json:"index"`
	Message      openAiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type OpenAiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type TestOnlyOverrideAiSuggestionRequest struct {
	Query       string   `json:"query"`
	Suggestions []string `json:"suggestions"`
}

var TestOnlyOverrideAiSuggestions map[string][]string = make(map[string][]string)

// markdownFenceLangs is the set of common info-string tokens after an opening ``` fence.
var markdownFenceLangs = map[string]struct{}{
	"bash": {}, "sh": {}, "shell": {}, "zsh": {}, "fish": {}, "nu": {}, "xonsh": {}, "elvish": {},
	"pwsh": {}, "powershell": {}, "posh": {}, "cmd": {}, "bat": {}, "batch": {},
	"text": {}, "plaintext": {}, "console": {}, "terminal": {}, "unix": {}, "linux": {},
}

func isMarkdownFenceLang(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	_, ok := markdownFenceLangs[strings.ToLower(line)]
	return ok
}

// stripLeadingMarkdownFence removes ``` fences: closed ```...``` blocks (optionally with a
// language line like ```bash) or an opening ``` plus a language line when there is no closing fence yet.
func stripLeadingMarkdownFence(s string) string {
	s = strings.TrimFunc(s, unicode.IsSpace)
	for strings.HasPrefix(s, "```") {
		s = strings.TrimFunc(s, unicode.IsSpace)
		if strings.HasSuffix(s, "```") && len(s) >= 6 {
			inner := strings.TrimFunc(s[3:len(s)-3], unicode.IsSpace)
			first, rest, ok := splitFirstLine(inner)
			if ok && isMarkdownFenceLang(first) {
				s = strings.TrimLeft(rest, " \t\r\n")
				continue
			}
			s = inner
			continue
		}
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimLeft(s, " \t\r\n")
		first, rest, ok := splitFirstLine(s)
		if !ok {
			break
		}
		if isMarkdownFenceLang(first) {
			s = strings.TrimLeft(rest, " \t\r\n")
			continue
		}
		break
	}
	return strings.TrimFunc(s, unicode.IsSpace)
}

func splitFirstLine(s string) (first, rest string, ok bool) {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		first = s[:i]
		rest = s[i+1:]
		first = strings.TrimSuffix(first, "\r")
		return first, rest, true
	}
	if i := strings.IndexByte(s, '\r'); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return "", s, false
}

// stripBalancedOuterChar removes one layer of leading+trailing r only when both ends match.
func stripBalancedOuterChar(s string, r rune) string {
	s = strings.TrimFunc(s, unicode.IsSpace)
	first, fw := utf8.DecodeRuneInString(s)
	last, lw := utf8.DecodeLastRuneInString(s)
	if first == utf8.RuneError || last == utf8.RuneError || fw+lw >= len(s) {
		return s
	}
	if first == r && last == r {
		return strings.TrimFunc(s[fw:len(s)-lw], unicode.IsSpace)
	}
	return s
}

// NormalizeAISuggestion strips opening markdown fences (e.g. ```bash), then trims
// leading/trailing whitespace. Trailing `,` / `;` are removed; `"`, `'`, and “ ` “
// are only stripped when they wrap the whole string (same char at start and end).
func NormalizeAISuggestion(s string) string {
	s = stripLeadingMarkdownFence(s)
	s = strings.TrimFunc(s, unicode.IsSpace)
	for {
		before := s
		s = strings.TrimFunc(s, unicode.IsSpace)
		s2 := stripBalancedOuterChar(s, '`')
		if s2 != s {
			s = s2
			continue
		}
		s2 = stripBalancedOuterChar(s, '\'')
		if s2 != s {
			s = s2
			continue
		}
		s2 = stripBalancedOuterChar(s, '"')
		if s2 != s {
			s = s2
			continue
		}
		s = strings.TrimSuffix(s, ",")
		s = strings.TrimSuffix(s, ";")
		s = strings.TrimFunc(s, unicode.IsSpace)
		if s == before {
			break
		}
	}
	return strings.TrimFunc(s, unicode.IsSpace)
}

// NormalizeSuggestionSlice applies [NormalizeAISuggestion] to each entry, drops empties, and de-dupes.
func NormalizeSuggestionSlice(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		n := NormalizeAISuggestion(s)
		if n == "" {
			continue
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

// getEnvWithFallbacks returns the first non-empty environment variable from the list
func getEnvWithFallbacks(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

// GetAiProvider determines which AI provider to use based on endpoint and API key patterns
func GetAiProvider(apiEndpoint string) AiProvider {
	// Check the endpoint first - if it's a known endpoint, use that provider
	if apiEndpoint == DefaultClaudeEndpoint {
		return ProviderAnthropic
	}
	if apiEndpoint == DefaultOpenAiEndpoint {
		return ProviderOpenAI
	}

	// For unknown endpoints, auto-detect based on API key prefix
	// Check ANTHROPIC_API_KEY
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		if len(anthropicKey) >= 7 && anthropicKey[:7] == "sk-ant-" {
			return ProviderAnthropic
		}
	}

	// Check OPENAI_API_KEY
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		if len(openaiKey) >= 8 && openaiKey[:8] == "sk-proj-" {
			return ProviderOpenAI
		}
	}

	// Check generic AI_API_KEY
	if genericKey := getEnvWithFallbacks("AI_API_KEY"); genericKey != "" {
		if len(genericKey) >= 7 && genericKey[:7] == "sk-ant-" {
			return ProviderAnthropic
		}
		if len(genericKey) >= 8 && genericKey[:8] == "sk-proj-" {
			return ProviderOpenAI
		}
	}

	// Default to OpenAI for backwards compatibility
	return ProviderOpenAI
}

// getApiKey returns the appropriate API key based on the provider
func getApiKey(provider AiProvider) string {
	if provider == ProviderAnthropic {
		return getEnvWithFallbacks("ANTHROPIC_API_KEY", "AI_API_KEY")
	}
	return getEnvWithFallbacks("OPENAI_API_KEY", "AI_API_KEY")
}

// makeSingleApiCall makes a single API call with n=1 and returns the results
func makeSingleApiCall(apiEndpoint, query, shellName, osName, overriddenOpenAiModel string, provider AiProvider, apiKey string) ([]string, OpenAiUsage, error) {
	apiReqStr, err := json.Marshal(createOpenAiRequest(query, shellName, osName, overriddenOpenAiModel, 1))
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to serialize JSON for AI API: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, apiEndpoint, bytes.NewBuffer(apiReqStr))
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to create AI API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Set authentication headers based on provider
	if apiKey != "" {
		if provider == ProviderAnthropic {
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	resp, err := lib.GetHttpClient().Do(req)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to query AI API: %w", err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to read AI API response: %w", err)
	}
	if resp.StatusCode == 429 {
		return nil, OpenAiUsage{}, fmt.Errorf("received 429 error code from AI API (is your API key valid?)")
	}
	var apiResp openAiResponse
	err = json.Unmarshal(bodyText, &apiResp)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to parse AI API response=%#v: %w", string(bodyText), err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, OpenAiUsage{}, fmt.Errorf("AI API returned zero choices, parsed resp=%#v, resp body=%#v, resp.StatusCode=%d", apiResp, bodyText, resp.StatusCode)
	}
	ret := make([]string, 0)
	for _, item := range apiResp.Choices {
		norm := NormalizeAISuggestion(item.Message.Content)
		if norm == "" {
			continue
		}
		if !slices.Contains(ret, norm) {
			ret = append(ret, norm)
		}
	}
	return ret, apiResp.Usage, nil
}

// apiCallResult holds the result from a single API call
type apiCallResult struct {
	results []string
	usage   OpenAiUsage
}

// getMultipleClaudeCompletions makes multiple parallel API calls for Claude since it doesn't support n>1
func getMultipleClaudeCompletions(apiEndpoint, query, shellName, osName, overriddenOpenAiModel string, numberCompletions int, apiKey string) ([]string, OpenAiUsage, error) {
	hctx.GetLogger().Infof("Making %d parallel Claude API calls for multiple completions", numberCompletions)

	// Create array of indices to map over
	indices := make([]int, numberCompletions)
	for i := 0; i < numberCompletions; i++ {
		indices[i] = i
	}

	// Use ParallelMap to make concurrent API calls
	results, err := shared.ParallelMap(indices, func(index int) (apiCallResult, error) {
		results, usage, err := makeSingleApiCall(apiEndpoint, query, shellName, osName, overriddenOpenAiModel, ProviderAnthropic, apiKey)
		if err != nil {
			return apiCallResult{}, fmt.Errorf("failed on completion %d/%d: %w", index+1, numberCompletions, err)
		}
		return apiCallResult{results: results, usage: usage}, nil
	})
	if err != nil {
		return nil, OpenAiUsage{}, err
	}

	// Aggregate results and usage stats
	allResults := make([]string, 0, numberCompletions)
	totalUsage := OpenAiUsage{}

	for _, result := range results {
		// Add unique results
		for _, r := range result.results {
			if !slices.Contains(allResults, r) {
				allResults = append(allResults, r)
			}
		}

		// Aggregate usage stats
		totalUsage.PromptTokens += result.usage.PromptTokens
		totalUsage.CompletionTokens += result.usage.CompletionTokens
		totalUsage.TotalTokens += result.usage.TotalTokens
	}

	hctx.GetLogger().Infof("For Claude query=%#v with %d parallel completions ==> %#v (total tokens: %d)", query, numberCompletions, allResults, totalUsage.TotalTokens)
	return allResults, totalUsage, nil
}

func GetAiSuggestionsViaOpenAiApi(apiEndpoint, query, shellName, osName, overriddenOpenAiModel string, numberCompletions int) ([]string, OpenAiUsage, error) {
	if results := TestOnlyOverrideAiSuggestions[query]; len(results) > 0 {
		return NormalizeSuggestionSlice(results), OpenAiUsage{}, nil
	}

	provider := GetAiProvider(apiEndpoint)
	hctx.GetLogger().Infof("Running AI query via %s for %#v", provider, query)

	apiKey := getApiKey(provider)
	if apiKey == "" {
		if apiEndpoint == DefaultOpenAiEndpoint {
			return nil, OpenAiUsage{}, fmt.Errorf("OPENAI_API_KEY or AI_API_KEY environment variable is not set")
		}
		if apiEndpoint == DefaultClaudeEndpoint {
			return nil, OpenAiUsage{}, fmt.Errorf("ANTHROPIC_API_KEY or AI_API_KEY environment variable is not set")
		}
	}

	// Claude's OpenAI-compatible endpoint only supports n=1 per request
	// For multiple completions, we make multiple sequential API calls
	if provider == ProviderAnthropic && numberCompletions > 1 {
		return getMultipleClaudeCompletions(apiEndpoint, query, shellName, osName, overriddenOpenAiModel, numberCompletions, apiKey)
	}

	apiReqStr, err := json.Marshal(createOpenAiRequest(query, shellName, osName, overriddenOpenAiModel, numberCompletions))
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to serialize JSON for OpenAI API: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, apiEndpoint, bytes.NewBuffer(apiReqStr))
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to create AI API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Set authentication headers based on provider
	if apiKey != "" {
		if provider == ProviderAnthropic {
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	resp, err := lib.GetHttpClient().Do(req)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to query OpenAI API: %w", err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to read OpenAI API response: %w", err)
	}
	if resp.StatusCode == 429 {
		return nil, OpenAiUsage{}, fmt.Errorf("received 429 error code from OpenAI (is your API key valid?)")
	}
	var apiResp openAiResponse
	err = json.Unmarshal(bodyText, &apiResp)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to parse OpenAI API response=%#v: %w", string(bodyText), err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, OpenAiUsage{}, fmt.Errorf("OpenAI API returned zero choices, parsed resp=%#v, resp body=%#v, resp.StatusCode=%d", apiResp, bodyText, resp.StatusCode)
	}
	ret := make([]string, 0)
	for _, item := range apiResp.Choices {
		norm := NormalizeAISuggestion(item.Message.Content)
		if norm == "" {
			continue
		}
		if !slices.Contains(ret, norm) {
			ret = append(ret, norm)
		}
	}
	hctx.GetLogger().Infof("For OpenAI query=%#v ==> %#v", query, ret)
	return ret, apiResp.Usage, nil
}

func buildShellAssistantMessages(query, shellName, osName string) []openAiMessage {
	if osName == "" {
		osName = "Linux"
	}
	if shellName == "" {
		shellName = "bash"
	}
	defaultSystemPrompt := "You are an expert programmer that loves to help people with writing shell commands. " +
		"You always reply with just a shell command and no additional context, information, or formatting. " +
		"Your replies will be directly executed in " + shellName + " on " + osName +
		", so ensure that they are correct and do not contain anything other than a shell command."

	if systemPrompt := getEnvWithFallbacks("AI_API_SYSTEM_PROMPT", "OPENAI_API_SYSTEM_PROMPT"); systemPrompt != "" {
		defaultSystemPrompt = systemPrompt
	}

	return []openAiMessage{
		{Role: "system", Content: defaultSystemPrompt},
		{Role: "user", Content: query},
	}
}

func resolveOllamaModel(overriddenModel string) string {
	model := shared.DefaultOllamaModel
	if envModel := getEnvWithFallbacks("OLLAMA_MODEL", "AI_API_MODEL", "OPENAI_API_MODEL"); envModel != "" {
		model = envModel
	}
	if overriddenModel != "" {
		model = overriddenModel
	}
	return model
}

func ollamaPromptFromMessages(msgs []openAiMessage) string {
	if len(msgs) < 2 {
		return ""
	}
	return msgs[0].Content + "\n\n" + msgs[1].Content
}

func makeSingleOllamaCall(apiEndpoint, model, prompt string) (string, error) {
	body := ollamaGenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	}
	apiReqStr, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to serialize JSON for Ollama API: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, apiEndpoint, bytes.NewBuffer(apiReqStr))
	if err != nil {
		return "", fmt.Errorf("failed to create Ollama API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := lib.GetHttpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query Ollama API: %w", err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Ollama API response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama API returned status %d, body=%#v", resp.StatusCode, string(bodyText))
	}
	var apiResp ollamaGenerateResponse
	if err := json.Unmarshal(bodyText, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse Ollama API response=%#v: %w", string(bodyText), err)
	}
	if apiResp.Response == "" {
		return "", fmt.Errorf("ollama API returned empty response, body=%#v", string(bodyText))
	}
	return NormalizeAISuggestion(apiResp.Response), nil
}

func getMultipleOllamaCompletions(apiEndpoint, query, shellName, osName, overriddenModel string, numberCompletions int) ([]string, OpenAiUsage, error) {
	hctx.GetLogger().Infof("Making %d parallel Ollama API calls for multiple completions", numberCompletions)
	msgs := buildShellAssistantMessages(query, shellName, osName)
	prompt := ollamaPromptFromMessages(msgs)
	model := resolveOllamaModel(overriddenModel)

	indices := make([]int, numberCompletions)
	for i := 0; i < numberCompletions; i++ {
		indices[i] = i
	}

	results, err := shared.ParallelMap(indices, func(index int) (apiCallResult, error) {
		text, err := makeSingleOllamaCall(apiEndpoint, model, prompt)
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
			if !slices.Contains(allResults, norm) {
				allResults = append(allResults, norm)
			}
		}
	}
	hctx.GetLogger().Infof("For Ollama query=%#v with %d parallel completions ==> %#v", query, numberCompletions, allResults)
	return allResults, OpenAiUsage{}, nil
}

// GetAiSuggestionsViaOllama calls Ollama's /api/generate endpoint (non-streaming JSON).
func GetAiSuggestionsViaOllama(apiEndpoint, query, shellName, osName, overriddenModel string, numberCompletions int) ([]string, OpenAiUsage, error) {
	if results := TestOnlyOverrideAiSuggestions[query]; len(results) > 0 {
		return NormalizeSuggestionSlice(results), OpenAiUsage{}, nil
	}

	hctx.GetLogger().Infof("Running AI query via Ollama for %#v", query)

	if envNumberCompletions := getEnvWithFallbacks("AI_API_NUMBER_COMPLETIONS", "OPENAI_API_NUMBER_COMPLETIONS"); envNumberCompletions != "" {
		n, err := strconv.Atoi(envNumberCompletions)
		if err == nil {
			numberCompletions = n
		}
	}

	if numberCompletions > 1 {
		return getMultipleOllamaCompletions(apiEndpoint, query, shellName, osName, overriddenModel, numberCompletions)
	}

	msgs := buildShellAssistantMessages(query, shellName, osName)
	prompt := ollamaPromptFromMessages(msgs)
	model := resolveOllamaModel(overriddenModel)

	text, err := makeSingleOllamaCall(apiEndpoint, model, prompt)
	if err != nil {
		return nil, OpenAiUsage{}, err
	}
	hctx.GetLogger().Infof("For Ollama query=%#v ==> %#v", query, text)
	if norm := NormalizeAISuggestion(text); norm != "" {
		return []string{norm}, OpenAiUsage{}, nil
	}
	return nil, OpenAiUsage{}, fmt.Errorf("ollama returned only whitespace or punctuation after normalization")
}

type AiSuggestionRequest struct {
	DeviceId          string `json:"device_id"`
	UserId            string `json:"user_id"`
	Query             string `json:"query"`
	NumberCompletions int    `json:"number_completions"`
	ShellName         string `json:"shell_name"`
	OsName            string `json:"os_name"`
	Model             string `json:"model"`
}

type AiSuggestionResponse struct {
	Suggestions []string `json:"suggestions"`
}

func createOpenAiRequest(query, shellName, osName, overriddenOpenAiModel string, numberCompletions int) openAiRequest {
	// Determine the default model based on available API keys
	defaultModel := "gpt-4o-mini"
	if os.Getenv("ANTHROPIC_API_KEY") != "" && os.Getenv("OPENAI_API_KEY") == "" {
		// If only Anthropic key is available, default to Claude
		defaultModel = "claude-haiku-4-5-20251001"
	}

	// Check for model override with generic env variable taking precedence
	model := defaultModel
	if envModel := getEnvWithFallbacks("AI_API_MODEL", "OPENAI_API_MODEL"); envModel != "" {
		model = envModel
	}
	if overriddenOpenAiModel != "" {
		model = overriddenOpenAiModel
	}

	// Check for number of completions override
	if envNumberCompletions := getEnvWithFallbacks("AI_API_NUMBER_COMPLETIONS", "OPENAI_API_NUMBER_COMPLETIONS"); envNumberCompletions != "" {
		n, err := strconv.Atoi(envNumberCompletions)
		if err == nil {
			numberCompletions = n
		}
	}

	msgs := buildShellAssistantMessages(query, shellName, osName)

	return openAiRequest{
		Model:             model,
		NumberCompletions: numberCompletions,
		Messages:          msgs,
	}
}
