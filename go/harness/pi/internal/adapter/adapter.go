// Package adapter validates BYO compiler output and materializes compiler-owned Pi state.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/pi/config"
	"github.com/kagent-dev/kagent/go/harness/pi/internal/driver"
)

const piHomeEnv = "PI_CODING_AGENT_DIR"

// Input contains compiler output and Actor-owned locations used to construct
// the Pi driver.
type Input struct {
	ConfigJSON  []byte
	Workspace   string
	DurableDir  string
	Environment []string
}

// New validates the supported BYO config subset, prepares private Pi state,
// materializes compiler-owned native Pi configuration, and constructs the RPC
// process driver.
func New(ctx context.Context, input Input) (*driver.ProcessDriver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var agent adk.AgentConfig
	if err := json.Unmarshal(input.ConfigJSON, &agent); err != nil {
		return nil, fmt.Errorf("decode BYO agent config: %w", err)
	}
	cfg, err := config.FromAgentConfig(&agent)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(input.Workspace) || !filepath.IsAbs(input.DurableDir) {
		return nil, fmt.Errorf("workspace and durable directories must be absolute paths")
	}
	piHome := filepath.Join(input.DurableDir, "pi")
	sessionDir := filepath.Join(piHome, "sessions")
	for _, directory := range []string{input.Workspace, piHome, sessionDir} {
		if err := utils.EnsurePrivateDir(directory); err != nil {
			return nil, fmt.Errorf("prepare Pi directory %q: %w", directory, err)
		}
	}
	if cfg.Provider.Name == "kagent-openai" {
		if err := writeModelsConfig(piHome, cfg); err != nil {
			return nil, err
		}
	}
	environment := append([]string(nil), input.Environment...)
	environment = setEnvironment(environment, piHomeEnv, piHome)
	environment = setEnvironment(environment, "PI_OFFLINE", "1")
	environment = setEnvironment(environment, "PI_SKIP_VERSION_CHECK", "1")
	environment = setEnvironment(environment, "PI_TELEMETRY", "0")
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: cfg.PiExecutable, ExpectedVersion: cfg.ExpectedPiVersion, StrictVersion: cfg.StrictVersion,
		Workspace: input.Workspace, SessionDir: sessionDir, Provider: cfg.Provider.Name, Model: cfg.Model,
		SystemPrompt: cfg.SystemPrompt, Environment: environment,
		MaxLineBytes: cfg.MaxFrameBytes, MaxStderrBytes: cfg.MaxStderrBytes, InterruptGrace: cfg.InterruptGrace(),
	}), nil
}

// writeModelsConfig translates the compiler-owned OpenAI provider into Pi's
// native models.json without copying credential values into durable state.
func writeModelsConfig(piHome string, cfg config.Config) error {
	modelConfig := map[string]any{
		"providers": map[string]any{
			cfg.Provider.Name: map[string]any{
				"baseUrl": cfg.Provider.BaseURL,
				"api":     cfg.Provider.API,
				"apiKey":  "$OPENAI_API_KEY",
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
