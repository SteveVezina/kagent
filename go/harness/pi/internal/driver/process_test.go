package driver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type processRecordingSink struct {
	sessions []runtime.SessionStarted
	text     bytes.Buffer
}

func (s *processRecordingSink) SessionStarted(event runtime.SessionStarted) error {
	s.sessions = append(s.sessions, event)
	return nil
}
func (s *processRecordingSink) TextDelta(event runtime.TextDelta) error {
	_, _ = s.text.WriteString(event.Text)
	return nil
}
func (s *processRecordingSink) ToolCall(runtime.ToolCall) error     { return nil }
func (s *processRecordingSink) ToolResult(runtime.ToolResult) error { return nil }

func TestProcessDriverRunsRPCProtocol(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "pi")
	capture := filepath.Join(directory, "requests.jsonl")
	argsCapture := filepath.Join(directory, "args.txt")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "0.85.0"
  exit 0
fi
printf '%s\n' "$*" >> "$ARGS_CAPTURE"
read state
printf '%s\n' "$state" >> "$CAPTURE"
printf '%s\n' '{"id":"state","type":"response","command":"get_state","success":true,"data":{"sessionFile":"/data/pi/sessions/session-123.jsonl","sessionId":"session-123"}}'
read prompt
printf '%s\n' "$prompt" >> "$CAPTURE"
printf '%s\n' '{"id":"prompt","type":"response","command":"prompt","success":true}'
printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello"}}'
printf '%s\n' '{"type":"agent_settled"}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(directory, "workspace")
	sessions := filepath.Join(directory, "sessions")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	extension := "/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts"
	skills := "/data/pi/skills"
	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, ExpectedVersion: "0.85.0", StrictVersion: true,
		Workspace: workspace, SessionDir: sessions, Provider: "kagent-openai", Model: "gpt-5.4",
		SystemPrompt: "help", Environment: append(os.Environ(), "CAPTURE="+capture, "ARGS_CAPTURE="+argsCapture),
		ExtensionPaths: []string{extension}, SkillPaths: []string{skills},
		MaxLineBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 100 * time.Millisecond,
	})
	if err := driver.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}

	sink := &processRecordingSink{}
	outcome, err := driver.Run(context.Background(), runtime.Turn{Prompt: "say hello"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Failure != nil || sink.text.String() != "hello" || len(sink.sessions) != 1 || sink.sessions[0].ContinuationID != "session-123" {
		t.Fatalf("outcome = %#v, sessions = %#v, text = %q", outcome, sink.sessions, sink.text.String())
	}
	requests, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"type":"get_state"`, `"type":"prompt"`, `"message":"say hello"`} {
		if !bytes.Contains(requests, []byte(fragment)) {
			t.Errorf("requests omit %s:\n%s", fragment, requests)
		}
	}
	args, err := os.ReadFile(argsCapture)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"--mode rpc", "--offline", "--provider kagent-openai", "--model gpt-5.4", "--system-prompt help",
		"--no-approve", "--no-context-files", "--no-extensions", "-e " + extension,
		"--no-skills", "--skill " + skills, "--no-prompt-templates", "--no-themes",
	} {
		if !bytes.Contains(args, []byte(fragment)) {
			t.Errorf("Pi args omit %q:\n%s", fragment, args)
		}
	}

	resumed := &processRecordingSink{}
	if _, err := driver.Run(context.Background(), runtime.Turn{Prompt: "resume", ContinuationID: "session-123"}, resumed); err != nil {
		t.Fatal(err)
	}
	args, err = os.ReadFile(argsCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(args, []byte("--session session-123")) {
		t.Fatalf("resume did not select the exact Pi session:\n%s", args)
	}
}

func TestProcessDriverWithoutCompiledResourcesOmitsExplicitPaths(t *testing.T) {
	args := processArgs(ProcessConfig{
		Provider: "kagent-openai", Model: "gpt-5.4", SessionDir: "/data/pi/sessions", SystemPrompt: "help",
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, " -e ") || strings.Contains(joined, " --skill ") {
		t.Fatalf("resource-free Pi args unexpectedly load explicit resources: %s", joined)
	}
	if !strings.Contains(joined, "--no-extensions") || !strings.Contains(joined, "--no-skills") {
		t.Fatalf("resource-free Pi args must keep ambient resources disabled: %s", joined)
	}
}

func TestProcessDriverRejectsEmptyPrompt(t *testing.T) {
	driver := NewProcessDriver(ProcessConfig{})
	_, err := driver.Run(context.Background(), runtime.Turn{Prompt: "  "}, &processRecordingSink{})
	if err == nil || !strings.Contains(err.Error(), "Pi prompt is required") {
		t.Fatalf("Run() error = %v", err)
	}
}
