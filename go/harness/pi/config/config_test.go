package config

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
)

func TestProductionUsesPinnedRuntimeDefaults(t *testing.T) {
	provider := Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions"}
	cfg := Production(provider, "gpt-5.4", "help")

	require.Equal(t, Version, cfg.Version)
	require.Equal(t, "pi", cfg.PiExecutable)
	require.Equal(t, PinnedPiVersion, cfg.ExpectedPiVersion)
	require.True(t, cfg.StrictVersion)
	require.Equal(t, provider, cfg.Provider)
	require.Equal(t, "gpt-5.4", cfg.Model)
	require.Equal(t, "help", cfg.SystemPrompt)
	require.Equal(t, 1<<20, cfg.MaxFrameBytes)
	require.Equal(t, 64<<10, cfg.MaxStderrBytes)
	require.Equal(t, 2000, cfg.InterruptGraceMillis)
}

func TestParseRoundTripsProductionConfig(t *testing.T) {
	want := Production(Provider{Name: "anthropic"}, "claude-sonnet-4-5", "be concise")
	raw, err := json.Marshal(want)
	require.NoError(t, err)

	got, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`{
		"version":1,
		"pi_executable":"pi",
		"expected_pi_version":"0.85.1",
		"strict_version":true,
		"model":"gpt-5.4",
		"provider":{"name":"kagent-openai","base_url":"https://api.openai.com/v1","api":"openai-completions"},
		"max_frame_bytes":1048576,
		"max_stderr_bytes":65536,
		"interrupt_grace_millis":2000,
		"unexpected":true
	}`))

	require.ErrorContains(t, err, "unknown field")
}

func TestParseRejectsWrongVersion(t *testing.T) {
	cfg := Production(Provider{Name: "anthropic"}, "claude-sonnet-4-5", "")
	cfg.Version++
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = Parse(raw)
	require.ErrorContains(t, err, "unsupported config version")
}

func TestFromAgentConfigMapsOpenAI(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model:       &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-5.4"}},
		Instruction: "You are a Kubernetes troubleshooting agent.",
	})

	require.NoError(t, err)
	require.Equal(t, Production(
		Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions"},
		"gpt-5.4",
		"You are a Kubernetes troubleshooting agent.",
	), cfg)
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
	require.Equal(t, Production(
		Provider{Name: "kagent-openai", BaseURL: "http://mock-llm.kagent.svc.cluster.local/v1", API: "openai-completions"},
		"gpt-4.1-mini",
		"Reply briefly.",
	), cfg)
}

func TestFromAgentConfigMapsOpenAIResponses(t *testing.T) {
	cfg, err := FromAgentConfig(&adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{Model: "gpt-5.4"},
			APIFormat: "responses",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1", cfg.Provider.BaseURL)
	require.Equal(t, "openai-responses", cfg.Provider.API)
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
	require.Equal(t, Production(Provider{Name: "anthropic"}, "claude-sonnet-4-5", "Be concise."), cfg)
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
		Model:     &adk.Anthropic{BaseModel: adk.BaseModel{Model: "claude-sonnet-4-5"}},
		HttpTools: []adk.HttpMcpServerConfig{{}},
	})

	require.ErrorContains(t, err, "MCP tools")
}

func TestFromAgentConfigRequiresModel(t *testing.T) {
	_, err := FromAgentConfig(&adk.AgentConfig{})
	require.ErrorContains(t, err, "model is required")
}
