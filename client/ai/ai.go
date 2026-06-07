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
	"github.com/ddworken/hishtory/shared"
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
		trimmed := lines[:min(10, len(lines))]
		newQuery += strings.Join(trimmed, "\n")
		newQuery += "\n...```\n\n"
	}
	newQuery += query

	return newQuery
}

func GetAiSuggestions(ctx context.Context, shellName, query string, numberCompletions int) ([]string, error) {
	cfg := hctx.GetConf(ctx)
	hasKeys := shared.HasAiAPIKeys()
	endpoint := cfg.AiCompletionEndpoint
	augmented := augmentQuery(ctx, query)

	// Explicit "chat" backend with no keys and default cloud URLs: hiSHtory proxy (legacy no-key path).
	if cfg.AiCompletionBackend == shared.AiCompletionBackendChat && !hasKeys &&
		(endpoint == ai.DefaultOpenAiEndpoint || endpoint == ai.DefaultClaudeEndpoint) {
		return GetAiSuggestionsViaHishtoryApi(ctx, shellName, augmented, numberCompletions)
	}

	// Gemini: forced backend, or auto with no API keys (default).
	if cfg.AiCompletionBackend == shared.AiCompletionBackendGemini ||
		(!hasKeys && cfg.AiCompletionBackend != shared.AiCompletionBackendChat &&
			cfg.AiCompletionBackend != shared.AiCompletionBackendOllama) {
		modelOverride := os.Getenv("AI_API_MODEL")
		if modelOverride == "" {
			modelOverride = os.Getenv("GEMINI_MODEL")
		}
		suggestions, _, err := ai.GetAiSuggestionsViaGeminiWeb(augmented, shellName, getOsName(), modelOverride, numberCompletions)
		return suggestions, err
	}

	// Ollama: forced backend or endpoint is Ollama /api/generate.
	if cfg.AiCompletionBackend == shared.AiCompletionBackendOllama ||
		shared.IsOllamaGenerateAPIEndpoint(endpoint) {
		suggestions, _, err := ai.GetAiSuggestionsViaOllama(endpoint, augmented, shellName, getOsName(), "", numberCompletions)
		return suggestions, err
	}

	modelOverride := os.Getenv("AI_API_MODEL")
	if modelOverride == "" {
		modelOverride = os.Getenv("OPENAI_API_MODEL")
	}
	suggestions, _, err := ai.GetAiSuggestionsViaOpenAiApi(endpoint, augmented, shellName, getOsName(), modelOverride, numberCompletions)
	return suggestions, err
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
	hctx.GetLogger().Infof("Running AI query for %#v via hishtory server", query)

	// Get model override with generic env variable taking precedence
	modelOverride := os.Getenv("AI_API_MODEL")
	if modelOverride == "" {
		modelOverride = os.Getenv("OPENAI_API_MODEL")
	}

	req := ai.AiSuggestionRequest{
		DeviceId:          hctx.GetConf(ctx).DeviceId,
		UserId:            data.UserId(hctx.GetConf(ctx).UserSecret),
		Query:             query,
		NumberCompletions: numberCompletions,
		OsName:            getOsName(),
		ShellName:         shellName,
		Model:             modelOverride,
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
	hctx.GetLogger().Infof("For AI query=%#v ==> %#v", query, resp.Suggestions)
	return ai.NormalizeSuggestionSlice(resp.Suggestions), nil
}
