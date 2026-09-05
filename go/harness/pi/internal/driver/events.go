package driver

import (
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type eventTranslator struct {
	tools   map[string]string
	failure *runtime.Failure
}

func newEventTranslator() *eventTranslator {
	return &eventTranslator{tools: make(map[string]string)}
}

func (t *eventTranslator) translate(raw []byte, sink runtime.EventSink) (runtime.Outcome, bool, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return runtime.Outcome{}, false, fmt.Errorf("decode Pi event: %w", err)
	}
	switch envelope.Type {
	case "message_update":
		var event struct {
			AssistantMessageEvent struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
			} `json:"assistantMessageEvent"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Pi message update: %w", err)
		}
		if event.AssistantMessageEvent.Type != "text_delta" {
			return runtime.Outcome{}, false, nil
		}
		return runtime.Outcome{}, false, sink.TextDelta(runtime.TextDelta{Text: event.AssistantMessageEvent.Delta})
	case "message_end":
		var event struct {
			Message struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Pi message end: %w", err)
		}
		if event.Message.Role != "assistant" {
			return runtime.Outcome{}, false, nil
		}
		switch event.Message.StopReason {
		case "error":
			t.failure = &runtime.Failure{Message: "Pi execution failed"}
		case "aborted":
			t.failure = &runtime.Failure{Message: "Pi execution was aborted"}
		default:
			t.failure = nil
		}
		return runtime.Outcome{}, false, nil
	case "tool_execution_start":
		var event struct {
			ToolCallID string         `json:"toolCallId"`
			ToolName   string         `json:"toolName"`
			Args       map[string]any `json:"args"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Pi tool start: %w", err)
		}
		if event.ToolCallID == "" || event.ToolName == "" {
			return runtime.Outcome{}, false, fmt.Errorf("Pi tool start requires toolCallId and toolName")
		}
		if _, exists := t.tools[event.ToolCallID]; exists {
			return runtime.Outcome{}, false, fmt.Errorf("Pi tool %q started more than once", event.ToolCallID)
		}
		t.tools[event.ToolCallID] = event.ToolName
		return runtime.Outcome{}, false, sink.ToolCall(runtime.ToolCall{ID: event.ToolCallID, Name: event.ToolName, Arguments: event.Args})
	case "tool_execution_end":
		var event struct {
			ToolCallID string `json:"toolCallId"`
			ToolName   string `json:"toolName"`
			Result     any    `json:"result"`
			IsError    bool   `json:"isError"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return runtime.Outcome{}, false, fmt.Errorf("decode Pi tool end: %w", err)
		}
		startedName, exists := t.tools[event.ToolCallID]
		if !exists {
			return runtime.Outcome{}, false, fmt.Errorf("Pi tool %q completed without starting", event.ToolCallID)
		}
		if startedName != event.ToolName {
			return runtime.Outcome{}, false, fmt.Errorf("Pi tool %q changed name from %q to %q", event.ToolCallID, startedName, event.ToolName)
		}
		delete(t.tools, event.ToolCallID)
		return runtime.Outcome{}, false, sink.ToolResult(runtime.ToolResult{ID: event.ToolCallID, Name: event.ToolName, Result: event.Result, IsError: event.IsError})
	case "agent_settled":
		return runtime.Outcome{Failure: t.failure}, true, nil
	default:
		return runtime.Outcome{}, false, nil
	}
}
