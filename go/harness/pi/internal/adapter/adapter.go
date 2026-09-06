// Package adapter validates native compiler output and materializes compiler-owned Pi state.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/pi/config"
	"github.com/kagent-dev/kagent/go/harness/pi/internal/driver"
)

const (
	piHomeEnv        = "PI_CODING_AGENT_DIR"
	mcpConfigEnv     = "KAGENT_PI_MCP_CONFIG"
	mcpExtensionPath = "/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts"
)

// Input contains compiler output and Actor-owned locations used to construct
// the Pi driver.
type Input struct {
	ConfigJSON  []byte
	Workspace   string
	DurableDir  string
	Environment []string
}

// New validates the native Pi config, prepares private Pi state, materializes
// compiler-owned resources, and constructs the RPC process driver.
func New(ctx context.Context, input Input) (*driver.ProcessDriver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := config.Parse(input.ConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("decode native Pi config: %w", err)
	}
	if !filepath.IsAbs(input.Workspace) || !filepath.IsAbs(input.DurableDir) {
		return nil, fmt.Errorf("workspace and durable directories must be absolute paths")
	}
	piHome := filepath.Join(input.DurableDir, "pi")
	sessionDir := filepath.Join(piHome, "sessions")
	packagesDir := filepath.Join(piHome, "packages")
	skillsDir := filepath.Join(piHome, "skills")
	for _, directory := range []string{input.Workspace, piHome, sessionDir, packagesDir, skillsDir} {
		if err := utils.EnsurePrivateDir(directory); err != nil {
			return nil, fmt.Errorf("prepare Pi directory %q: %w", directory, err)
		}
	}
	if err := reconcileGeneratedDir(skillsDir); err != nil {
		return nil, fmt.Errorf("reconcile Pi skills: %w", err)
	}
	if err := writeModelsConfig(piHome, cfg); err != nil {
		return nil, err
	}

	var skillPaths []string
	if cfg.SkillResources != nil {
		materialization, err := agentplugins.Materialize(ctx, *cfg.SkillResources, agentplugins.Paths{
			Packages: packagesDir,
			Skills:   skillsDir,
		})
		if err != nil {
			return nil, fmt.Errorf("materialize Pi skills: %w", err)
		}
		pluginMCP, err := agentplugins.LoadMCP(ctx, materialization, filepath.Join(piHome, "plugin-data"))
		if err != nil {
			return nil, fmt.Errorf("inspect Pi Agent Plugin MCP configuration: %w", err)
		}
		if len(pluginMCP.StreamableHTTP) != 0 || len(pluginMCP.SSE) != 0 || len(pluginMCP.Stdio) != 0 {
			return nil, fmt.Errorf("Pi does not support Agent Plugin MCP servers yet")
		}
		if hasSelectedSkills(*cfg.SkillResources) {
			skillPaths = []string{materialization.SkillsDirectory}
		}
	}

	var extensionPaths []string
	environment := append([]string(nil), input.Environment...)
	mcpPath := filepath.Join(piHome, "mcp.json")
	if len(cfg.MCPServers) != 0 {
		if err := writeMCPConfig(mcpPath, cfg.MCPServers); err != nil {
			return nil, err
		}
		environment = setEnvironment(environment, mcpConfigEnv, mcpPath)
		extensionPaths = []string{mcpExtensionPath}
	} else if err := removeGeneratedFile(mcpPath); err != nil {
		return nil, fmt.Errorf("remove stale Pi MCP configuration: %w", err)
	}

	environment = setEnvironment(environment, piHomeEnv, piHome)
	environment = setEnvironment(environment, "PI_OFFLINE", "1")
	environment = setEnvironment(environment, "PI_SKIP_VERSION_CHECK", "1")
	environment = setEnvironment(environment, "PI_TELEMETRY", "0")
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: cfg.PiExecutable, ExpectedVersion: cfg.ExpectedPiVersion, StrictVersion: cfg.StrictVersion,
		Workspace: input.Workspace, SessionDir: sessionDir, Provider: cfg.Provider.Name, Model: cfg.Model,
		SystemPrompt: cfg.SystemPrompt, Environment: environment,
		ExtensionPaths: extensionPaths, SkillPaths: skillPaths,
		MaxLineBytes: cfg.MaxFrameBytes, MaxStderrBytes: cfg.MaxStderrBytes, InterruptGrace: cfg.InterruptGrace(),
	}), nil
}

// writeModelsConfig translates the compiler-owned provider into Pi's native
// models.json without copying credential values into durable state.
func writeModelsConfig(piHome string, cfg config.Config) error {
	modelConfig := map[string]any{
		"providers": map[string]any{
			cfg.Provider.Name: map[string]any{
				"baseUrl": cfg.Provider.BaseURL,
				"api":     cfg.Provider.API,
				"apiKey":  "$" + cfg.Provider.APIKeyEnv,
				"models": []map[string]any{{
					"id": cfg.Model,
				}},
			},
		},
	}
	contents, err := json.MarshalIndent(modelConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Pi models config: %w", err)
	}
	contents = append(contents, '\n')
	if err := utils.ReplacePrivateFile(filepath.Join(piHome, "models.json"), contents); err != nil {
		return fmt.Errorf("materialize Pi models configuration: %w", err)
	}
	return nil
}

func writeMCPConfig(path string, servers []config.MCPServer) error {
	contents, err := json.MarshalIndent(struct {
		Servers []config.MCPServer `json:"servers"`
	}{Servers: servers}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Pi MCP configuration: %w", err)
	}
	contents = append(contents, '\n')
	if err := utils.ReplacePrivateFile(path, contents); err != nil {
		return fmt.Errorf("materialize Pi MCP configuration: %w", err)
	}
	return nil
}

func hasSelectedSkills(resources agentplugin.Resources) bool {
	if len(resources.Skills) != 0 {
		return true
	}
	for _, bundle := range resources.Plugins {
		if len(bundle.Skills) != 0 {
			return true
		}
	}
	return false
}

func reconcileGeneratedDir(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("generated path %q is a symlink", path)
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func removeGeneratedFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("generated path %q is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("generated path %q is not a regular file", path)
	}
	return os.Remove(path)
}

func setEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}
