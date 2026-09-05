package config

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
)

func TestFromAgentConfigMapsOpenAI(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		Instruction: "You are a Kubernetes troubleshooting agent.",
	})

	require.NoError(t, err)
	require.Equal(t, Config{
		Provider: "openai", Model: "gpt-5.4", SystemPrompt: "You are a Kubernetes troubleshooting agent.",
	}, cfg)
}

func TestFromAgentConfigMapsAnthropic(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model:       &adk.Anthropic{BaseModel: adk.BaseModel{Model: "claude-sonnet-4-5"}},
		Instruction: "Be concise.",
	})

	require.NoError(t, err)
	require.Equal(t, Config{Provider: "anthropic", Model: "claude-sonnet-4-5", SystemPrompt: "Be concise."}, cfg)
}

func TestFromAgentConfigRejectsUnsupportedModelConfiguration(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{Model: "gpt-5.4"},
			BaseUrl:   "https://gateway.example.com/v1",
		},
	})

	require.ErrorContains(t, err, "custom OpenAI base URL")
}

func TestFromAgentConfigRejectsUnsupportedAgentFeatures(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.Anthropic{BaseModel: adk.BaseModel{Model: "claude-sonnet-4-5"}},
		HttpTools: []adk.HttpMcpServerConfig{{}},
	})

	require.ErrorContains(t, err, "MCP tools")
}

func TestFromAgentConfigRequiresModel(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{})
	require.ErrorContains(t, err, "model is required")
}
