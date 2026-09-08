package config

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/stretchr/testify/require"
)

func TestParseRoundTripsMCPAndSkills(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.SkillResources = &agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "deploy",
		Source: agentplugin.Source{OCI: "example.invalid/skills/deploy@sha256:deadbeef"},
	}}}
	cfg.MCPServers = []MCPServer{{
		Name: "tools",
		URL:  "https://mcp.example.com/mcp",
		Headers: map[string]string{
			"Authorization": "__KAGENT_ENV[" + MCPCredentialEnvPrefix + "0123ABCD]__",
		},
		EnabledTools:   []string{"lookup", "search"},
		TimeoutSeconds: 30,
	}}

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	got, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, cfg, got)
}

func TestParseNormalizesMCPOrdering(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.MCPServers = []MCPServer{
		{Name: "zeta", URL: "https://zeta.example.com/mcp", EnabledTools: []string{"search", "lookup"}, TimeoutSeconds: 30},
		{Name: "alpha", URL: "https://alpha.example.com/mcp", TimeoutSeconds: 30},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	got, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "zeta"}, []string{got.MCPServers[0].Name, got.MCPServers[1].Name})
	require.Equal(t, []string{"lookup", "search"}, got.MCPServers[1].EnabledTools)
}

func TestParseRejectsInvalidMCPTimeout(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.MCPServers = []MCPServer{{Name: "tools", URL: "https://mcp.example.com/mcp", TimeoutSeconds: 0}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = Parse(raw)
	require.ErrorContains(t, err, "timeout must be positive")
}

func TestParseRejectsDuplicateEnabledMCPTool(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName},
		"gpt-5.4",
		"help",
	)
	cfg.MCPServers = []MCPServer{{
		Name: "tools", URL: "https://mcp.example.com/mcp", EnabledTools: []string{"lookup", "lookup"}, TimeoutSeconds: 30,
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = Parse(raw)
	require.ErrorContains(t, err, "duplicate enabled tool")
}
