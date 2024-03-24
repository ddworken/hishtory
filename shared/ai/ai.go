package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ddworken/hishtory/client/hctx"
	"golang.org/x/exp/slices"
)

const DefaultOpenAiEndpoint = "https://api.openai.com/v1/chat/completions"

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

func GetAiSuggestionsViaOpenAiApi(apiEndpoint, query, shellName, osName string, numberCompletions int) ([]string, OpenAiUsage, error) {
	if results := TestOnlyOverrideAiSuggestions[query]; len(results) > 0 {
		return results, OpenAiUsage{}, nil
	}
	hctx.GetLogger().Infof("Running OpenAI query for %#v", query)
	if osName == "" {
		osName = "Linux"
	}
	if shellName == "" {
		shellName = "bash"
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" && apiEndpoint == DefaultOpenAiEndpoint {
		return nil, OpenAiUsage{}, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}
	client := &http.Client{}
	apiReq := openAiRequest{
		Model:             "gpt-3.5-turbo",
		NumberCompletions: numberCompletions,
		Messages: []openAiMessage{
			{Role: "system", Content: "You are an expert programmer that loves to help people with writing shell commands. " +
				"You always reply with just a shell command and no additional context, information, or formatting. " +
				"Your replies will be directly executed in " + shellName + " on " + osName +
				", so ensure that they are correct and do not contain anything other than a shell command."},
			{Role: "user", Content: query},
		},
	}
	apiReqStr, err := json.Marshal(apiReq)
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to serialize JSON for OpenAI API: %w", err)
	}
	req, err := http.NewRequest("POST", apiEndpoint, bytes.NewBuffer(apiReqStr))
	if err != nil {
		return nil, OpenAiUsage{}, fmt.Errorf("failed to create OpenAI API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
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
		return nil, OpenAiUsage{}, fmt.Errorf("failed to parse OpenAI API response=%#v: %w", bodyText, err)
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
}

type AiSuggestionResponse struct {
	Suggestions []string `json:"suggestions"`
}
