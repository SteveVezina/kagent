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
		Provider: "kagent-openai",
		Model: "gpt-5.4",
		SystemPrompt: "You are a Kubernetes troubleshooting agent.",
		BaseURL: "https://api.openai.com/v1",
		API: "openai-completions",
	}, cfg)
}

func TestFromAgentConfigMapsOpenAIGateway(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{Model: "gpt-4.1-mini"},
			BaseUrl:   "http://mock-llm.kagent.svc.cluster.local/v1",
			APIFormat: "chatCompletions",
		},
		Instruction: "Reply briefly.",
	})

	require.NoError(t, err)
	require.Equal(t, Config{
		Provider: "kagent-openai",
		Model: "gpt-4.1-mini",
		SystemPrompt: "Reply briefly.",
		BaseURL: "http://mock-llm.kagent.svc.cluster.local/v1",
		API: "openai-completions",
	}, cfg)
}

func TestFromAgentConfigMapsOpenAIResponses(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{Model: "gpt-5.4"},
			APIFormat: "responses",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1", cfg.BaseURL)
	require.Equal(t, "openai-responses", cfg.API)
}

func TestFromAgentConfigRejectsUnknownOpenAIAPIFormat(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{Model: "gpt-5.4"},
			APIFormat: "future-api",
		},
	})

	require.ErrorContains(t, err, "OpenAI API format")
}

func TestFromAgentConfigRejectsUnsupportedOpenAITuning(t *testing.T) {
	temperature := 0.2
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel:   adk.BaseModel{Model: "gpt-5.4"},
			Temperature: &temperature,
		},
	})

	require.ErrorContains(t, err, "OpenAI model tuning")
}

func TestFromAgentConfigMapsAnthropic(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model:       &adk.Anthropic{BaseModel: adk.BaseModel{Model: "claude-sonnet-4-5"}},
		Instruction: "Be concise.",
	})

	require.NoError(t, err)
	require.Equal(t, Config{Provider: "anthropic", Model: "claude-sonnet-4-5", SystemPrompt: "Be concise."}, cfg)
}

func TestFromAgentConfigRejectsUnsupportedAnthropicTuning(t *testing.T) {
	maxTokens := 2048
	_, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.Anthropic{
			BaseModel: adk.BaseModel{Model: "claude-sonnet-4-5"},
			MaxTokens: &maxTokens,
		},
	})

	require.ErrorContains(t, err, "Anthropic model tuning")
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
