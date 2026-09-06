// Package pi compiles resolved v1alpha3 inputs for the native Pi Harness adapter.
package pi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	piconfig "github.com/kagent-dev/kagent/go/harness/pi/config"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	openAIAPIKeyEnv     = "OPENAI_API_KEY"
	anthropicAPIKeyEnv  = "ANTHROPIC_API_KEY"
	mcpCredentialPrefix = "KAGENT_PI_MCP_CREDENTIAL_"
	openAIBaseURL       = "https://api.openai.com/v1"
	anthropicBaseURL    = "https://api.anthropic.com"
)

var ownedEnvironment = map[string]struct{}{
	openAIAPIKeyEnv:    {},
	anthropicAPIKeyEnv: {},
}

type Compiler struct {
	ctx         krt.HandlerContext
	collections v2translator.Collections
}

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{ctx: ctx, collections: collections}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.CompileResult, error) {
	if input == nil || input.Harness == nil || input.Root == nil || input.Root.Template == nil || input.Root.ResolvedModelConfig == nil || input.Root.ResolvedModelConfig.Config == nil {
		return nil, fmt.Errorf("pi compiler requires a resolved Harness, AgentTemplate, and ModelConfig")
	}
	if len(input.Root.Shared) != 0 {
		return nil, v2translator.NewValidationError("Pi does not support Shared AgentTemplate tools yet")
	}

	model := input.Root.ResolvedModelConfig.Config
	if strings.TrimSpace(model.Spec.Model) == "" {
		return nil, v2translator.NewValidationError("Pi ModelConfig model is required")
	}
	if len(model.Spec.DefaultHeaders) != 0 || !model.Spec.TLS.IsEmpty() || model.Spec.APIKeyPassthrough {
		return nil, v2translator.NewValidationError("Pi does not support ModelConfig defaultHeaders, TLS, or apiKeyPassthrough")
	}

	provider, providerEnvironment, egress, err := c.compileProvider(ctx, model)
	if err != nil {
		return nil, err
	}
	skillResources, skillEgress, err := v2translator.CompileSkillResources(input.Root.Template)
	if err != nil {
		return nil, err
	}
	mcp, err := c.compileMCP(ctx, input.Root.Template.Namespace, input.Root.MCPTools)
	if err != nil {
		return nil, err
	}

	environment := append([]corev1.EnvVar(nil), providerEnvironment...)
	environment = append(environment, mcp.environment...)
	for _, variable := range input.Harness.Spec.Env {
		if _, reserved := ownedEnvironment[variable.Name]; reserved || strings.HasPrefix(variable.Name, mcpCredentialPrefix) {
			return nil, v2translator.NewValidationError("Harness env %q conflicts with Pi compiled configuration", variable.Name)
		}
		envVar := corev1.EnvVar{Name: variable.Name}
		if variable.Value != nil {
			envVar.Value = *variable.Value
		} else if variable.CredentialRef != nil {
			envVar.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: variable.CredentialRef.DeepCopy()}
		} else {
			return nil, v2translator.NewValidationError("Harness env %q requires a value or credentialRef", variable.Name)
		}
		environment = append(environment, envVar)
	}

	cfg := piconfig.Production(provider, strings.TrimSpace(model.Spec.Model), input.Root.Instruction)
	if len(skillResources.Skills) != 0 || len(skillResources.Plugins) != 0 {
		cfg.SkillResources = &skillResources
	}
	cfg.MCPServers = mcp.servers
	if err := cfg.Validate(); err != nil {
		return nil, v2translator.NewValidationError("invalid compiled Pi configuration: %v", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal Pi config: %w", err)
	}
	cardJSON, err := json.Marshal(agentTemplateCard(input.Root.Template))
	if err != nil {
		return nil, fmt.Errorf("marshal Pi agent card: %w", err)
	}
	provenance, err := c.buildProvenance(ctx, input, environment, configJSON, cardJSON)
	if err != nil {
		return nil, fmt.Errorf("build Pi revision provenance: %w", err)
	}
	environment, err = c.resolveEnvironment(ctx, input.Harness.Namespace, environment)
	if err != nil {
		return nil, fmt.Errorf("resolve Pi runtime environment: %w", err)
	}

	egress = append(egress, skillEgress...)
	egress = append(egress, mcp.egress...)
	slices.Sort(egress)
	egress = slices.Compact(egress)
	template, harness := input.Root.Template, input.Harness
	return &v2translator.CompileResult{Revision: v2translator.Revision{
		Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
		Image: harness.Spec.Workload.Image, Environment: environment,
		ConfigJSON: configJSON, AgentCardJSON: cardJSON,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Provenance: provenance, EgressDestinations: egress,
	}}, nil
}

