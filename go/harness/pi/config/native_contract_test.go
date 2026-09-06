package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnsEnvironment(t *testing.T) {
	for _, name := range []string{
		OpenAIAPIKeyEnvName,
		AnthropicAPIKeyEnvName,
		PiHomeEnvName,
		MCPConfigEnvName,
		OfflineEnvName,
		SkipVersionCheckEnvName,
		TelemetryEnvName,
		MCPCredentialEnvPrefix + "0123ABCD",
	} {
		t.Run(name, func(t *testing.T) {
			require.True(t, OwnsEnvironment(name), "expected Pi to own %s", name)
		})
	}

	require.False(t, OwnsEnvironment("PATH"))
	require.False(t, OwnsEnvironment("KAGENT_USER_SETTING"))
}

func TestParseAcceptsNativeMCPServerName(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.MCPServers = []MCPServer{{Name: "math-server", URL: "https://mcp.example.com/mcp"}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	got, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "math-server", got.MCPServers[0].Name)
}

func TestParseRejectsInvalidNativeMCPServerName(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.MCPServers = []MCPServer{{Name: "math.api", URL: "https://mcp.example.com/mcp"}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = Parse(raw)
	require.ErrorContains(t, err, "MCP server name")
}

func TestParseRejectsDuplicateNativeMCPServerName(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.MCPServers = []MCPServer{
		{Name: "math", URL: "https://one.example.com/mcp"},
		{Name: "math", URL: "https://two.example.com/mcp"},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = Parse(raw)
	require.ErrorContains(t, err, "duplicate Pi MCP server name")
}
