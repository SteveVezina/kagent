# Pi Harness

The Pi Harness runs a `kagent.dev/v1alpha3` `AgentTemplate` through Pi's native
RPC mode as a first-class kagent Harness runtime. The controller resolves
Kubernetes resources and credentials into a strict, versioned Pi runtime config;
the Actor consumes that config directly and exposes the same private A2A runtime
contract used by the Codex and Claude Harnesses.

One Pi RPC process is started for each public A2A Task. Native Pi sessions and
the workspace are retained in the Actor's durable storage so tasks can resume
and Substrate checkpoints can fork the runtime state.

## Architecture

```text
Harness.spec.pi
      |
      v
translator/pi.Compiler
      |
      +-- ModelConfig + Secret credentials
      +-- RemoteMCPServer bindings
      +-- AgentTemplate skills/plugins
      +-- egress + provenance
      |
      v
piconfig.Config
      |
      v
KAGENT_CONFIG_JSON
      |
      v
kagent-pi Actor
      |
      +-- models.json
      +-- mcp.json + compiler-owned Pi extension
      +-- materialized skills
      +-- Pi RPC process/session
      |
      v
private A2A
```

The controller owns public API translation. The Actor never reconstructs a
provider configuration from the generic ADK `AgentConfig`; it strictly parses
the versioned Pi config produced by the native compiler.

| Path | Look here for |
| --- | --- |
| [`../../core/internal/translator/pi`](../../core/internal/translator/pi) | Native Pi compiler, provider/MCP validation, credentials, egress and provenance |
| [`config/config.go`](config/config.go) | Versioned compiler-to-Actor contract, runtime-owned environment names and validation |
| [`cmd/main.go`](cmd/main.go) | Actor startup, Pi version validation, continuation-store wiring and private A2A startup |
| [`internal/adapter`](internal/adapter) | Durable Pi state, `models.json`, MCP config and skill materialization |
| [`internal/driver`](internal/driver) | RPC lifecycle, JSONL framing, event translation, resume, cancellation and process supervision |
| [`extensions`](extensions) | Compiler-owned Pi MCP extension and pinned MCP SDK dependency surface |
| [`testdata`](testdata) | Checked-in Pi RPC success, failure and tool-event transcripts |

## Supported model providers

The first native Pi Harness release supports:

- OpenAI Chat Completions.
- OpenAI Responses.
- Anthropic Messages.
- Custom OpenAI and Anthropic base URLs.
- Secret-backed API credentials.

The compiler creates a private Pi `models.json` so arbitrary valid kagent model
IDs do not depend on Pi's built-in model catalog. Credential values remain in
the Actor environment and are never written into the runtime config or
provenance.

Pi currently fails closed for Bedrock, Vertex AI, API-key passthrough, model
sampling/tuning fields, custom model headers and custom model TLS. Those
features should only be added when Pi can preserve the corresponding
`ModelConfig` semantics exactly.

## MCP

Pi supports direct `RemoteMCPServer` bindings over Streamable HTTP.

The native compiler:

- preserves server identity rather than relying on the lossy BYO `AgentConfig`;
- resolves literal and ConfigMap-backed headers;
- represents Secret-backed headers with compiler-owned environment markers;
- validates selected tools, transport, timeout and supported runtime options;
- derives MCP egress and provenance; and
- emits deterministic native server namespaces.

Like the existing native providers, a Kubernetes MCP server name containing a
dot is normalized to `_` by the compiler. Pi's tool surface then follows the
Codex namespace convention by normalizing hyphens in that native server name:

```text
RemoteMCPServer: math-api
MCP tool:        add_numbers
Pi tool:         mcp__math_api__add_numbers
```

The MCP extension calls the upstream server with the original MCP tool name,
so the namespace is only a collision-free LLM/Pi tool identity.

Selected tools are enforced at runtime against `tools/list`. Multiple MCP
servers may therefore expose the same upstream tool name without colliding in
Pi. MCP request timeouts are applied to connection negotiation, `tools/list`
and `tools/call`.

MCP text and image results map to Pi native tool results. MCP application errors
become failed Pi tool executions. Unsupported MCP content fails closed rather
than being silently coerced.

Not yet supported: SSE/stdio MCP, `terminateOnClose=false`, custom MCP TLS,
MCP approval policies and Agent Plugin-declared MCP servers.

## Skills and isolation

Standalone skills and Agent Plugin-selected skills are materialized through
kagent's existing `agentplugins.Materialize` machinery.

Ambient Pi resource discovery remains disabled. The adapter starts Pi with the
resource-discovery flags disabled and explicitly supplies only compiler-owned
extension and skill paths. Context files, prompt templates, themes and ambient
extensions therefore cannot silently change a compiled revision.

Pi also uses offline startup mode to suppress update/package checks and install
telemetry while still allowing configured model-provider and MCP requests.

## Runtime behavior

Implemented runtime behavior includes:

- Pi Coding Agent `0.85.1`, pinned and version-checked.
- Streaming assistant text.
- Native tool call/result events through A2A.
- Exact Pi session resume through the continuation store.
- Bounded cancellation using Pi RPC `abort` before process termination.
- Durable sessions under `/data/pi/sessions`.
- Durable workspace under `/data/workspace`.
- Bounded JSONL frame and stderr handling.
- Strict runtime config parsing with unknown-field rejection.
- Substrate checkpoint/fork resume.

Runtime-owned environment variables are reserved by the Pi config package so a
Harness cannot override compiler or adapter state such as provider credentials,
Pi home, MCP config, offline settings or compiler-generated MCP credential
variables.

Shared/Dedicated AgentTemplate tools and kagent memory/context remain unsupported
in this release and are rejected at compile time.

Runtime configuration is supplied through `KAGENT_CONFIG_JSON` and
`KAGENT_AGENT_CARD_JSON`. Private A2A is served on port 80 and readiness on
`/readyz` at port 8081, matching the other native Harness adapters.

## Development

Build the runtime image with the same repository flow as Codex and Claude:

```text
make build-pi-harness
```

The image uses Node 24, installs the pinned Pi package with npm lifecycle scripts
disabled, validates the Pi CLI during the build and runs the adapter as UID/GID
`65532`.

The image owns the MCP extension dependency tree under
`/usr/local/lib/kagent-pi`, pins the MCP SDK and TypeBox, runs the extension tests
during image construction, removes test files and keeps the extension root-owned
before dropping privileges.

The shared E2E flow builds the Pi image, resolves it to a digest and applies a
native `spec.pi` Harness. Pi coverage exercises public A2A streaming, persisted
tasks, session resume, checkpoint/fork resume, built-in `bash`, Streamable HTTP
MCP, Secret-backed MCP headers, namespaced MCP tool events and cancellation.

## Example

```yaml
apiVersion: kagent.dev/v1alpha3
kind: Harness
metadata:
  name: pi-harness
  namespace: kagent
spec:
  pi: {}
  workload:
    image: ${KAGENT_PI_IMAGE_DIGEST}
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: gs://ate-snapshots/kagent/
  allowedAgentTemplates:
    selector:
      matchLabels:
        runtime: pi
---
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: pi-assistant
  namespace: kagent
  labels:
    runtime: pi
spec:
  description: Pi Harness example
  modelConfig:
    name: default-model-config
  systemPrompt: Reply briefly.
```
