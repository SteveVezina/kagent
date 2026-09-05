# Pi Harness

The Pi Harness runs a `kagent.dev/v1alpha3` `AgentTemplate` through Pi's native
RPC mode while using kagent's existing BYO compiler and private A2A runtime
contract. One Pi RPC process is started for each public A2A Task, and native Pi
sessions plus the workspace are retained in the Actor's durable storage.

This prototype deliberately uses `spec.byo` so the runtime can be proven before
adding a first-class Pi Harness API.

## Code structure

The controller keeps Kubernetes resolution and secret handling. The Pi adapter
receives the compiler-owned BYO `AgentConfig` through `KAGENT_CONFIG_JSON` and
materializes only the native Pi configuration required to preserve those
semantics.

| Path | Look here for |
| --- | --- |
| [`../../core/internal/translator/byo`](../../core/internal/translator/byo) | Existing BYO compiler and resolved runtime revision |
| [`config/config.go`](config/config.go) | Mapping the supported ADK configuration subset to Pi |
| [`cmd/main.go`](cmd/main.go) | Actor startup, Pi version validation, continuation-store wiring, and private A2A startup |
| [`internal/adapter`](internal/adapter) | Durable Pi state and compiler-owned `models.json` materialization |
| [`internal/driver`](internal/driver) | RPC lifecycle, JSONL framing, event translation, exact session resume, cancellation, and process supervision |

## Implemented support

- Pi Coding Agent `0.85.1`, pinned and validated at Actor startup.
- OpenAI and Anthropic through Secret-backed environment credentials resolved by
  kagent's existing BYO compiler.
- OpenAI-compatible gateways through an optional absolute `baseUrl`, materialized
  as a private Pi `models.json` owned by the compiled runtime revision.
- OpenAI Chat Completions and Responses API selection for gateway-backed models.
- Compiler-owned system prompts.
- Streaming assistant text and native tool execution events.
- Exact native Pi session resume through the kagent continuation store.
- Context cancellation through Pi's RPC `abort` command plus bounded process
  group termination.
- Durable Pi session state under `/data/pi/sessions` and workspace state under
  `/data/workspace`.

The prototype fails closed for MCP, Shared/Dedicated agent tools, Agent Plugin
resources, kagent memory/context configuration, custom model headers/TLS, API-key
passthrough, and custom Anthropic endpoints. Those features should be added only
when their semantics can be represented faithfully and covered by conformance
or E2E tests.

Pi's workspace/user resource auto-discovery is disabled for this runtime so
extensions, skills, context files, prompt templates, and themes cannot silently
change compiler-owned behavior.

Runtime configuration is supplied through `KAGENT_CONFIG_JSON` and
`KAGENT_AGENT_CARD_JSON`. Private A2A is served on port 80 and readiness on
`/readyz` at port 8081, matching the other native Harness adapters.

## Development

Build the runtime image with the same repository build flow used by the Codex
and Claude Harnesses:

```text
make build-pi-harness
```

The image installs Pi using the upstream recommended npm path with lifecycle
scripts disabled and runs the adapter as UID/GID `65532`.

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
