// Package driver supervises Pi RPC mode and translates its events into the
// runtime-neutral stream consumed by kagent's private A2A executor.
package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// ProcessConfig contains validated Pi process inputs for one Actor.
type ProcessConfig struct {
	Executable      string
	ExpectedVersion string
	StrictVersion   bool
	Workspace       string
	SessionDir      string
	Provider        string
	Model           string
	SystemPrompt    string
	Environment     []string
	MaxLineBytes    int
	MaxStderrBytes  int
	InterruptGrace  time.Duration
}

// ProcessDriver supervises one Pi RPC process per runtime turn.
type ProcessDriver struct{ config ProcessConfig }

// NewProcessDriver constructs a Pi process driver.
func NewProcessDriver(config ProcessConfig) *ProcessDriver { return &ProcessDriver{config: config} }

// Validate checks that the configured executable is the pinned Pi version.
func (d *ProcessDriver) Validate(ctx context.Context) error {
	executable, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return fmt.Errorf("find Pi executable %q: %w", d.config.Executable, err)
	}
	output, err := exec.CommandContext(ctx, executable, "--version").Output()
	if err != nil {
		return fmt.Errorf("read Pi version: %w", err)
	}
	version := strings.TrimSpace(string(output))
	if d.config.StrictVersion && version != d.config.ExpectedVersion {
		return fmt.Errorf("Pi version mismatch: got %q, expected %q", version, d.config.ExpectedVersion)
	}
	return nil
}

// Run starts or resumes a Pi session and emits ordered runtime events until
// Pi reports agent_settled.
func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	if strings.TrimSpace(turn.Prompt) == "" {
		return runtime.Outcome{}, fmt.Errorf("Pi prompt is required")
	}
	executable, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("find Pi executable %q: %w", d.config.Executable, err)
	}
	args := []string{
		"--mode", "rpc",
		"--offline",
		"--provider", d.config.Provider,
		"--model", d.config.Model,
		"--session-dir", d.config.SessionDir,
		"--system-prompt", d.config.SystemPrompt,
		"--no-approve",
		"--no-context-files",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
	}
	if turn.ContinuationID != "" {
		args = append(args, "--session", turn.ContinuationID)
	}
	command := exec.Command(executable, args...)
	command.Dir, command.Env = d.config.Workspace, append([]string(nil), d.config.Environment...)
	utils.ConfigureProcessGroup(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Pi stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return runtime.Outcome{}, fmt.Errorf("open Pi stdout: %w", err)
	}
	stderr := utils.NewBoundedBuffer(d.config.MaxStderrBytes)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return runtime.Outcome{}, fmt.Errorf("start Pi RPC process: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait(); close(wait) }()
	grace := d.config.InterruptGrace
	if grace <= 0 {
		grace = 2 * time.Second
	}
	defer func() {
		_ = stdin.Close()
		_ = utils.TerminateProcessGroup(command.Process)
		select {
		case <-wait:
		case <-time.After(grace):
			_ = utils.KillProcessGroup(command.Process)
			<-wait
		}
	}()

	client := newRPCClient(stdin, stdout, d.config.MaxLineBytes)
	if err := client.send(map[string]any{"id": "state", "type": "get_state"}); err != nil {
		return runtime.Outcome{}, err
	}
	sessionID, err := d.awaitState(ctx, client, wait, stderr)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if turn.ContinuationID != "" && sessionID != turn.ContinuationID {
		return runtime.Outcome{}, fmt.Errorf("Pi resumed unexpected session %q", sessionID)
	}
	if err := sink.SessionStarted(runtime.SessionStarted{ContinuationID: sessionID}); err != nil {
		return runtime.Outcome{}, err
	}
	if err := client.send(map[string]any{"id": "prompt", "type": "prompt", "message": turn.Prompt}); err != nil {
		return runtime.Outcome{}, err
	}
	return d.consume(ctx, client, command, wait, stderr, sink)
}

type responseEnvelope struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

func (d *ProcessDriver) awaitState(ctx context.Context, client *rpcClient, wait <-chan error, stderr *utils.BoundedBuffer) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case waitErr := <-wait:
			return "", d.exitError(waitErr, stderr)
		case frame, ok := <-client.frames:
			if !ok {
				return "", fmt.Errorf("Pi RPC stream closed before get_state response")
			}
			if frame.err != nil {
				return "", frame.err
			}
			var response responseEnvelope
			if err := json.Unmarshal(frame.raw, &response); err != nil {
				return "", fmt.Errorf("decode Pi RPC response: %w", err)
			}
			if response.Type != "response" || response.ID != "state" {
				continue
			}
			if response.Command != "get_state" {
				return "", fmt.Errorf("Pi RPC state response has command %q", response.Command)
			}
			if !response.Success {
				return "", fmt.Errorf("Pi get_state failed: %s", response.Error)
			}
			var state struct {
				SessionID string `json:"sessionId"`
			}
			if err := json.Unmarshal(response.Data, &state); err != nil {
				return "", fmt.Errorf("decode Pi session state: %w", err)
			}
			if strings.TrimSpace(state.SessionID) == "" {
				return "", fmt.Errorf("Pi get_state returned an empty session ID")
			}
			return state.SessionID, nil
		}
	}
}

func (d *ProcessDriver) consume(ctx context.Context, client *rpcClient, command *exec.Cmd, wait <-chan error, stderr *utils.BoundedBuffer, sink runtime.EventSink) (runtime.Outcome, error) {
	translator := newEventTranslator()
	accepted := false
	for {
		select {
		case <-ctx.Done():
			_ = client.send(map[string]any{"id": "abort", "type": "abort"})
			_ = utils.TerminateProcessGroup(command.Process)
			return runtime.Outcome{}, ctx.Err()
		case waitErr := <-wait:
			return runtime.Outcome{}, d.exitError(waitErr, stderr)
		case frame, ok := <-client.frames:
			if !ok {
				return runtime.Outcome{}, fmt.Errorf("Pi RPC stream closed without agent_settled")
			}
			if frame.err != nil {
				return runtime.Outcome{}, frame.err
			}
			var envelope responseEnvelope
			if err := json.Unmarshal(frame.raw, &envelope); err != nil {
				return runtime.Outcome{}, fmt.Errorf("decode Pi RPC frame: %w", err)
			}
			if envelope.Type == "response" {
				if envelope.ID == "prompt" {
					if envelope.Command != "prompt" {
						return runtime.Outcome{}, fmt.Errorf("Pi prompt response has command %q", envelope.Command)
					}
					if !envelope.Success {
						return runtime.Outcome{}, fmt.Errorf("Pi prompt rejected: %s", envelope.Error)
					}
					accepted = true
				}
				continue
			}
			outcome, done, err := translator.translate(frame.raw, sink)
			if err != nil {
				return runtime.Outcome{}, err
			}
			if done {
				if !accepted {
					return runtime.Outcome{}, fmt.Errorf("Pi settled before accepting the prompt")
				}
				return outcome, nil
			}
		}
	}
}

func (d *ProcessDriver) exitError(waitErr error, stderr *utils.BoundedBuffer) error {
	message := strings.TrimSpace(stderr.String())
	if waitErr == nil {
		if message != "" {
			return fmt.Errorf("Pi RPC process exited without a terminal event: %s", message)
		}
		return fmt.Errorf("Pi RPC process exited without a terminal event")
	}
	if message != "" {
		return fmt.Errorf("Pi RPC process exited: %w: %s", waitErr, message)
	}
	return fmt.Errorf("Pi RPC process exited: %w", waitErr)
}
