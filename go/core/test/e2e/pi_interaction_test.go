package e2e_test

import (
	"context"
	"embed"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

// TestE2EPiMockCheckpointForkAndResume mirrors the native Codex checkpoint
// coverage. The fork must inherit the durable Pi session and continuation state
// from the checkpoint, then resume that exact native session on its next turn.
func TestE2EPiMockCheckpointForkAndResume(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, piInteractionMocks, "mocks/invoke_pi_agent.json"))
	template := createPiMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)

	_, _, first := fixture.send(t, "Return exactly PI_MOCK_FIRST.")
	if first.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("initial Pi task state = %s, want COMPLETED", first.Status.State)
	}

	created, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
		Namespace: "kagent", AgentInstanceId: fixture.instanceID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create Pi checkpoint: %v", err)
	}
	checkpointID := created.GetCheckpoint().GetId()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, cleanupErr := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{
			Namespace: "kagent", CheckpointId: checkpointID,
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete Pi checkpoint: %v", cleanupErr)
		}
	})

	forked, err := fixture.checkpoints.ForkAgentInstance(fixture.ctx, &apiv1alpha1.ForkAgentInstanceRequest{
		Namespace: "kagent", CheckpointId: checkpointID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("fork Pi AgentInstance: %v", err)
	}
	fork := forked.GetAgentInstance()
	if fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("forked Pi AgentInstance state = %s, want READY", fork.GetState())
	}
	forkID := fork.GetId()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, cleanupErr := fixture.instances.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{
			Namespace: "kagent", AgentInstanceId: forkID,
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete forked Pi AgentInstance: %v", cleanupErr)
		}
	})

	forkCtx, forkCancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(),
		"x-user-id", "e2e",
		"x-kagent-agent-instance-namespace", "kagent",
		"x-kagent-agent-instance-id", forkID,
	), 4*time.Minute)
	t.Cleanup(forkCancel)
	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: forkID, PageSize: 10})
	if err != nil {
		t.Fatalf("build forked Pi task list request: %v", err)
	}
	listedResponse, err := fixture.client.ListTasks(forkCtx, listRequest)
	if err != nil {
		t.Fatalf("list forked Pi tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedResponse)
	if err != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ContextID != forkID || listed.Tasks[0].Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("forked Pi tasks = %+v, error %v; want one copied task in context %s", listed, err, forkID)
	}

	forkFixture := &interactionFixture{ctx: forkCtx, client: fixture.client, instanceID: forkID}
	_, _, resumed := forkFixture.send(t, "Return exactly PI_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(resumed), "PI_MOCK_SECOND") {
		t.Fatalf("forked Pi task state = %s, text = %q, want completed resumed response", resumed.Status.State, taskText(resumed))
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
