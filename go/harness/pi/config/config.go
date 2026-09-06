// Package config defines the versioned, non-secret Pi Harness runtime
// configuration shared by its compiler and Actor entrypoint.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
)

const (
	Version         = 1
	PinnedPiVersion = "0.85.0"

	PiHomeEnvName           = "PI_CODING_AGENT_DIR"
	MCPConfigEnvName        = "KAGENT_PI_MCP_CONFIG"
	OfflineEnvName          = "PI_OFFLINE"
	SkipVersionCheckEnvName = "PI_SKIP_VERSION_CHECK"
	TelemetryEnvName        = "PI_TELEMETRY"
	OpenAIAPIKeyEnvName     = "OPENAI_API_KEY"
	AnthropicAPIKeyEnvName  = "ANTHROPIC_API_KEY"
	MCPCredentialEnvPrefix  = "KAGENT_PI_MCP_CREDENTIAL_"
)

var nativeNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// OwnsEnvironment reports whether the compiler or adapter reserves name for
// Pi runtime configuration. Harness authors cannot override these values.
func OwnsEnvironment(name string) bool {
	if strings.HasPrefix(name, MCPCredentialEnvPrefix) {
		return true
	}
	switch name {
	case PiHomeEnvName, MCPConfigEnvName, OfflineEnvName, SkipVersionCheckEnvName,
		TelemetryEnvName, OpenAIAPIKeyEnvName, AnthropicAPIKeyEnvName:
		return true
	default:
		return false
	}
}

// Config is compiler-owned input to the native Pi adapter. Credential values
// are supplied only through the process environment.
type Config struct {
	Version              int                    `json:"version"`
	PiExecutable         string                 `json:"pi_executable"`
	ExpectedPiVersion    string                 `json:"expected_pi_version"`
	StrictVersion        bool                   `json:"strict_version"`
	Model                string                 `json:"model"`
	Provider             Provider               `json:"provider"`
	SystemPrompt         string                 `json:"system_prompt,omitempty"`
	SkillResources       *agentplugin.Resources `json:"skill_resources,omitempty"`
	MCPServers           []MCPServer            `json:"mcp_servers,omitempty"`
	MaxFrameBytes        int                    `json:"max_frame_bytes"`
	MaxStderrBytes       int                    `json:"max_stderr_bytes"`
	InterruptGraceMillis int                    `json:"interrupt_grace_millis"`
}

// Provider contains the compiler-owned Pi provider settings required to
// preserve kagent ModelConfig semantics without persisting credential values.
type Provider struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	API       string `json:"api"`
	APIKeyEnv string `json:"api_key_env"`
}

// MCPServer is one compiler-owned direct Streamable HTTP MCP server. Name is
// the sanitized native namespace used in Pi tool names.
type MCPServer struct {
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers,omitempty"`
	EnabledTools   []string          `json:"enabled_tools,omitempty"`
	TimeoutSeconds float64           `json:"timeout_seconds"`
}

// Production returns the pinned runtime defaults used by normal Actor launches.
func Production(provider Provider, model, systemPrompt string) Config {
	return Config{
		Version: Version, PiExecutable: "pi", ExpectedPiVersion: PinnedPiVersion,
		StrictVersion: true, Model: model, Provider: provider, SystemPrompt: systemPrompt,
		MaxFrameBytes: 1 << 20, MaxStderrBytes: 64 << 10, InterruptGraceMillis: 2000,
	}
}

// Parse decodes the native compiler payload using the same strict, versioned
// boundary as the Codex and Claude adapters.
func Parse(data []byte) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, fmt.Errorf("decode config: trailing JSON value")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	servers, err := normalizeMCPServers(cfg.MCPServers)
	if err != nil {
		return Config{}, err
	}
	cfg.MCPServers = servers
	return cfg, nil
}

