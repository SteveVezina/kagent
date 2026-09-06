package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinnedPiVersionMatchesPublishedPackage(t *testing.T) {
	require.Equal(t, "0.85.1", PinnedPiVersion)
}

func TestProductionUsesPinnedRuntimeDefaults(t *testing.T) {
	provider := Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: OpenAIAPIKeyEnvName}
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
	want := Production(Provider{Name: "kagent-anthropic", BaseURL: "https://api.anthropic.com", API: "anthropic-messages", APIKeyEnv: AnthropicAPIKeyEnvName}, "claude-sonnet-4-5", "be concise")
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
		"provider":{"name":"kagent-openai","base_url":"https://api.openai.com/v1","api":"openai-completions","api_key_env":"OPENAI_API_KEY"},
		"max_frame_bytes":1048576,
		"max_stderr_bytes":65536,
		"interrupt_grace_millis":2000,
		"unexpected":true
	}`))

	require.ErrorContains(t, err, "unknown field")
}

func TestParseRejectsWrongVersion(t *testing.T) {
	cfg := Production(Provider{Name: "kagent-anthropic", BaseURL: "https://api.anthropic.com", API: "anthropic-messages", APIKeyEnv: AnthropicAPIKeyEnvName}, "claude-sonnet-4-5", "")
	cfg.Version++
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = Parse(raw)
	require.ErrorContains(t, err, "unsupported config version")
}

func TestValidateRejectsProviderContractDrift(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     string
	}{
		{
			name: "OpenAI API",
			provider: Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "future", APIKeyEnv: OpenAIAPIKeyEnvName},
			want: "OpenAI API",
		},
		{
			name: "OpenAI credential env",
			provider: Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OTHER"},
			want: OpenAIAPIKeyEnvName,
		},
		{
			name: "Anthropic API",
			provider: Provider{Name: "kagent-anthropic", BaseURL: "https://api.anthropic.com", API: "future", APIKeyEnv: AnthropicAPIKeyEnvName},
			want: "Anthropic API",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Production(tc.provider, "model", "")
			require.ErrorContains(t, cfg.Validate(), tc.want)
		})
	}
}