package e2e_test

import (
	"context"
	"embed"
	"errors"
	"io"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const piE2EHarness = "pi-e2e"

//go:embed mocks/invoke_pi_agent.json
var piInteractionMocks embed.FS

// TestE2EPiMockInteractionResume verifies the same public streaming,
// persistence, and continuation path exercised by the native Codex and Claude
// Harness adapters, while Pi is selected through the existing BYO Harness type
// during the prototype phase.
func TestE2EPiMockInteractionResume(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, piInteractionMocks, "mocks/invoke_pi_agent.json"))
	template := createPiMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)

	streamed := sendPiStreaming(t, fixture, "Return exactly PI_MOCK_FIRST.")
	if streamed.state != a2atype.TaskStateCompleted {
		t.Fatalf("streamed Pi task state = %s, failure = %q, want COMPLETED", streamed.state, streamed.failureText)
	}
	if !streamed.sawWorking || !streamed.sawArtifact {
		t.Fatalf("streamed Pi events: working=%t artifact=%t, want both", streamed.sawWorking, streamed.sawArtifact)
	}
	if !strings.Contains(streamed.text, "PI_MOCK_FIRST") {
		t.Fatalf("streamed Pi response = %q, want PI_MOCK_FIRST", streamed.text)
	}
	first := getPiTask(t, fixture, streamed.taskID)
	if first.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(first), "PI_MOCK_FIRST") {
		t.Fatalf("persisted first Pi task state = %s, text = %q", first.Status.State, taskText(first))
	}

	_, _, resumed := fixture.send(t, "Return exactly PI_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(resumed), "PI_MOCK_SECOND") {
		t.Fatalf("resumed Pi task state = %s, text = %q, want completed PI_MOCK_SECOND", resumed.Status.State, taskText(resumed))
	}
}

type piStreamResult struct {
	taskID      a2atype.TaskID
	state       a2atype.TaskState
	text        string
	sawWorking  bool
	sawArtifact bool
	failureText string
}

func sendPiStreaming(t *testing.T, fixture *interactionFixture, text string) piStreamResult {
	t.Helper()
	_, request := newMessageRequest(t, text)
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming Pi A2A message: %v", err)
	}
	var result piStreamResult
	var output strings.Builder
	terminalEvents := 0
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if terminalEvents != 1 {
				t.Fatalf("Pi stream terminal event count = %d, want 1", terminalEvents)
			}
			if result.taskID == "" {
				t.Fatal("Pi stream completed without a task ID")
			}
			result.text = output.String()
			return result
		}
		if err != nil {
			t.Fatalf("receive Pi task stream: %v", err)
		}
		if terminalEvents != 0 {
			t.Fatalf("Pi stream emitted an event after terminal state %s", result.state)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode Pi task stream: %v", err)
		}
		if info := event.TaskInfo(); info.TaskID != "" {
			result.taskID = info.TaskID
		}
		switch event := event.(type) {
		case *a2atype.Task:
			result.state = event.Status.State
			if event.Status.State == a2atype.TaskStateWorking {
				result.sawWorking = true
			}
		case *a2atype.TaskArtifactUpdateEvent:
			result.sawArtifact = true
			if event.Artifact != nil {
				for _, part := range event.Artifact.Parts {
					output.WriteString(part.Text())
				}
			}
		case *a2atype.TaskStatusUpdateEvent:
			result.state = event.Status.State
			if event.Status.State == a2atype.TaskStateWorking {
				result.sawWorking = true
			}
			if event.Status.State == a2atype.TaskStateFailed && event.Status.Message != nil {
				var parts []string
				for _, part := range event.Status.Message.Parts {
					parts = append(parts, part.Text())
				}
				result.failureText = strings.Join(parts, "\n")
			}
		}
		if result.state.Terminal() {
			terminalEvents++
		}
	}
}

func getPiTask(t *testing.T, fixture *interactionFixture, taskID a2atype.TaskID) *a2atype.Task {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: taskID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	response, err := fixture.client.GetTask(fixture.ctx, request)
	if err != nil {
		t.Fatalf("get Pi task %s: %v", taskID, err)
	}
	task, err := pbconv.FromProtoTask(response)
	if err != nil {
		t.Fatalf("decode Pi task %s: %v", taskID, err)
	}
	return task
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
			Provider:        v1alpha3.ModelProviderOpenAI,
			Model:           "gpt-4.1-mini",
			APIKeySecret:    secret.Name,
			APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI:          &v1alpha3.OpenAIConfig{BaseURL: baseURL, APIFormat: &chatCompletions},
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
