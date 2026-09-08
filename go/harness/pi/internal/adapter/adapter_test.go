package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	piconfig "github.com/kagent-dev/kagent/go/harness/pi/config"
	"github.com/stretchr/testify/require"
)

func TestNewPreparesPrivatePiState(t *testing.T) {
	directory := t.TempDir()
	workspace := filepath.Join(directory, "workspace")
	durable := filepath.Join(directory, "data")
	configJSON := nativeConfigJSON(t, piconfig.Provider{
		Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY",
	}, "gpt-5.4", "help")

	runner, err := New(context.Background(), Input{
		ConfigJSON: configJSON, Workspace: workspace, DurableDir: durable, Environment: os.Environ(),
	})

	require.NoError(t, err)
	require.NotNil(t, runner)
	for _, path := range []string{workspace, filepath.Join(durable, "pi"), filepath.Join(durable, "pi", "sessions")} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	}
}

func TestNewMaterializesCompilerOwnedOpenAIGateway(t *testing.T) {
	directory := t.TempDir()
	workspace := filepath.Join(directory, "workspace")
	durable := filepath.Join(directory, "data")
	configJSON := nativeConfigJSON(t, piconfig.Provider{
		Name: "kagent-openai", BaseURL: "http://mock-llm:8080/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY",
	}, "gpt-4.1-mini", "reply briefly")

	runner, err := New(context.Background(), Input{
		ConfigJSON: configJSON, Workspace: workspace, DurableDir: durable, Environment: append(os.Environ(), "OPENAI_API_KEY=fake"),
	})

	require.NoError(t, err)
	require.NotNil(t, runner)
	provider := readMaterializedProvider(t, durable, "kagent-openai")
	require.Equal(t, "http://mock-llm:8080/v1", provider["baseUrl"])
	require.Equal(t, "openai-completions", provider["api"])
	require.Equal(t, "$OPENAI_API_KEY", provider["apiKey"])
	model := provider["models"].([]any)[0].(map[string]any)
	require.Equal(t, "gpt-4.1-mini", model["id"])
}

func TestNewMaterializesCompilerOwnedAnthropicGateway(t *testing.T) {
	directory := t.TempDir()
	workspace := filepath.Join(directory, "workspace")
	durable := filepath.Join(directory, "data")
	configJSON := nativeConfigJSON(t, piconfig.Provider{
		Name: "kagent-anthropic", BaseURL: "https://gateway.example.com/anthropic", API: "anthropic-messages", APIKeyEnv: "ANTHROPIC_API_KEY",
	}, "claude-custom", "reply briefly")

	runner, err := New(context.Background(), Input{
		ConfigJSON: configJSON, Workspace: workspace, DurableDir: durable, Environment: append(os.Environ(), "ANTHROPIC_API_KEY=fake"),
	})

	require.NoError(t, err)
	require.NotNil(t, runner)
	provider := readMaterializedProvider(t, durable, "kagent-anthropic")
	require.Equal(t, "https://gateway.example.com/anthropic", provider["baseUrl"])
	require.Equal(t, "anthropic-messages", provider["api"])
	require.Equal(t, "$ANTHROPIC_API_KEY", provider["apiKey"])
	model := provider["models"].([]any)[0].(map[string]any)
	require.Equal(t, "claude-custom", model["id"])
}

func TestNewRejectsUnknownNativeConfigField(t *testing.T) {
	cfg := piconfig.Production(piconfig.Provider{
		Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY",
	}, "gpt-5.4", "help")
	contents, err := json.Marshal(cfg)
	require.NoError(t, err)
	contents[len(contents)-1] = ','
	contents = append(contents, []byte(`"unexpected":true}`)...)

	_, err = New(context.Background(), Input{
		ConfigJSON: contents, Workspace: filepath.Join(t.TempDir(), "workspace"), DurableDir: filepath.Join(t.TempDir(), "data"),
	})
	require.ErrorContains(t, err, "unknown field")
}

func TestNewRejectsRelativeActorPaths(t *testing.T) {
	configJSON := nativeConfigJSON(t, piconfig.Provider{
		Name: "kagent-anthropic", BaseURL: "https://api.anthropic.com", API: "anthropic-messages", APIKeyEnv: "ANTHROPIC_API_KEY",
	}, "claude-sonnet-4-5", "help")
	_, err := New(context.Background(), Input{ConfigJSON: configJSON, Workspace: "workspace", DurableDir: "data"})
	require.ErrorContains(t, err, "absolute paths")
}

func nativeConfigJSON(t *testing.T, provider piconfig.Provider, model, prompt string) []byte {
	t.Helper()
	contents, err := json.Marshal(piconfig.Production(provider, model, prompt))
	require.NoError(t, err)
	return contents
}

func readMaterializedProvider(t *testing.T, durable, providerName string) map[string]any {
	t.Helper()
	modelsPath := filepath.Join(durable, "pi", "models.json")
	contents, err := os.ReadFile(modelsPath)
	require.NoError(t, err)
	var models map[string]any
	require.NoError(t, json.Unmarshal(contents, &models))
	provider := models["providers"].(map[string]any)[providerName].(map[string]any)
	info, err := os.Stat(modelsPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	return provider
}
