package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPreparesPrivatePiState(t *testing.T) {
	directory := t.TempDir()
	workspace := filepath.Join(directory, "workspace")
	durable := filepath.Join(directory, "data")
	configJSON := []byte(`{
		"model":{"type":"openai","model":"gpt-5.4"},
		"description":"",
		"instruction":"help",
		"stream":true
	}`)

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
	configJSON := []byte(`{
		"model":{"type":"openai","model":"gpt-4.1-mini","base_url":"http://mock-llm:8080/v1","api_format":"chatCompletions"},
		"description":"",
		"instruction":"reply briefly",
		"stream":true
	}`)

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
	configJSON := []byte(`{
		"model":{"type":"anthropic","model":"claude-custom","base_url":"https://gateway.example.com/anthropic"},
		"description":"",
		"instruction":"reply briefly",
		"stream":true
	}`)

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

func TestNewRejectsRelativeActorPaths(t *testing.T) {
	_, err := New(context.Background(), Input{
		ConfigJSON: []byte(`{"model":{"type":"anthropic","model":"claude-sonnet-4-5"},"description":"","instruction":"help"}`),
		Workspace: "workspace", DurableDir: "data",
	})

	require.ErrorContains(t, err, "absolute paths")
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
