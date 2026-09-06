package pi

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	piconfig "github.com/kagent-dev/kagent/go/harness/pi/config"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const piCredentialValue = "pi-credential-must-not-be-serialized"

func TestCompileSupportedProviders(t *testing.T) {
	chat, responses := v1alpha3.OpenAIAPIFormatChatCompletions, v1alpha3.OpenAIAPIFormatResponses
	tests := []struct {
		name     string
		model    v1alpha3.ModelConfigSpec
		provider piconfig.Provider
		egress   []string
	}{
		{
			name: "OpenAI chat completions",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.1", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}},
			provider: piconfig.Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY"},
			egress: []string{"api.openai.com"},
		},
		{
			name: "OpenAI responses gateway",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.1", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses, BaseURL: "https://gateway.example.com/v1"}},
			provider: piconfig.Provider{Name: "kagent-openai", BaseURL: "https://gateway.example.com/v1", API: "openai-responses", APIKeyEnv: "OPENAI_API_KEY"},
			egress: []string{"gateway.example.com"},
		},
		{
			name: "Anthropic",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet", APIKeySecret: "model-auth", APIKeySecretKey: "api-key"},
			provider: piconfig.Provider{Name: "kagent-anthropic", BaseURL: "https://api.anthropic.com", API: "anthropic-messages", APIKeyEnv: "ANTHROPIC_API_KEY"},
			egress: []string{"api.anthropic.com"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, collections := piTestInput(t, test.model, map[string][]byte{"api-key": []byte(piCredentialValue)})
			result, err := NewCompiler(krt.TestingDummyContext{}, collections).Compile(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := piconfig.Parse(result.ConfigJSON)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Provider, test.provider) || cfg.Model != test.model.Model {
				t.Fatalf("compiled config = %#v", cfg)
			}
			if !reflect.DeepEqual(result.EgressDestinations, test.egress) {
				t.Fatalf("egress = %v, want %v", result.EgressDestinations, test.egress)
			}
			if bytes.Contains(result.ConfigJSON, []byte(piCredentialValue)) || bytes.Contains(result.Provenance, []byte(piCredentialValue)) {
				t.Fatal("credential leaked into immutable Pi revision")
			}
			if len(result.Environment) != 1 || result.Environment[0].Value != piCredentialValue || result.Environment[0].ValueFrom != nil {
				t.Fatalf("resolved environment = %#v", result.Environment)
			}
		})
	}
}

func TestCompileRejectsUnsupportedProviderConfiguration(t *testing.T) {
	chat := v1alpha3.OpenAIAPIFormatChatCompletions
	tests := []v1alpha3.ModelConfigSpec{
		{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", APIKeySecret: "model-auth", APIKeySecretKey: "api-key"},
		{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", APIKeyPassthrough: true, OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}},
		{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat, Temperature: "1"}},
		{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", DefaultHeaders: map[string]string{"x-test": "value"}},
		{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", Anthropic: &v1alpha3.AnthropicConfig{Temperature: "0.5"}},
	}
	for _, model := range tests {
		input, collections := piTestInput(t, model, map[string][]byte{"api-key": []byte("secret")})
		_, err := NewCompiler(krt.TestingDummyContext{}, collections).Compile(context.Background(), input)
		var validation *v2translator.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("Compile(%s) error = %v, want validation", model.Provider, err)
		}
	}
}

func TestCompileRejectsReservedHarnessEnvironment(t *testing.T) {
	chat := v1alpha3.OpenAIAPIFormatChatCompletions
	model := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}}
	input, collections := piTestInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	value := "override"
	input.Harness.Spec.Env = []v1alpha3.HarnessEnvVar{{Name: "OPENAI_API_KEY", Value: &value}}
	if _, err := NewCompiler(krt.TestingDummyContext{}, collections).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "conflicts with Pi") {
		t.Fatalf("reserved environment Compile() error = %v", err)
	}
}

