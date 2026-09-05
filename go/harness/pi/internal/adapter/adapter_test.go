package adapter

import (
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

	runner, err := New(Input{
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

func TestNewRejectsRelativeActorPaths(t *testing.T) {
	_, err := New(Input{
		ConfigJSON: []byte(`{"model":{"type":"anthropic","model":"claude-sonnet-4-5"},"description":"","instruction":"help"}`),
		Workspace: "workspace", DurableDir: "data",
	})

	require.ErrorContains(t, err, "absolute paths")
}