func (c *Compiler) compileProvider(ctx context.Context, model *v1alpha3.ModelConfig) (piconfig.Provider, []corev1.EnvVar, []string, error) {
	switch model.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		options := v1alpha3.OpenAIConfig{}
		if model.Spec.OpenAI != nil {
			options = *model.Spec.OpenAI
		}
		baseURL := strings.TrimSpace(options.BaseURL)
		if baseURL == "" {
			baseURL = openAIBaseURL
		}
		api := "openai-completions"
		if options.APIFormat != nil {
			switch *options.APIFormat {
			case v1alpha3.OpenAIAPIFormatChatCompletions:
				api = "openai-completions"
			case v1alpha3.OpenAIAPIFormatResponses:
				api = "openai-responses"
			default:
				return piconfig.Provider{}, nil, nil, v2translator.NewValidationError("Pi does not support OpenAI API format %q", *options.APIFormat)
			}
		}
		options.BaseURL, options.APIFormat = "", nil
		if !reflect.DeepEqual(options, v1alpha3.OpenAIConfig{}) {
			return piconfig.Provider{}, nil, nil, v2translator.NewValidationError("Pi does not support OpenAI provider options beyond baseUrl and apiFormat")
		}
		if err := c.requireSecretKey(ctx, model.Namespace, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey); err != nil {
			return piconfig.Provider{}, nil, nil, err
		}
		host, err := absoluteHTTPHostname(baseURL)
		if err != nil {
			return piconfig.Provider{}, nil, nil, v2translator.NewValidationError("Pi OpenAI baseUrl %v", err)
		}
		return piconfig.Provider{Name: "kagent-openai", BaseURL: baseURL, API: api, APIKeyEnv: openAIAPIKeyEnv}, []corev1.EnvVar{
			secretEnvironment(openAIAPIKeyEnv, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey),
		}, []string{host}, nil

	case v1alpha3.ModelProviderAnthropic:
		options := v1alpha3.AnthropicConfig{}
		if model.Spec.Anthropic != nil {
			options = *model.Spec.Anthropic
		}
		baseURL := strings.TrimSpace(options.BaseURL)
		if baseURL == "" {
			baseURL = anthropicBaseURL
		}
		options.BaseURL = ""
		if !reflect.DeepEqual(options, v1alpha3.AnthropicConfig{}) {
			return piconfig.Provider{}, nil, nil, v2translator.NewValidationError("Pi does not support Anthropic provider options beyond baseUrl")
		}
		if err := c.requireSecretKey(ctx, model.Namespace, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey); err != nil {
			return piconfig.Provider{}, nil, nil, err
		}
		host, err := absoluteHTTPHostname(baseURL)
		if err != nil {
			return piconfig.Provider{}, nil, nil, v2translator.NewValidationError("Pi Anthropic baseUrl %v", err)
		}
		return piconfig.Provider{Name: "kagent-anthropic", BaseURL: baseURL, API: "anthropic-messages", APIKeyEnv: anthropicAPIKeyEnv}, []corev1.EnvVar{
			secretEnvironment(anthropicAPIKeyEnv, model.Spec.APIKeySecret, model.Spec.APIKeySecretKey),
		}, []string{host}, nil

	default:
		return piconfig.Provider{}, nil, nil, v2translator.NewValidationError("Pi does not support ModelConfig provider %q", model.Spec.Provider)
	}
}

func (c *Compiler) requireSecretKey(ctx context.Context, namespace, name, key string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(key) == "" {
		return v2translator.NewValidationError("Pi provider requires apiKeySecret and apiKeySecretKey")
	}
	secret, err := c.secret(ctx, namespace, name)
	if err != nil {
		return err
	}
	if len(secret.Data[key]) == 0 {
		return v2translator.NewValidationError("Pi credential Secret %q does not contain a non-empty key %q", name, key)
	}
	return nil
}

func (c *Compiler) secret(_ context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := krt.FetchOne(c.ctx, c.collections.Secrets, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: name}))
	if secret == nil {
		return nil, fmt.Errorf("read Pi credential Secret %q: not found", name)
	}
	return *secret, nil
}

func secretEnvironment(environmentName, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: environmentName, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key,
	}}}
}

func absoluteHTTPHostname(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return parsed.Hostname(), nil
}

type provenanceEntry struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Key        string    `json:"key,omitempty"`
	UID        types.UID `json:"uid"`
	Generation int64     `json:"generation,omitempty"`
	Hash       string    `json:"hash"`
}

