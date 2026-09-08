package driver

import (
	"bytes"
	"strings"
	"testing"
)

func TestRPCClientRejectsOversizedFrame(t *testing.T) {
	client := newRPCClient(&bytes.Buffer{}, strings.NewReader(strings.Repeat("x", 20)+"\n"), 10)
	frame, ok := <-client.frames
	if !ok || frame.err == nil || !strings.Contains(frame.err.Error(), "read Pi RPC stream") {
		t.Fatalf("oversized frame = %#v, open=%t", frame, ok)
	}
}

func TestRPCClientWritesJSONL(t *testing.T) {
	var output bytes.Buffer
	client := newRPCClient(&output, strings.NewReader(""), 1024)
	if err := client.send(map[string]any{"id": "prompt", "type": "prompt", "message": "hello"}); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "{\"id\":\"prompt\",\"message\":\"hello\",\"type\":\"prompt\"}\n"; got != want {
		t.Fatalf("RPC command = %q, want %q", got, want)
	}
}
