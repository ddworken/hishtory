package shared

import (
	"net/url"
	"os"
	"strings"
)

const (
	DefaultOllamaGenerateEndpoint = "http://localhost:11434/api/generate"
	DefaultOllamaModel            = "eslider/bonsai-1.7b"
	DefaultGeminiModel            = "gemini-3.5-flash"
	AiCompletionBackendChat       = "chat"
	AiCompletionBackendOllama     = "ollama"
	AiCompletionBackendGemini     = "gemini"
)

// HasAiAPIKeys reports whether any supported cloud AI API key is set in the environment.
func HasAiAPIKeys() bool {
	return os.Getenv("OPENAI_API_KEY") != "" ||
		os.Getenv("ANTHROPIC_API_KEY") != "" ||
		os.Getenv("AI_API_KEY") != ""
}

// IsOllamaGenerateAPIEndpoint reports whether endpoint targets Ollama's POST /api/generate API
// (so the OpenAI chat client must not be used, even if cloud API keys are set).
func IsOllamaGenerateAPIEndpoint(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	path := strings.TrimSuffix(u.Path, "/")
	return strings.HasSuffix(path, "/api/generate")
}
