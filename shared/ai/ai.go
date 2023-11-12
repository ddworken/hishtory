package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/zmwangx/debounce"
	"golang.org/x/exp/slices"
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
	Usage   openAiUsage    `json:"usage"`
	Choices []openAiChoice `json:"choices"`
}

type openAiChoice struct {
	Index        int           `json:"index"`
	Message      openAiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type debouncedAiSuggestionsIn struct {
	ctx               context.Context
	query             string
	numberCompletions int
}

type debouncedAiSuggestionsOut struct {
	suggestions []string
	err         error
}

var debouncedGetAiSuggestionsInner func(...debouncedAiSuggestionsIn) debouncedAiSuggestionsOut
var debouncedControl debounce.ControlWithReturnValue[debouncedAiSuggestionsOut]

func init() {
	openAiDebouncePeriod := time.Millisecond * 1000
	debouncedGetAiSuggestionsInner, debouncedControl = debounce.DebounceWithCustomSignature(
		func(input ...debouncedAiSuggestionsIn) debouncedAiSuggestionsOut {
			if len(input) != 1 {
				return debouncedAiSuggestionsOut{nil, fmt.Errorf("unexpected input length: %d", len(input))}
			}
			suggestions, err := GetAiSuggestions(input[0].ctx, input[0].query, input[0].numberCompletions)
			return debouncedAiSuggestionsOut{suggestions, err}
		},
		openAiDebouncePeriod,
		debounce.WithLeading(false),
		debounce.WithTrailing(true),
		debounce.WithMaxWait(openAiDebouncePeriod),
	)
	go func() {
		for {
			debouncedControl.Flush()
			time.Sleep(openAiDebouncePeriod)
		}
	}()
}

func DebouncedGetAiSuggestions(ctx context.Context, query string, numberCompletions int) ([]string, error) {
	resp := debouncedGetAiSuggestionsInner(debouncedAiSuggestionsIn{ctx, query, numberCompletions})
	return resp.suggestions, resp.err
}

func GetAiSuggestions(ctx context.Context, query string, numberCompletions int) ([]string, error) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		panic("TODO: Implement integration with the hishtory API")
	} else {
		return GetAiSuggestionsViaOpenAiApi(query, numberCompletions)
	}
}

func GetAiSuggestionsViaOpenAiApi(query string, numberCompletions int) ([]string, error) {
	hctx.GetLogger().Infof("Running OpenAI query for %#v", query)
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}
	client := &http.Client{}
	apiReq := openAiRequest{
		Model:             "gpt-3.5-turbo",
		NumberCompletions: numberCompletions,
		Messages: []openAiMessage{
			{Role: "system", Content: "You are an expert programmer that loves to help people with writing shell commands. You always reply with just a shell command and no additional context or information. Your replies will be directly executed in bash, so ensure that they are correct and do not contain anything other than a bash command."},
			{Role: "user", Content: query},
		},
	}
	apiReqStr, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize JSON for OpenAI API: %w", err)
	}
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(apiReqStr))
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAI API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query OpenAI API: %w", err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OpenAI API response: %w", err)
	}
	var apiResp openAiResponse
	err = json.Unmarshal(bodyText, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI API response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI API returned zero choicesm, resp=%#v", apiResp)
	}
	ret := make([]string, 0)
	for _, item := range apiResp.Choices {
		if !slices.Contains(ret, item.Message.Content) {
			ret = append(ret, item.Message.Content)
		}
	}
	hctx.GetLogger().Infof("For OpenAI query=%#v ==> %#v", query, ret)
	return ret, nil
}

type AiSuggestionRequest struct {
	DeviceId          string `json:"device_id"`
	UserId            string `json:"user_id"`
	Query             string `json:"query"`
	NumberCompletions int    `json:"number_completions"`
}

type AiSuggestionResponse struct {
	Suggestions []string `json:"suggestions"`
}
