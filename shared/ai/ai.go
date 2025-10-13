package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"golang.org/x/exp/slices"
)

const (
	DefaultOpenAiEndpoint = "https://api.openai.com/v1/chat/completions"
	DefaultClaudeEndpoint = "https://api.anthropic.com/v1/chat/completions"
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

// getEnvWithFallbacks returns the first non-empty environment variable from the list
func getEnvWithFallbacks(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

// GetAiProvider determines which AI provider to use based on environment variables and endpoint
func GetAiProvider(apiEndpoint string) AiProvider {
	// Check for explicit API keys first
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return ProviderAnthropic
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return ProviderOpenAI
	}

	// Check generic AI_API_KEY and determine provider from endpoint
	if getEnvWithFallbacks("AI_API_KEY") != "" {
		if apiEndpoint == DefaultClaudeEndpoint {
			return ProviderAnthropic
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

func GetAiSuggestionsViaOpenAiApi(apiEndpoint, query, shellName, osName, overriddenOpenAiModel string, numberCompletions int) ([]string, OpenAiUsage, error) {
	if results := TestOnlyOverrideAiSuggestions[query]; len(results) > 0 {
		return results, OpenAiUsage{}, nil
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
		if !slices.Contains(ret, item.Message.Content) {
			ret = append(ret, item.Message.Content)
		}
	}
	hctx.GetLogger().Infof("For OpenAI query=%#v ==> %#v", query, ret)
	return ret, apiResp.Usage, nil
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
	if osName == "" {
		osName = "Linux"
	}
	if shellName == "" {
		shellName = "bash"
	}

	// Determine the default model based on available API keys
	defaultModel := "gpt-4o-mini"
	if os.Getenv("ANTHROPIC_API_KEY") != "" && os.Getenv("OPENAI_API_KEY") == "" {
		// If only Anthropic key is available, default to Claude
		defaultModel = "claude-sonnet-4-5"
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

	// Set default system prompt
	defaultSystemPrompt := "You are an expert programmer that loves to help people with writing shell commands. " +
		"You always reply with just a shell command and no additional context, information, or formatting. " +
		"Your replies will be directly executed in " + shellName + " on " + osName +
		", so ensure that they are correct and do not contain anything other than a shell command."

	if systemPrompt := getEnvWithFallbacks("AI_API_SYSTEM_PROMPT", "OPENAI_API_SYSTEM_PROMPT"); systemPrompt != "" {
		defaultSystemPrompt = systemPrompt
	}

	return openAiRequest{
		Model:             model,
		NumberCompletions: numberCompletions,
		Messages: []openAiMessage{
			{Role: "system", Content: defaultSystemPrompt},
			{Role: "user", Content: query},
		},
	}
}
