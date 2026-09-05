// Package adapter validates BYO compiler output and constructs the Pi process driver.
package adapter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
// and constructs the RPC process driver.
func New(input Input) (*driver.ProcessDriver, error) {
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
	environment := append([]string(nil), input.Environment...)
	environment = setEnvironment(environment, piHomeEnv, piHome)
	environment = setEnvironment(environment, "PI_SKIP_VERSION_CHECK", "1")
	environment = setEnvironment(environment, "PI_TELEMETRY", "0")
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: "pi", ExpectedVersion: config.PinnedPiVersion, StrictVersion: true,
		Workspace: input.Workspace, SessionDir: sessionDir, Provider: cfg.Provider, Model: cfg.Model,
		SystemPrompt: cfg.SystemPrompt, Environment: environment,
		MaxLineBytes: 1 << 20, MaxStderrBytes: 64 << 10, InterruptGrace: 2 * time.Second,
	}), nil
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
