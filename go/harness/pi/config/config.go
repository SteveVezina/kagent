// Package config translates the BYO ADK configuration into the subset Pi can
// execute without silently dropping semantics.
package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/api/adk"
)

const (
	PinnedPiVersion     = "0.85.1"
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
)

// Config is the runtime-neutral Pi startup configuration derived from a
// compiler-owned AgentConfig. Credentials remain in the process environment.
type Config struct {
	Provider     string
	Model        string
	SystemPrompt string
	BaseURL      string
	API          string
}

// FromAgentConfig maps the currently supported BYO AgentConfig subset to Pi.
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

	cfg := Config{SystemPrompt: agent.Instruction}
	switch model := agent.Model.(type) {
	case *adk.OpenAI:
		if err := validateBaseModel(model.BaseModel); err != nil {
			return Config{}, err
		}
		if err := validateOpenAITuning(model); err != nil {
			return Config{}, err
		}
		cfg.Provider, cfg.Model = "kagent-openai", strings.TrimSpace(model.Model)
		cfg.BaseURL = strings.TrimSpace(model.BaseUrl)
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultOpenAIBaseURL
		}
		if err := validateHTTPURL(cfg.BaseURL); err != nil {
			return Config{}, fmt.Errorf("OpenAI base URL: %w", err)
		}
		switch strings.TrimSpace(model.APIFormat) {
		case "", "chatCompletions":
			cfg.API = "openai-completions"
		case "responses":
			cfg.API = "openai-responses"
		default:
			return Config{}, fmt.Errorf("Pi BYO prototype does not support OpenAI API format %q", model.APIFormat)
		}
	case *adk.Anthropic:
		if err := validateBaseModel(model.BaseModel); err != nil {
			return Config{}, err
		}
		if err := validateAnthropicTuning(model); err != nil {
			return Config{}, err
		}
		if strings.TrimSpace(model.BaseUrl) != "" {
			return Config{}, fmt.Errorf("Pi BYO prototype does not support a custom Anthropic base URL yet")
		}
		cfg.Provider, cfg.Model = "anthropic", strings.TrimSpace(model.Model)
	default:
		return Config{}, fmt.Errorf("Pi BYO prototype does not support model provider %q yet", agent.Model.GetType())
	}
	if cfg.Model == "" {
		return Config{}, fmt.Errorf("Pi model name is required")
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
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL")
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
