package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared/ai"
)

var mostRecentQuery string

func DebouncedGetAiSuggestions(ctx context.Context, shellName, query string, numberCompletions int) ([]string, error) {
	mostRecentQuery = query
	time.Sleep(time.Millisecond * 300)
	if mostRecentQuery == query {
		return GetAiSuggestions(ctx, shellName, query, numberCompletions)
	}
	return nil, nil
}

func extractFilenames(text string) []string {
	if strings.Count(text, "`")%2 != 0 {
		return []string{}
	}
	pattern := "`([^`]*)`"
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(text, -1)
	filenames := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			filename := match[1]
			if len(filename) == 0 || len(filename) > 50 {
				return []string{}
			}
			_, err := os.Stat(filename)
			if err != nil {
				return []string{}
			}
			filenames = append(filenames, filename)
		}
	}

	return filenames
}

func augmentQuery(ctx context.Context, query string) string {
	if !hctx.GetConf(ctx).BetaMode {
		return query
	}
	filenames := extractFilenames(query)
	if len(filenames) == 0 {
		return query
	}
	newQuery := "Context:\n"
	for _, filename := range filenames {
		newQuery += "The file `" + filename + "` has contents like:\n```"
		contents, err := os.ReadFile(filename)
		if err != nil {
			hctx.GetLogger().Warnf("while augmenting OpenAI query=%#v, failed to read the contents of %#v: %v", query, filename, err)
			return query
		}
		lines := strings.Split(string(contents), "\n")
		trimmed := lines[:min(5, len(lines))]
		newQuery += strings.Join(trimmed, "\n")
		newQuery += "\n...```\n\n"
	}
	newQuery += query
	return newQuery
}

func GetAiSuggestions(ctx context.Context, shellName, query string, numberCompletions int) ([]string, error) {
	if os.Getenv("OPENAI_API_KEY") == "" && hctx.GetConf(ctx).AiCompletionEndpoint == ai.DefaultOpenAiEndpoint {
		return GetAiSuggestionsViaHishtoryApi(ctx, shellName, augmentQuery(ctx, query), numberCompletions)
	} else {
		suggestions, _, err := ai.GetAiSuggestionsViaOpenAiApi(hctx.GetConf(ctx).AiCompletionEndpoint, augmentQuery(ctx, query), shellName, getOsName(), os.Getenv("OPENAI_API_MODEL"), numberCompletions)
		return suggestions, err
	}
}

func getOsName() string {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("apt-get"); err == nil {
			return "Ubuntu Linux"
		}
		if _, err := exec.LookPath("dnf"); err == nil {
			return "Fedora Linux"
		}
		if _, err := exec.LookPath("pacman"); err == nil {
			return "Arch Linux"
		}
		return "Linux"
	case "darwin":
		return "MacOS"
	default:
		return runtime.GOOS
	}
}

func GetAiSuggestionsViaHishtoryApi(ctx context.Context, shellName, query string, numberCompletions int) ([]string, error) {
	hctx.GetLogger().Infof("Running OpenAI query for %#v via hishtory server", query)
	req := ai.AiSuggestionRequest{
		DeviceId:          hctx.GetConf(ctx).DeviceId,
		UserId:            data.UserId(hctx.GetConf(ctx).UserSecret),
		Query:             query,
		NumberCompletions: numberCompletions,
		OsName:            getOsName(),
		ShellName:         shellName,
		Model:             os.Getenv("OPENAI_API_MODEL"),
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
