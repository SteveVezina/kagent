package config

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/stretchr/testify/require"
)

func TestParseRoundTripsMCPAndSkills(t *testing.T) {
	cfg := Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY"},
		"gpt-5.4",
		"help",
	)
	cfg.SkillResources = &agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "deploy",
		Source: agentplugin.Source{OCI: "example.invalid/skills/deploy@sha256:deadbeef"},
	}}}
	cfg.MCPServers = []MCPServer{{
		URL: "https://mcp.example.com/mcp",
		Headers: map[string]string{
			"Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__",
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

func TestFromAgentConfigMapsStreamableHTTPMCP(t *testing.T) {
	timeout := 30.0
	terminate := true
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		HttpTools: []adk.HttpMcpServerConfig{{
			Params: adk.StreamableHTTPConnectionParams{
				Url: "https://mcp.example.com/mcp",
				Headers: map[string]string{
					"Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__",
				},
				Timeout:          &timeout,
				TerminateOnClose: &terminate,
			},
			Tools: []string{"search", "lookup", "search"},
		}},
	})

	require.NoError(t, err)
	require.Equal(t, []MCPServer{{
		URL: "https://mcp.example.com/mcp",
		Headers: map[string]string{
			"Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__",
		},
		EnabledTools:   []string{"lookup", "search"},
		TimeoutSeconds: 30,
	}}, cfg.MCPServers)
}

func TestFromAgentConfigMapsCustomMCPTimeout(t *testing.T) {
	timeout := 12.5
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		HttpTools: []adk.HttpMcpServerConfig{{
			Params: adk.StreamableHTTPConnectionParams{Url: "https://mcp.example.com/mcp", Timeout: &timeout},
		}},
	})

	require.NoError(t, err)
	require.Equal(t, 12.5, cfg.MCPServers[0].TimeoutSeconds)
}

func TestFromAgentConfigMapsSkillResources(t *testing.T) {
	resources := &agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "deploy",
		Source: agentplugin.Source{OCI: "example.invalid/skills/deploy@sha256:deadbeef"},
	}}}
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model:        &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		AgentPlugins: resources,
	})

	require.NoError(t, err)
	require.Equal(t, resources, cfg.SkillResources)
}

func TestFromAgentConfigRejectsUnsupportedMCPTransports(t *testing.T) {
	t.Run("sse", func(t *testing.T) {
		_, err := FromAgentConfig(&adk.AgentConfig{
			Model:    &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
			SseTools: []adk.SseMcpServerConfig{{}},
		})
		require.ErrorContains(t, err, "SSE MCP")
	})

	t.Run("stdio", func(t *testing.T) {
		_, err := FromAgentConfig(&adk.AgentConfig{
			Model:      &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
			StdioTools: []adk.StdioMcpServerConfig{{Command: "server"}},
		})
		require.ErrorContains(t, err, "stdio MCP")
	})
}

func TestFromAgentConfigRejectsMCPApproval(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		HttpTools: []adk.HttpMcpServerConfig{{
			Params:          adk.StreamableHTTPConnectionParams{Url: "https://mcp.example.com/mcp"},
			RequireApproval: []string{"deploy"},
		}},
	})

	require.ErrorContains(t, err, "approval")
}

func TestFromAgentConfigRejectsUnsupportedMCPRuntimeOverrides(t *testing.T) {
	timeout := 10.0
	insecure := true
	terminate := false
	cases := []struct {
		name   string
		params adk.StreamableHTTPConnectionParams
	}{
		{name: "sse read timeout", params: adk.StreamableHTTPConnectionParams{Url: "https://mcp.example.com/mcp", SseReadTimeout: &timeout}},
		{name: "terminate on close false", params: adk.StreamableHTTPConnectionParams{Url: "https://mcp.example.com/mcp", TerminateOnClose: &terminate}},
		{name: "custom tls", params: adk.StreamableHTTPConnectionParams{Url: "https://mcp.example.com/mcp", TLSInsecureSkipVerify: &insecure}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromAgentConfig(&adk.AgentConfig{
				Model:     &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
				HttpTools: []adk.HttpMcpServerConfig{{Params: tc.params}},
			})
			require.ErrorContains(t, err, "MCP")
		})
	}
}

func TestFromAgentConfigRejectsInvalidMCPTimeout(t *testing.T) {
	zero := 0.0
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		HttpTools: []adk.HttpMcpServerConfig{{
			Params: adk.StreamableHTTPConnectionParams{Url: "https://mcp.example.com/mcp", Timeout: &zero},
		}},
	})

	require.ErrorContains(t, err, "timeout")
}

func TestFromAgentConfigRejectsInvalidMCPURL(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		HttpTools: []adk.HttpMcpServerConfig{{
			Params: adk.StreamableHTTPConnectionParams{Url: "relative/mcp"},
		}},
	})

	require.ErrorContains(t, err, "MCP server")
}

func TestFromAgentConfigRejectsDuplicateMCPServerDefinition(t *testing.T) {
	server := adk.HttpMcpServerConfig{
		Params: adk.StreamableHTTPConnectionParams{
			Url:     "https://mcp.example.com/mcp",
			Headers: map[string]string{"X-Tenant": "one"},
		},
		Tools: []string{"lookup"},
	}
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model:     &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		HttpTools: []adk.HttpMcpServerConfig{server, server},
	})

	require.ErrorContains(t, err, "duplicate MCP server")
}
