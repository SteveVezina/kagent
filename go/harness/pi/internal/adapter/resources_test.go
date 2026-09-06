package adapter

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/stretchr/testify/require"
)

func TestNewMaterializesPrivateMCPConfig(t *testing.T) {
	directory := t.TempDir()
	workspace := filepath.Join(directory, "workspace")
	durable := filepath.Join(directory, "data")
	configJSON := marshalAgentConfig(t, &adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-4.1-mini"}},
		HttpTools: []adk.HttpMcpServerConfig{{
			Params: adk.StreamableHTTPConnectionParams{
				Url: "https://mcp.example.com/mcp",
				Headers: map[string]string{
					"Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__",
				},
			},
			Tools: []string{"lookup"},
		}},
	})

	runner, err := New(context.Background(), Input{
		ConfigJSON: configJSON,
		Workspace:  workspace,
		DurableDir: durable,
		Environment: append(os.Environ(),
			"OPENAI_API_KEY=fake",
			"KAGENT_CREDENTIAL_0123ABCD=actual-secret-value",
		),
	})

	require.NoError(t, err)
	require.NotNil(t, runner)
	path := filepath.Join(durable, "pi", "mcp.json")
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(contents), "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__")
	require.NotContains(t, string(contents), "actual-secret-value")
	var document struct {
		Servers []struct {
			URL          string            `json:"url"`
			Headers      map[string]string `json:"headers"`
			EnabledTools []string          `json:"enabled_tools"`
		} `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(contents, &document))
	require.Len(t, document.Servers, 1)
	require.Equal(t, "https://mcp.example.com/mcp", document.Servers[0].URL)
	require.Equal(t, []string{"lookup"}, document.Servers[0].EnabledTools)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestNewMaterializesCompilerOwnedSkills(t *testing.T) {
	repository, commit := gitFixture(t, map[string]string{
		"SKILL.md": "# Review\nUse the configured review process.\n",
	})
	directory := t.TempDir()
	durable := filepath.Join(directory, "data")
	configJSON := marshalAgentConfig(t, &adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-4.1-mini"}},
		AgentPlugins: &agentplugin.Resources{Skills: []agentplugin.Skill{{
			Name: "review",
			Source: agentplugin.Source{Git: &agentplugin.GitSource{
				URL: repository, Commit: commit,
			}},
		}}},
	})

	runner, err := New(context.Background(), Input{
		ConfigJSON: configJSON,
		Workspace:  filepath.Join(directory, "workspace"),
		DurableDir: durable,
		Environment: append(os.Environ(),
			"OPENAI_API_KEY=fake",
		),
	})

	require.NoError(t, err)
	require.NotNil(t, runner)
	contents, err := os.ReadFile(filepath.Join(durable, "pi", "skills", "review", "SKILL.md"))
	require.NoError(t, err)
	require.Contains(t, string(contents), "Review")
	_, err = os.Stat(filepath.Join(durable, "pi", "packages", "standalone-0"))
	require.NoError(t, err)
}

func TestNewReconcilesStaleCompilerOwnedSkills(t *testing.T) {
	directory := t.TempDir()
	durable := filepath.Join(directory, "data")
	stale := filepath.Join(durable, "pi", "skills", "stale")
	require.NoError(t, os.MkdirAll(stale, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "SKILL.md"), []byte("# stale"), 0o644))

	configJSON := marshalAgentConfig(t, &adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-4.1-mini"}},
	})
	_, err := New(context.Background(), Input{
		ConfigJSON:  configJSON,
		Workspace:   filepath.Join(directory, "workspace"),
		DurableDir:  durable,
		Environment: append(os.Environ(), "OPENAI_API_KEY=fake"),
	})

	require.NoError(t, err)
	_, err = os.Stat(stale)
	require.True(t, os.IsNotExist(err))
}

func TestNewRejectsAgentPluginMCPUntilSupported(t *testing.T) {
	repository, commit := gitFixture(t, map[string]string{
		"plugin.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test"}`,
		"mcp.json":    `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"local":{"type":"stdio","command":"server"}}}`,
	})
	directory := t.TempDir()
	configJSON := marshalAgentConfig(t, &adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "gpt-4.1-mini"}},
		AgentPlugins: &agentplugin.Resources{Plugins: []agentplugin.Bundle{{
			Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}},
		}}},
	})

	_, err := New(context.Background(), Input{
		ConfigJSON:  configJSON,
		Workspace:   filepath.Join(directory, "workspace"),
		DurableDir:  filepath.Join(directory, "data"),
		Environment: append(os.Environ(), "OPENAI_API_KEY=fake"),
	})

	require.ErrorContains(t, err, "Agent Plugin MCP")
}

func marshalAgentConfig(t *testing.T, cfg *adk.AgentConfig) []byte {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return raw
}

func gitFixture(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	repository := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
		return strings.TrimSpace(string(output))
	}
	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	for name, content := range files {
		path := filepath.Join(repository, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	git("add", ".")
	git("commit", "-m", "fixture")
	return repository, git("rev-parse", "HEAD")
}
