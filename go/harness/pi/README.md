# Pi Harness

The Pi Harness runs a `kagent.dev/v1alpha3` `AgentTemplate` through Pi's native
RPC mode while using kagent's existing BYO compiler and private A2A runtime
contract. One Pi RPC process is started for each public A2A Task, and native Pi
sessions plus the workspace are retained in the Actor's durable storage.

This prototype deliberately uses `spec.byo` so the runtime can be proven before
adding a first-class Pi Harness API.

## Code structure

The controller keeps Kubernetes resolution and secret handling. During the BYO
prototype phase, the Pi adapter receives the compiler-owned ADK `AgentConfig`
through `KAGENT_CONFIG_JSON`, normalizes the supported subset into the same
versioned runtime-config boundary used by the native Harness adapters, and
materializes only the Pi configuration required to preserve those semantics.

| Path | Look here for |
| --- | --- |
| [`../../core/internal/translator/byo`](../../core/internal/translator/byo) | Existing BYO compiler and resolved runtime revision |
| [`config/config.go`](config/config.go) | Versioned Pi runtime config, strict parsing/validation, pinned defaults, and BYO ADK mapping |
| [`cmd/main.go`](cmd/main.go) | Actor startup, Pi version validation, continuation-store wiring, and private A2A startup |
| [`internal/adapter`](internal/adapter) | Durable Pi state, environment isolation, and compiler-owned `models.json` materialization |
| [`internal/driver`](internal/driver) | RPC lifecycle, JSONL framing, event translation, exact session resume, cancellation, and process supervision |
| [`testdata`](testdata) | Checked-in Pi RPC success, failure, and tool-event transcripts |

A future first-class Pi Harness compiler can emit the versioned Pi runtime
configuration directly without changing the Actor-side driver contract.

## Implemented support

- Pi Coding Agent `0.85.0`, pinned to the currently published npm package and
  validated at image build and Actor startup.
- OpenAI and Anthropic through Secret-backed environment credentials resolved by
  kagent's existing BYO compiler.
- Compiler-owned Pi providers for both OpenAI and Anthropic, so arbitrary valid
  kagent model IDs do not depend on Pi's built-in model catalog.
- Custom OpenAI and Anthropic base URLs materialized into a private Pi
  `models.json` owned by the compiled runtime revision.
- OpenAI Chat Completions and Responses API selection and Anthropic Messages
  selection through compiler-owned provider definitions.
- Compiler-owned system prompts.
- Streaming assistant text and native tool execution events.
- Exact native Pi session resume through the kagent continuation store.
- Context cancellation through Pi's RPC `abort` command plus bounded process
  group termination.
- Durable Pi session state under `/data/pi/sessions` and workspace state under
  `/data/workspace`.
- Bounded RPC frame and stderr handling, strict runtime config parsing, and
  checked-in RPC transcript tests matching the native Harness test style.

The prototype fails closed for MCP, Shared/Dedicated agent tools, Agent Plugin
resources, kagent memory/context configuration, model tuning, custom model
headers/TLS, and API-key passthrough. Those features should be added only when
their semantics can be represented faithfully and covered by conformance or E2E
tests.

Pi's workspace/user resource auto-discovery is disabled for this runtime so
extensions, skills, context files, prompt templates, and themes cannot silently
change compiler-owned behavior. Pi also runs in offline startup mode, which
prevents update checks, package checks, and install telemetry while still
allowing configured model-provider requests.

Runtime configuration is supplied through `KAGENT_CONFIG_JSON` and
`KAGENT_AGENT_CARD_JSON`. Private A2A is served on port 80 and readiness on
`/readyz` at port 8081, matching the other native Harness adapters.

## Development

Build the runtime image with the same repository build flow used by the Codex
and Claude Harnesses:

```text
make build-pi-harness
```

The runtime image uses Node 24, installs an exact Pi package version with npm
lifecycle scripts disabled, verifies `pi --version` and `pi --help` during the
image build, and runs the adapter as UID/GID `65532`.

The shared E2E job builds the Pi image, resolves it to a digest, applies a BYO
Harness fixture, and runs the real Pi binary against kagent's mock OpenAI
endpoint. Pi E2E coverage mirrors the native Harness progression for the
capabilities currently implemented: public A2A streaming, persisted task state,
native Pi session resume, Substrate checkpoint/fork resume, and real built-in
`bash` execution with streamed and persisted A2A tool events.

## BYO example

```yaml
apiVersion: kagent.dev/v1alpha3
kind: Harness
metadata:
  name: pi-harness
  namespace: kagent
spec:
  byo: {}
  workload:
    image: ${KAGENT_PI_IMAGE_DIGEST}
    command: ["/usr/local/bin/kagent-pi"]
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: gs://ate-snapshots/kagent/
  allowedAgentTemplates:
    selector:
      matchLabels:
        kagent.dev/e2e-runtime: pi
---
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: kagent-pi
  namespace: kagent
  labels:
    kagent.dev/e2e-runtime: pi
spec:
  description: Pi Harness example
  modelConfig:
    name: default-model-config
  systemPrompt: Reply briefly.
```
