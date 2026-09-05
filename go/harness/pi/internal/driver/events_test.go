package driver

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/stretchr/testify/require"
)

type recordingSink struct {
	text        []runtime.TextDelta
	toolCalls   []runtime.ToolCall
	toolResults []runtime.ToolResult
}

func (s *recordingSink) SessionStarted(runtime.SessionStarted) error { return nil }
func (s *recordingSink) TextDelta(event runtime.TextDelta) error {
	s.text = append(s.text, event)
	return nil
}
func (s *recordingSink) ToolCall(event runtime.ToolCall) error {
	s.toolCalls = append(s.toolCalls, event)
	return nil
}
func (s *recordingSink) ToolResult(event runtime.ToolResult) error {
	s.toolResults = append(s.toolResults, event)
	return nil
}

func TestEventTranslatorStreamsTextDelta(t *testing.T) {
	translator := newEventTranslator()
	sink := &recordingSink{}

	outcome, done, err := translator.translate([]byte(`{
		"type":"message_update",
		"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello"}
	}`), sink)

	require.NoError(t, err)
	require.False(t, done)
	require.Nil(t, outcome.Failure)
	require.Equal(t, []runtime.TextDelta{{Text: "hello"}}, sink.text)
}

func TestEventTranslatorCorrelatesToolExecution(t *testing.T) {
	translator := newEventTranslator()
	sink := &recordingSink{}

	_, done, err := translator.translate([]byte(`{
		"type":"tool_execution_start",
		"toolCallId":"call-1",
		"toolName":"bash",
		"args":{"command":"pwd"}
	}`), sink)
	require.NoError(t, err)
	require.False(t, done)
	require.Equal(t, []runtime.ToolCall{{
		ID: "call-1", Name: "bash", Arguments: map[string]any{"command": "pwd"},
	}}, sink.toolCalls)

	_, done, err = translator.translate([]byte(`{
		"type":"tool_execution_end",
		"toolCallId":"call-1",
		"toolName":"bash",
		"result":{"content":[{"type":"text","text":"/data/workspace"}]},
		"isError":false
	}`), sink)
	require.NoError(t, err)
	require.False(t, done)
	require.Len(t, sink.toolResults, 1)
	require.Equal(t, "call-1", sink.toolResults[0].ID)
	require.Equal(t, "bash", sink.toolResults[0].Name)
	require.False(t, sink.toolResults[0].IsError)
}

func TestEventTranslatorRejectsToolCompletionWithoutStart(t *testing.T) {
	translator := newEventTranslator()
	sink := &recordingSink{}

	_, _, err := translator.translate([]byte(`{
		"type":"tool_execution_end",
		"toolCallId":"call-1",
		"toolName":"bash",
		"result":{},
		"isError":false
	}`), sink)

	require.ErrorContains(t, err, "completed without starting")
}

func TestEventTranslatorUsesAgentSettledAsTerminalEvent(t *testing.T) {
	translator := newEventTranslator()
	sink := &recordingSink{}

	_, done, err := translator.translate([]byte(`{"type":"agent_end","messages":[],"willRetry":false}`), sink)
	require.NoError(t, err)
	require.False(t, done)

	outcome, done, err := translator.translate([]byte(`{"type":"agent_settled"}`), sink)
	require.NoError(t, err)
	require.True(t, done)
	require.Nil(t, outcome.Failure)
}

func TestEventTranslatorReportsAssistantFailureAtSettlement(t *testing.T) {
	translator := newEventTranslator()
	sink := &recordingSink{}

	_, done, err := translator.translate([]byte(`{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"rate limited"}}`), sink)
	require.NoError(t, err)
	require.False(t, done)

	outcome, done, err := translator.translate([]byte(`{"type":"agent_settled"}`), sink)
	require.NoError(t, err)
	require.True(t, done)
	require.NotNil(t, outcome.Failure)
	require.Equal(t, "Pi execution failed", outcome.Failure.Message)
}

func TestEventTranslatorClearsRetriedAssistantFailure(t *testing.T) {
	translator := newEventTranslator()
	sink := &recordingSink{}

	_, _, err := translator.translate([]byte(`{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"rate limited"}}`), sink)
	require.NoError(t, err)
	_, _, err = translator.translate([]byte(`{"type":"message_end","message":{"role":"assistant","stopReason":"stop"}}`), sink)
	require.NoError(t, err)

	outcome, done, err := translator.translate([]byte(`{"type":"agent_settled"}`), sink)
	require.NoError(t, err)
	require.True(t, done)
	require.Nil(t, outcome.Failure)
}

func TestEventTranslatorCheckedInTranscripts(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		outcome, sink := replayTranscript(t, "rpc-success.jsonl")
		require.Nil(t, outcome.Failure)
		require.Equal(t, []runtime.TextDelta{{Text: "hello"}}, sink.text)
	})

	t.Run("failure", func(t *testing.T) {
		outcome, _ := replayTranscript(t, "rpc-failure.jsonl")
		require.NotNil(t, outcome.Failure)
		require.Equal(t, "Pi execution failed", outcome.Failure.Message)
	})

	t.Run("tools", func(t *testing.T) {
		outcome, sink := replayTranscript(t, "rpc-tools.jsonl")
		require.Nil(t, outcome.Failure)
		require.Len(t, sink.toolCalls, 1)
		require.Len(t, sink.toolResults, 1)
		require.Equal(t, "call-1", sink.toolCalls[0].ID)
		require.Equal(t, "call-1", sink.toolResults[0].ID)
	})
}

func replayTranscript(t *testing.T, name string) (runtime.Outcome, *recordingSink) {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "testdata", name))
	require.NoError(t, err)
	defer file.Close()

	translator := newEventTranslator()
	sink := &recordingSink{}
	var outcome runtime.Outcome
	terminalEvents := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		translated, done, err := translator.translate(scanner.Bytes(), sink)
		require.NoError(t, err)
		if done {
			outcome = translated
			terminalEvents++
		}
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, 1, terminalEvents)
	return outcome, sink
}
