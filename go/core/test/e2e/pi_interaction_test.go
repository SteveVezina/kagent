package e2e_test

import (
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPiBYOAgentInteraction verifies the same public interaction path exercised
// by the built-in Harness adapters while running the real Pi binary behind the
// existing BYO compiler/runtime contract. The second turn verifies native Pi
// session continuation across Actor suspension and wakeup.
func TestPiBYOAgentInteraction(t *testing.T) {
	target := interactionTarget(t)
	template := createPiInteractionTemplate(t, startInteractionMock(t))
	fixture := newInteractionFixtureForHarnessTemplate(t, target, "pi-e2e", template)

	for turn := range 2 {
		_, _, task := fixture.send(t, "What is 2+2?")
		if task.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("Pi A2A task %d state = %s, want COMPLETED", turn+1, task.Status.State)
		}
		if text := taskText(task); !strings.Contains(text, "The answer is 4.") {
			t.Fatalf("Pi A2A response text = %q, want mock LLM response", text)
		}
	}
}

func createPiInteractionTemplate(t *testing.T, modelURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createInteractionModel(t, kube, modelURL, nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pi-interaction-",
			Namespace:    "kagent",
			Labels: map[string]string{
				"kagent.dev/e2e-runtime": "pi",
				"kagent.dev/harness":     "pi-e2e",
			},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Pi Harness interaction E2E fixture",
			SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplate(t, kube, template)
	return template.Name
}
