package e2e_test

import (
	"embed"
	"errors"
	"io"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
)

//go:embed mocks/invoke_pi_builtin_tools.json
var piBuiltinToolMocks embed.FS

func TestE2EPiMockBuiltinToolEvents(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, piBuiltinToolMocks, "mocks/invoke_pi_builtin_tools.json"))
	template := createPiMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)

	streamed := sendPiToolStreaming(t, fixture, "Run the requested shell command.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "PI_BUILTIN_TOOL_DONE") {
		t.Fatalf("built-in tool task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertPiToolEvents(t, streamed.toolEvents, "bash")
	assertPiToolEvents(t, piTaskToolEvents(getPiTask(t, fixture, streamed.taskID)), "bash")
}

type piToolStreamResult struct {
	taskID      a2atype.TaskID
	state       a2atype.TaskState
	text        string
	toolEvents  []piToolEvent
	failureText string
}

type piToolEvent struct {
	partType string
	id       string
	name     string
}

func sendPiToolStreaming(t *testing.T, fixture *interactionFixture, text string) piToolStreamResult {
	t.Helper()
	_, request := newMessageRequest(t, text)
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming Pi tool A2A message: %v", err)
	}
	var result piToolStreamResult
	var output strings.Builder
	terminalEvents := 0
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if terminalEvents != 1 {
				t.Fatalf("Pi tool stream terminal event count = %d, want 1", terminalEvents)
			}
			if result.taskID == "" {
				t.Fatal("Pi tool stream completed without a task ID")
			}
			result.text = output.String()
			return result
		}
		if err != nil {
			t.Fatalf("receive Pi tool task stream: %v", err)
		}
		if terminalEvents != 0 {
			t.Fatalf("Pi tool stream emitted an event after terminal state %s", result.state)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode Pi tool task stream: %v", err)
		}
		if info := event.TaskInfo(); info.TaskID != "" {
			result.taskID = info.TaskID
		}
		switch event := event.(type) {
		case *a2atype.Task:
			result.state = event.Status.State
		case *a2atype.TaskArtifactUpdateEvent:
			if event.Artifact != nil {
				result.toolEvents = append(result.toolEvents, piToolEvents(event.Artifact.Parts)...)
				for _, part := range event.Artifact.Parts {
					output.WriteString(part.Text())
				}
			}
		case *a2atype.TaskStatusUpdateEvent:
			result.state = event.Status.State
			if event.Status.State == a2atype.TaskStateFailed && event.Status.Message != nil {
				var parts []string
				for _, part := range event.Status.Message.Parts {
					parts = append(parts, part.Text())
				}
				result.failureText = strings.Join(parts, "\n")
			}
			if event.Status.Message != nil {
				result.toolEvents = append(result.toolEvents, piToolEvents(event.Status.Message.Parts)...)
			}
		}
		if result.state.Terminal() {
			terminalEvents++
		}
	}
}

func piToolEvents(parts []*a2atype.Part) []piToolEvent {
	var events []piToolEvent
	for _, part := range parts {
		partType, _ := part.Metadata["kagent_type"].(string)
		if partType != "function_call" && partType != "function_response" {
			continue
		}
		data, ok := part.Data().(map[string]any)
		if !ok {
			continue
		}
		id, _ := data["id"].(string)
		name, _ := data["name"].(string)
		events = append(events, piToolEvent{partType: partType, id: id, name: name})
	}
	return events
}

func piTaskToolEvents(task *a2atype.Task) []piToolEvent {
	var events []piToolEvent
	for _, message := range task.History {
		if message != nil {
			events = append(events, piToolEvents(message.Parts)...)
		}
	}
	if task.Status.Message != nil {
		events = append(events, piToolEvents(task.Status.Message.Parts)...)
	}
	for _, artifact := range task.Artifacts {
		if artifact != nil {
			events = append(events, piToolEvents(artifact.Parts)...)
		}
	}
	return events
}

func assertPiToolEvents(t *testing.T, events []piToolEvent, toolName string) {
	t.Helper()
	calls, responses := 0, 0
	ids := map[string]struct{}{}
	for _, event := range events {
		if event.name != toolName {
			continue
		}
		if event.id == "" {
			t.Fatalf("%s event for %s has no tool-use ID", event.partType, toolName)
		}
		switch event.partType {
		case "function_call":
			calls++
			ids[event.id] = struct{}{}
		case "function_response":
			responses++
			if _, ok := ids[event.id]; !ok {
				t.Fatalf("response for %s tool-use ID %q has no preceding call", toolName, event.id)
			}
		}
	}
	if calls != 1 || responses != 1 {
		t.Fatalf("A2A events for %s: calls=%d responses=%d, want one of each; all events=%#v", toolName, calls, responses, events)
	}
}
