package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared/ai"
)

var mostRecentQuery string

func DebouncedGetAiSuggestions(ctx context.Context, query string, numberCompletions int) ([]string, error) {
	mostRecentQuery = query
	time.Sleep(time.Millisecond * 300)
	if mostRecentQuery == query {
		return GetAiSuggestions(ctx, query, numberCompletions)
	}
	return nil, nil
}

func GetAiSuggestions(ctx context.Context, query string, numberCompletions int) ([]string, error) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		return GetAiSuggestionsViaHishtoryApi(ctx, query, numberCompletions)
	} else {
		suggestions, _, err := ai.GetAiSuggestionsViaOpenAiApi(query, getShellName(), getOsName(), numberCompletions)
		return suggestions, err
	}
}

func getOsName() string {
	switch runtime.GOOS {
	case "linux":
		return "Linux"
	case "darwin":
		return "MacOS"
	default:
		return runtime.GOOS
	}
}

func getShellName() string {
	return "bash"
}

func GetAiSuggestionsViaHishtoryApi(ctx context.Context, query string, numberCompletions int) ([]string, error) {
	hctx.GetLogger().Infof("Running OpenAI query for %#v", query)
	req := ai.AiSuggestionRequest{
		DeviceId:          hctx.GetConf(ctx).DeviceId,
		UserId:            data.UserId(hctx.GetConf(ctx).UserSecret),
		Query:             query,
		NumberCompletions: numberCompletions,
		OsName:            getOsName(),
		ShellName:         getShellName(),
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal AiSuggestionRequest: %w", err)
	}
	respData, err := lib.ApiPost(ctx, "/api/v1/ai-suggest", "application/json", reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to query /api/v1/ai-suggest: %w", err)
	}
	var resp ai.AiSuggestionResponse
	err = json.Unmarshal(respData, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse /api/v1/ai-suggest response: %w", err)
	}
	hctx.GetLogger().Infof("For OpenAI query=%#v ==> %#v", query, resp.Suggestions)
	return resp.Suggestions, nil
}