func TestCompileMCPPreservesIdentityAndCredentials(t *testing.T) {
	chat := v1alpha3.OpenAIAPIFormatChatCompletions
	model := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}}
	input, collections := piTestInput(t, model, map[string][]byte{"api-key": []byte("secret"), "mcp-token": []byte(piCredentialValue)})
	timeout := &metav1.Duration{Duration: 45 * time.Second}
	terminate := true
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "math-server", Namespace: "test", UID: "mcp"}, Spec: v1alpha3.RemoteMCPServerSpec{
		Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
		URL: "https://mcp.example.com/mcp",
		Timeout: timeout,
		TerminateOnClose: &terminate,
		HeadersFrom: []v1alpha3.ValueRef{{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "model-auth", Key: "mcp-token"}}},
	}}
	input.Root.MCPTools = []v2translator.ResolvedMCPTool{{Binding: v1alpha3.MCPToolBinding{Tools: []string{"subtract", "add_numbers", "add_numbers"}}, Server: server}}

	result, err := NewCompiler(krt.TestingDummyContext{}, collections).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := piconfig.Parse(result.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "math-server" {
		t.Fatalf("MCP servers = %#v", cfg.MCPServers)
	}
	if !reflect.DeepEqual(cfg.MCPServers[0].EnabledTools, []string{"add_numbers", "subtract"}) || cfg.MCPServers[0].TimeoutSeconds != 45 {
		t.Fatalf("compiled MCP server = %#v", cfg.MCPServers[0])
	}
	if value := cfg.MCPServers[0].Headers["Authorization"]; !strings.HasPrefix(value, "${KAGENT_PI_MCP_CREDENTIAL_") {
		t.Fatalf("MCP Authorization header = %q", value)
	}
	if bytes.Contains(result.ConfigJSON, []byte(piCredentialValue)) || bytes.Contains(result.Provenance, []byte(piCredentialValue)) {
		t.Fatal("MCP credential leaked into immutable Pi revision")
	}
	if !reflect.DeepEqual(result.EgressDestinations, []string{"api.openai.com", "mcp.example.com"}) {
		t.Fatalf("egress = %v", result.EgressDestinations)
	}
}

func TestCompileSkillResources(t *testing.T) {
	chat := v1alpha3.OpenAIAPIFormatChatCompletions
	model := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}}
	input, collections := piTestInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	input.Root.Template.Spec.Skills = []v1alpha3.AgentTemplateSkill{{Name: "review", Source: v1alpha3.ArtifactSource{OCI: "ghcr.io/acme/review@sha256:" + strings.Repeat("b", 64)}}}

	result, err := NewCompiler(krt.TestingDummyContext{}, collections).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := piconfig.Parse(result.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillResources == nil || len(cfg.SkillResources.Skills) != 1 || cfg.SkillResources.Skills[0].Name != "review" {
		t.Fatalf("skill resources = %#v", cfg.SkillResources)
	}
	if !reflect.DeepEqual(result.EgressDestinations, []string{"api.openai.com", "ghcr.io"}) {
		t.Fatalf("egress = %v", result.EgressDestinations)
	}
}

func piTestInput(t *testing.T, modelSpec v1alpha3.ModelConfigSpec, secretData map[string][]byte) (*v2translator.HarnessInput, v2translator.Collections) {
	t.Helper()
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "pi", Namespace: "test", UID: "harness"}, Spec: v1alpha3.HarnessSpec{
		Pi: &v1alpha3.PiHarness{}, Workload: v1alpha3.HarnessWorkload{Image: "example.com/pi@sha256:" + strings.Repeat("a", 64)},
		Substrate: v1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"}},
	}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test", UID: "template"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, Description: "assistant"}}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "test", UID: "model"}, Spec: modelSpec}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test", UID: "secret"}, Data: secretData}
	mock := krttest.NewMock(t, []any{secret})
	collections := v2translator.Collections{Secrets: krttest.GetMockCollection[*corev1.Secret](mock), ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock)}
	return &v2translator.HarnessInput{Harness: harness, Root: &v2translator.AgentInput{Template: template, ResolvedModelConfig: &v2translator.ResolvedModelConfig{Config: model}, Instruction: "help carefully"}}, collections
}