// Validate enforces the compiler-to-Actor contract independently of the
// controller so corrupted or stale revisions fail before Pi starts.
func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("unsupported config version %d (want %d)", c.Version, Version)
	}
	if strings.TrimSpace(c.PiExecutable) == "" || strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.Provider.Name) == "" {
		return fmt.Errorf("pi executable, model, and provider are required")
	}
	if c.StrictVersion && strings.TrimSpace(c.ExpectedPiVersion) == "" {
		return fmt.Errorf("expected_pi_version is required when strict_version is enabled")
	}
	if c.MaxFrameBytes <= 0 || c.MaxStderrBytes <= 0 || c.InterruptGraceMillis <= 0 {
		return fmt.Errorf("frame, stderr, and interrupt grace limits must be positive")
	}
	if err := validateHTTPURL(c.Provider.BaseURL); err != nil {
		return fmt.Errorf("invalid %s provider base URL: %w", c.Provider.Name, err)
	}
	switch c.Provider.Name {
	case "kagent-openai":
		if c.Provider.API != "openai-completions" && c.Provider.API != "openai-responses" {
			return fmt.Errorf("unsupported Pi OpenAI API %q", c.Provider.API)
		}
		if c.Provider.APIKeyEnv != OpenAIAPIKeyEnvName {
			return fmt.Errorf("kagent-openai must use %s", OpenAIAPIKeyEnvName)
		}
	case "kagent-anthropic":
		if c.Provider.API != "anthropic-messages" {
			return fmt.Errorf("unsupported Pi Anthropic API %q", c.Provider.API)
		}
		if c.Provider.APIKeyEnv != AnthropicAPIKeyEnvName {
			return fmt.Errorf("kagent-anthropic must use %s", AnthropicAPIKeyEnvName)
		}
	default:
		return fmt.Errorf("unsupported Pi provider %q", c.Provider.Name)
	}
	if _, err := normalizeMCPServers(c.MCPServers); err != nil {
		return err
	}
	return nil
}

func normalizeMCPServers(servers []MCPServer) ([]MCPServer, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	result := make([]MCPServer, 0, len(servers))
	seenNames := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		server.Name = strings.TrimSpace(server.Name)
		if !nativeNamePattern.MatchString(server.Name) {
			return nil, fmt.Errorf("Pi MCP server name %q must contain only letters, numbers, underscores, or hyphens", server.Name)
		}
		if _, exists := seenNames[server.Name]; exists {
			return nil, fmt.Errorf("duplicate Pi MCP server name %q", server.Name)
		}
		seenNames[server.Name] = struct{}{}

		server.URL = strings.TrimSpace(server.URL)
		if err := validateHTTPURL(server.URL); err != nil {
			return nil, fmt.Errorf("invalid Pi MCP server %q URL: %w", server.Name, err)
		}
		if server.TimeoutSeconds <= 0 {
			return nil, fmt.Errorf("Pi MCP server %q timeout must be positive", server.Name)
		}
		headers := make(map[string]string, len(server.Headers))
		for name, value := range server.Headers {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("Pi MCP server %q has an empty header name", server.Name)
			}
			headers[name] = value
		}
		server.Headers = headers

		selected := append([]string(nil), server.EnabledTools...)
		seenTools := make(map[string]struct{}, len(selected))
		for _, tool := range selected {
			if strings.TrimSpace(tool) == "" {
				return nil, fmt.Errorf("Pi MCP server %q has an empty enabled tool", server.Name)
			}
			if _, exists := seenTools[tool]; exists {
				return nil, fmt.Errorf("Pi MCP server %q has duplicate enabled tool %q", server.Name, tool)
			}
			seenTools[tool] = struct{}{}
		}
		slices.Sort(selected)
		server.EnabledTools = selected
		result = append(result, server)
	}
	slices.SortFunc(result, func(a, b MCPServer) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func validateHTTPURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return nil
}

// InterruptGrace returns the configured bounded shutdown grace period.
func (c Config) InterruptGrace() time.Duration {
	return time.Duration(c.InterruptGraceMillis) * time.Millisecond
}
