# [FEATURE] Add Pi Coding Agent as a native Harness runtime

### 📋 Prerequisites

- [x] I have searched the existing issues to avoid creating a duplicate
- [x] By submitting this issue, I agree to follow the Code of Conduct

### 📝 Feature Summary

Add [Pi Coding Agent](https://github.com/badlogic/pi-mono) as a first-class native kagent Harness, alongside the existing Claude Code and Codex Harnesses.

Pi is an open-source coding-agent CLI with a machine-facing RPC mode. It fits the same class of workload that motivated the native Claude and Codex Harnesses: long-running, tool-using coding agents that benefit from Substrate isolation, durable sessions, checkpointing, MCP, skills, and kagent's A2A lifecycle.

This issue proposes integrating Pi through the existing Harness architecture rather than introducing another runtime abstraction.

### ❓ Problem Statement / Motivation

The Claude runtime discussion in #1676 evolved from BYO / protocol-adapter approaches toward a first-class runtime managed by kagent. That direction was later implemented by:

- #2602 - initial native Claude Code Harness
- #2645 - initial native Codex Harness, explicitly reusing the Claude Harness plumbing
- #2366 - the Harness / AgentTemplate architecture that now defines the runtime boundary

Pi has the same fundamental integration shape.

Today Pi can be run as a BYO image, but that leaves important runtime responsibilities outside the native Harness compiler:

- provider and credential translation
- MCP server identity and selected-tool configuration
- skill/plugin materialization
- egress derivation
- immutable revision provenance
- compile-time compatibility validation
- runtime capability reporting
- standard image/build/release integration

BYO is useful as a compatibility boundary and proof-of-concept, but it should not be necessary for a runtime that kagent intentionally supports.

### 💡 Proposed Solution

Add Pi as another native CLI Harness using the same architecture established by Claude and Codex.

```text
Harness.spec.pi
       │
       ▼
resolved AgentTemplate / ModelConfig / tools
       │
       ▼
Pi compiler
       │
       ▼
versioned Pi runtime config
       │
       ▼
Pi Harness Actor
       │
       ▼
pi --mode rpc
       │
       ▼
private A2A runtime
```

#### Runtime

Add a native `go/harness/pi` runtime that supervises the official Pi CLI through:

```text
pi --mode rpc
```

Pi RPC uses JSONL over stdin/stdout and exposes the primitives needed by the Harness runtime, including prompts, session state, cancellation, streaming message events, and tool execution events.

The adapter should follow the existing native Harness pattern:

- pinned Pi CLI version
- versioned compiler-to-runtime configuration
- non-root runtime container
- `/data` durable state
- shared private A2A runtime
- readiness endpoint
- durable continuation/session state
- bounded cancellation
- streaming text and tool lifecycle translation
- Substrate Actor as the sandbox/security boundary

Pi's ambient extensions, skills, context files, prompt templates and similar auto-discovered resources should be disabled. Only resources explicitly compiled by kagent should be loaded.

#### Native Harness API and compiler

Add:

```yaml
spec:
  pi: {}
```

as another typed `Harness` runtime variant, consistent with `spec.claude` and `spec.codex`.

The Pi compiler should consume the same resolved Harness input used by the other native runtimes and own:

- model/provider translation
- credential environment references
- prompt/config compilation
- MCP compilation
- skill/plugin resources
- egress destinations
- revision provenance
- runtime compatibility validation

Unsupported Pi/provider semantics should fail during compilation rather than being silently ignored.

#### Initial model-provider support

For the initial integration:

- OpenAI Chat Completions
- OpenAI Responses
- Anthropic Messages
- OpenAI-compatible gateways through custom base URLs
- Anthropic-compatible gateways through custom base URLs

Credentials remain Kubernetes Secret-backed runtime environment values and must never be serialized into immutable runtime config or revision provenance.

Additional providers such as Amazon Bedrock can be added independently once their Pi behavior is validated.

#### MCP

Support `RemoteMCPServer` using Streamable HTTP.

The compiler should preserve MCP server identity and expose deterministic namespaced Pi tools, similar to the existing native Harness convention:

```text
RemoteMCPServer: math-api
MCP tool:        add_numbers

Pi tool:
mcp__math_api__add_numbers
```

Initial MCP support should include:

- Streamable HTTP
- selected tools
- literal headers
- ConfigMap-backed headers
- Secret-backed headers
- timeout propagation
- egress derivation
- deterministic server naming

The upstream MCP server still receives the original MCP tool name.

Unsupported transport or security semantics should fail closed when Pi cannot represent them faithfully.

#### Skills and Agent Plugins

Reuse kagent's runtime-neutral resource materialization rather than adding Pi-specific artifact fetching.

Pi should explicitly load only compiler-selected standalone skills and plugin-provided skills while ambient Pi skill discovery remains disabled.

#### Lifecycle behavior

The initial Harness should cover the same core runtime lifecycle proven by the existing CLI Harnesses:

- A2A streaming
- persisted tasks
- session continuation/resume
- active-task cancellation
- built-in Pi tool execution
- Substrate checkpoint/fork and resume
- MCP tool execution
- durable runtime state

#### Build and release integration

Follow the Claude/Codex image lifecycle:

- `build-pi-harness`
- multi-arch image build
- CI image matrix
- digest-pinned E2E image
- release packaging
- dedicated provider E2E coverage

Where behavior is runtime-neutral, shared Harness runtime utilities should be reused. Pi-specific RPC behavior should remain isolated in the Pi driver.

### Compatibility / Initial Scope

The goal is to make Pi another native Harness, not to require artificial feature-for-feature equivalence with every other CLI.

The first version should support capabilities that can be represented faithfully and reject unsupported configuration explicitly.

Initial non-goals / follow-ups include:

- Shared AgentTemplate / native Pi subagent support
- Dedicated AgentTemplate tools
- Amazon Bedrock and Vertex providers
- SSE / stdio MCP
- MCP approval / HITL
- custom MCP TLS
- model tuning fields that Pi cannot preserve
- API-key passthrough
- Pi ambient/user-installed extensions

These can be added incrementally and independently once their behavior is validated.

### 🔄 Alternatives Considered

#### BYO only

A Pi BYO adapter can prove the runtime and private-A2A contract, but it is not the best long-term integration.

The generic BYO configuration loses runtime-specific information, notably native MCP server identity, and moves compatibility translation into the Actor instead of the controller.

Claude and Codex have already established the preferred first-class Harness pattern.

#### Generic ACP adapter

A generic ACP runtime could be useful for other agents, but it does not eliminate Pi-specific model, credential, MCP, skill, persistence, and lifecycle semantics.

Pi already exposes a stable machine-facing RPC mode suitable for a thin native adapter.

ACP support could be pursued independently without blocking a Pi Harness.

#### Embed a Pi SDK

The proposed integration supervises the official Pi CLI rather than embedding Pi implementation internals.

This follows the same general approach as the existing native coding-agent Harnesses and reduces coupling to Pi internals.

### 🎯 Affected Service(s)

Controller Service, Harness runtime, CI/build/release

### 📚 Additional Context

Related kagent work:

- #1676 - original Claude runtime discussion; the conversation also explicitly considered Codex and local Pi agents
- #2366 - Harness / AgentTemplate architecture
- #2602 - initial Claude Code Harness implementation
- #2645 - initial Codex Harness implementation

The Codex implementation explicitly reused the Claude Harness plumbing and targeted equivalent runtime coverage. Pi should extend that existing family rather than create a parallel integration model.

An experimental implementation is available in a fork and is being split into reviewable pieces:

1. Pi runtime proven through the existing BYO/private-A2A boundary
2. first-class `spec.pi` and native Pi compiler

The implementation currently exercises streaming, persistence/resume, checkpoint/fork, cancellation, built-in tools, Streamable HTTP MCP, selected MCP tools, Secret-backed MCP headers, skills and native MCP namespacing.

### 🙋 Are you willing to contribute?

- [x] I am willing to submit a PR for this feature
