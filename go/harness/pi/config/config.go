// Package config defines the versioned, non-secret Pi Harness runtime
// configuration consumed by the Actor adapter. During the BYO prototype phase,
// this configuration is derived from the compiler-owned ADK AgentConfig.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/adk"
)

const (
	Version                 = 1
	PinnedPiVersion         = "0.85.0"
	defaultOpenAIBaseURL    = "https://api.openai.com/v1"
	defaultAnthropicBaseURL = "https://api.anthropic.com"
)

// Config is the versioned, runtime-neutral Pi startup configuration.
// Credential values remain in the process environment.
type Config struct {
	Version              int      `json:"version"`
	PiExecutable         string   `json:"pi_executable"`
	ExpectedPiVersion    string   `json:"expected_pi_version"`
	StrictVersion        bool     `json:"strict_version"`
	Model                string   `json:"model"`
	Provider             Provider `json:"provider"`
	SystemPrompt         string   `json:"system_prompt,omitempty"`
	MaxFrameBytes        int      `json:"max_frame_bytes"`
	MaxStderrBytes       int      `json:"max_stderr_bytes"`
	InterruptGraceMillis int      `json:"interrupt_grace_millis"`
}

// Provider contains the compiler-owned Pi provider settings required to
// preserve kagent ModelConfig semantics without persisting credential values.
type Provider struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	API       string `json:"api"`
	APIKeyEnv string `json:"api_key_env"`
}

// Production returns the pinned runtime defaults used by normal Actor launches.
func Production(provider Provider, model, systemPrompt string) Config {
	return Config{
		Version: Version, PiExecutable: "pi", ExpectedPiVersion: PinnedPiVersion,
		StrictVersion: true, Model: model, Provider: provider, SystemPrompt: systemPrompt,
		MaxFrameBytes: 1 << 20, MaxStderrBytes: 64 << 10, InterruptGraceMillis: 2000,
	}
}

// Parse decodes a future first-class Pi compiler payload using the same strict,
// versioned boundary as the Codex and Claude adapters.
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
	return cfg, nil
}

// Validate enforces the runtime contract independently of the outer Harness
// compiler. This makes the BYO prototype ready to consume a typed Pi compiler
// payload later without changing the Actor-side contract.
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
		if c.Provider.APIKeyEnv != "OPENAI_API_KEY" {
			return fmt.Errorf("kagent-openai must use OPENAI_API_KEY")
		}
	case "kagent-anthropic":
		if c.Provider.API != "anthropic-messages" {
			return fmt.Errorf("unsupported Pi Anthropic API %q", c.Provider.API)
		}
		if c.Provider.APIKeyEnv != "ANTHROPIC_API_KEY" {
			return fmt.Errorf("kagent-anthropic must use ANTHROPIC_API_KEY")
		}
	default:
		return fmt.Errorf("unsupported Pi provider %q", c.Provider.Name)
	}
	return nil
}

// InterruptGrace returns the configured bounded shutdown grace period.
func (c Config) InterruptGrace() time.Duration {
	return time.Duration(c.InterruptGraceMillis) * time.Millisecond
}

// FromAgentConfig maps the currently supported BYO AgentConfig subset into the
// same versioned Pi runtime config a future first-class compiler can emit.
func FromAgentConfig(agent *adk.AgentConfig) (Config, error) {
	if agent == nil || agent.Model == nil {
		return Config{}, fmt.Errorf("Pi model is required")
	}
	if len(agent.HttpTools) > 0 || len(agent.SseTools) > 0 || len(agent.StdioTools) > 0 {
		return Config{}, fmt.Errorf("Pi BYO prototype does not support MCP tools yet")
	}
	if len(agent.RemoteAgents) > 0 || len(agent.SubAgents) > 0 {
		return Config{}, fmt.Errorf("Pi BYO prototype does not support agent tools yet")
	}
	if agent.AgentPlugins != nil {
		return Config{}, fmt.Errorf("Pi BYO prototype does not support Agent Plugin resources yet")
	}
	if agent.Memory != nil || agent.ContextConfig != nil {
		return Config{}, fmt.Errorf("Pi BYO prototype does not support kagent memory or context configuration yet")
	}

	var cfg Config
	switch model := agent.Model.(type) {
	case *adk.OpenAI:
		if err := validateBaseModel(model.BaseModel); err != nil {
			return Config{}, err
		}
		if err := validateOpenAITuning(model); err != nil {
			return Config{}, err
		}
		baseURL := strings.TrimSpace(model.BaseUrl)
		if baseURL == "" {
			baseURL = defaultOpenAIBaseURL
		}
		if err := validateHTTPURL(baseURL); err != nil {
			return Config{}, fmt.Errorf("OpenAI base URL: %w", err)
		}
		provider := Provider{Name: "kagent-openai", BaseURL: baseURL, APIKeyEnv: "OPENAI_API_KEY"}
		switch strings.TrimSpace(model.APIFormat) {
		case "", "chatCompletions":
			provider.API = "openai-completions"
		case "responses":
			provider.API = "openai-responses"
		default:
			return Config{}, fmt.Errorf("Pi BYO prototype does not support OpenAI API format %q", model.APIFormat)
		}
		cfg = Production(provider, strings.TrimSpace(model.Model), agent.Instruction)
	case *adk.Anthropic:
		if err := validateBaseModel(model.BaseModel); err != nil {
			return Config{}, err
		}
		if err := validateAnthropicTuning(model); err != nil {
			return Config{}, err
		}
		baseURL := strings.TrimSpace(model.BaseUrl)
		if baseURL == "" {
			baseURL = defaultAnthropicBaseURL
		}
		if err := validateHTTPURL(baseURL); err != nil {
			return Config{}, fmt.Errorf("Anthropic base URL: %w", err)
		}
		cfg = Production(Provider{
			Name: "kagent-anthropic", BaseURL: baseURL, API: "anthropic-messages", APIKeyEnv: "ANTHROPIC_API_KEY",
		}, strings.TrimSpace(model.Model), agent.Instruction)
	default:
		return Config{}, fmt.Errorf("Pi BYO prototype does not support model provider %q yet", agent.Model.GetType())
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateOpenAITuning(model *adk.OpenAI) error {
	if model.FrequencyPenalty != nil || model.MaxTokens != nil || model.MaxCompletionTokens != nil ||
		model.N != nil || model.PresencePenalty != nil || model.ReasoningEffort != nil || model.Seed != nil ||
		model.Temperature != nil || model.Timeout != nil || model.TopP != nil || model.TokenExchange != nil {
		return fmt.Errorf("Pi BYO prototype does not support OpenAI model tuning yet")
	}
	return nil
}

func validateAnthropicTuning(model *adk.Anthropic) error {
	if model.MaxTokens != nil || model.Temperature != nil || model.TopP != nil || model.TopK != nil || model.Timeout != nil {
		return fmt.Errorf("Pi BYO prototype does not support Anthropic model tuning yet")
	}
	return nil
}

func validateHTTPURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return nil
}

func validateBaseModel(model adk.BaseModel) error {
	if model.APIKeyPassthrough {
		return fmt.Errorf("Pi BYO prototype does not support API key passthrough yet")
	}
	if len(model.Headers) > 0 {
		return fmt.Errorf("Pi BYO prototype does not support custom model headers yet")
	}
	if model.TLSInsecureSkipVerify != nil || model.TLSCACertPath != nil || model.TLSDisableSystemCAs != nil {
		return fmt.Errorf("Pi BYO prototype does not support custom model TLS configuration yet")
	}
	return nil
}
