package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestProcessDriverWaitsForPiAbortResponseOnCancellation(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "pi")
	workspace := filepath.Join(directory, "workspace")
	sessions := filepath.Join(directory, "sessions")
	ready := filepath.Join(directory, "ready")
	abortCapture := filepath.Join(directory, "abort.json")
	abortAck := filepath.Join(directory, "abort-ack")
	for _, path := range []string{workspace, sessions} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "0.85.1"
  exit 0
fi
read state
printf '%s\n' '{"id":"state","type":"response","command":"get_state","success":true,"data":{"sessionFile":"/data/pi/sessions/session-cancel.jsonl","sessionId":"session-cancel"}}'
read prompt
printf '%s\n' '{"id":"prompt","type":"response","command":"prompt","success":true}'
printf ready > "$READY"
read abort
printf '%s\n' "$abort" > "$ABORT_CAPTURE"
sleep 1
printf acknowledged > "$ABORT_ACK"
printf '%s\n' '{"id":"abort","type":"response","command":"abort","success":true}'
sleep 5
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	driver := NewProcessDriver(ProcessConfig{
		Executable: executable, ExpectedVersion: "0.85.1", StrictVersion: true,
		Workspace: workspace, SessionDir: sessions, Provider: "kagent-openai", Model: "gpt-5.4",
		Environment: append(os.Environ(),
			"READY="+ready,
			"ABORT_CAPTURE="+abortCapture,
			"ABORT_ACK="+abortAck,
		),
		MaxLineBytes: 4096, MaxStderrBytes: 1024, InterruptGrace: 2 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := driver.Run(ctx, runtime.Turn{Prompt: "wait"}, &processRecordingSink{})
		result <- err
	}()

	waitForFile(t, ready, 2*time.Second)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Pi cancellation did not complete within the bounded grace period")
	}

	waitForFile(t, abortAck, time.Second)
	raw, err := os.ReadFile(abortCapture)
	if err != nil {
		t.Fatal(err)
	}
	request := string(raw)
	for _, fragment := range []string{`"id":"abort"`, `"type":"abort"`} {
		if !strings.Contains(request, fragment) {
			t.Fatalf("abort request = %q, want %s", request, fragment)
		}
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
