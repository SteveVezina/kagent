package e2e_test

import (
	"context"
	"embed"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const piE2EHarness = "pi-e2e"

//go:embed mocks/invoke_pi_agent.json
var piInteractionMocks embed.FS

// TestE2EPiMockInteractionResume verifies the same public interaction and
// continuation path exercised by the native Codex and Claude Harness adapters,
// while Pi is selected through the existing BYO Harness type during the
// prototype phase.
func TestE2EPiMockInteractionResume(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, piInteractionMocks, "mocks/invoke_pi_agent.json"))
	template := createPiMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)

	_, _, first := fixture.send(t, "Return exactly PI_MOCK_FIRST.")
	if first.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(first), "PI_MOCK_FIRST") {
		t.Fatalf("first Pi task state = %s, text = %q, want completed PI_MOCK_FIRST", first.Status.State, taskText(first))
	}

	_, _, resumed := fixture.send(t, "Return exactly PI_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(resumed), "PI_MOCK_SECOND") {
		t.Fatalf("resumed Pi task state = %s, text = %q, want completed PI_MOCK_SECOND", resumed.Status.State, taskText(resumed))
	}
}

func createPiMockTemplate(t *testing.T, baseURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createPiMockModel(t, kube, baseURL)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pi-interaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "pi"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Pi mockLLM interaction fixture",
			SystemPrompt: "Reply concisely and follow the requested output format exactly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, piE2EHarness)
	return template.Name
}

func createPiMockModel(t *testing.T, kube ctrlclient.Client, baseURL string) *v1alpha3.ModelConfig {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "pi-mock-", Namespace: "kagent"},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("mock-key")},
	}
	if err := kube.Create(t.Context(), secret); err != nil {
		t.Fatalf("create Pi mock Secret: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), secret); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Pi mock Secret: %v", err)
		}
	})

	chatCompletions := v1alpha3.OpenAIAPIFormatChatCompletions
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "pi-mock-", Namespace: "kagent"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider:     v1alpha3.ModelProviderOpenAI,
			Model:        "gpt-4.1-mini",
			APIKeySecret: secret.Name,
			APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI: &v1alpha3.OpenAIConfig{BaseURL: baseURL, APIFormat: &chatCompletions},
		},
	}
	if err := kube.Create(t.Context(), model); err != nil {
		t.Fatalf("create Pi mock ModelConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), model); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Pi mock ModelConfig: %v", err)
		}
	})
	return model
}
