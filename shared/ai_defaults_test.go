package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOllamaGenerateAPIEndpoint(t *testing.T) {
	require.True(t, IsOllamaGenerateAPIEndpoint(DefaultOllamaGenerateEndpoint))
	require.True(t, IsOllamaGenerateAPIEndpoint("http://127.0.0.1:11434/api/generate"))
	require.True(t, IsOllamaGenerateAPIEndpoint("http://localhost:11434/api/generate/"))
	require.False(t, IsOllamaGenerateAPIEndpoint("https://api.openai.com/v1/chat/completions"))
	require.False(t, IsOllamaGenerateAPIEndpoint("http://localhost:11434/api/chat"))
	require.False(t, IsOllamaGenerateAPIEndpoint(""))
}