func (c *Compiler) buildProvenance(ctx context.Context, input *v2translator.HarnessInput, environment []corev1.EnvVar, configJSON, cardJSON []byte) ([]byte, error) {
	entries := []provenanceEntry{
		objectProvenance(v1alpha3.GroupVersion.String(), "Harness", input.Harness.Name, input.Harness.UID, input.Harness.Generation, input.Harness.Spec),
		objectProvenance("kagent.internal/v1", "GeneratedInput", "config.json", "", 0, json.RawMessage(configJSON)),
		objectProvenance("kagent.internal/v1", "GeneratedInput", "agent-card.json", "", 0, json.RawMessage(cardJSON)),
		objectProvenance(v1alpha3.GroupVersion.String(), "AgentTemplate", input.Root.Template.Name, input.Root.Template.UID, input.Root.Template.Generation, input.Root.Template.Spec),
		objectProvenance(v1alpha3.GroupVersion.String(), "ModelConfig", input.Root.ResolvedModelConfig.Config.Name, input.Root.ResolvedModelConfig.Config.UID, input.Root.ResolvedModelConfig.Config.Generation, input.Root.ResolvedModelConfig.Config.Spec),
	}

	configMaps := map[string]struct{}{}
	if input.Root.Template.Spec.SystemPromptFrom != nil {
		configMaps[input.Root.Template.Spec.SystemPromptFrom.Name] = struct{}{}
	}
	if input.Root.Template.Spec.PromptTemplate != nil {
		for _, source := range input.Root.Template.Spec.PromptTemplate.DataSources {
			configMaps[source.Name] = struct{}{}
		}
	}
	seenMCP := map[string]struct{}{}
	for _, tool := range input.Root.MCPTools {
		if tool.Server == nil {
			continue
		}
		if _, seen := seenMCP[tool.Server.Name]; !seen {
			seenMCP[tool.Server.Name] = struct{}{}
			entries = append(entries, objectProvenance(v1alpha3.GroupVersion.String(), "RemoteMCPServer", tool.Server.Name, tool.Server.UID, tool.Server.Generation, tool.Server.Spec))
		}
		for _, header := range tool.Server.Spec.HeadersFrom {
			if header.ValueFrom != nil && header.ValueFrom.Type == v1alpha3.ConfigMapValueSource {
				configMaps[header.ValueFrom.Name] = struct{}{}
			}
	}
	}
	for name := range configMaps {
		configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(types.NamespacedName{Namespace: input.Harness.Namespace, Name: name}))
		if configMap == nil {
			return nil, fmt.Errorf("ConfigMap %q not found", name)
		}
		entries = append(entries, objectProvenance("v1", "ConfigMap", name, (*configMap).UID, (*configMap).Generation, (*configMap).Data))
	}

	seenSecrets := map[string]struct{}{}
	for _, variable := range environment {
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		identity := ref.Name + "\x00" + ref.Key
		if _, seen := seenSecrets[identity]; seen {
			continue
		}
		seenSecrets[identity] = struct{}{}
		secret, err := c.secret(ctx, input.Harness.Namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		hash := sha256.Sum256(value)
		entries = append(entries, provenanceEntry{APIVersion: "v1", Kind: "Secret", Name: ref.Name, Key: ref.Key, UID: secret.UID, Hash: fmt.Sprintf("%x", hash[:])})
	}
	slices.SortFunc(entries, func(a, b provenanceEntry) int {
		return strings.Compare(a.APIVersion+"\x00"+a.Kind+"\x00"+a.Name+"\x00"+a.Key, b.APIVersion+"\x00"+b.Kind+"\x00"+b.Name+"\x00"+b.Key)
	})
	return json.Marshal(entries)
}

func objectProvenance(apiVersion, kind, name string, uid types.UID, generation int64, content any) provenanceEntry {
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return provenanceEntry{APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Generation: generation, Hash: fmt.Sprintf("%x", hash[:])}
}

func (c *Compiler) resolveEnvironment(ctx context.Context, namespace string, environment []corev1.EnvVar) ([]corev1.EnvVar, error) {
	resolved := append([]corev1.EnvVar(nil), environment...)
	for i, variable := range resolved {
		if variable.ValueFrom == nil {
			continue
		}
		if variable.ValueFrom.SecretKeyRef == nil {
			return nil, fmt.Errorf("environment variable %q uses an unsupported value source", variable.Name)
		}
		ref := variable.ValueFrom.SecretKeyRef
		secret, err := c.secret(ctx, namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q does not contain key %q", ref.Name, ref.Key)
		}
		resolved[i].Value, resolved[i].ValueFrom = string(value), nil
	}
	return resolved, nil
}

func agentTemplateCard(template *v1alpha3.AgentTemplate) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name: strings.ReplaceAll(template.Name, "-", "_"), Description: template.Spec.Description, Version: "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{URL: "http://127.0.0.1:80", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version}},
		Capabilities: a2atype.AgentCapabilities{Streaming: true}, Skills: []a2atype.AgentSkill{},
		DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
}

var _ v2translator.HarnessCompiler = (*Compiler)(nil)
