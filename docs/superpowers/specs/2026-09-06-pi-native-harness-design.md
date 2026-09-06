# First-Class Pi Harness Design

## Summary

Promote the Pi runtime proven by `feat/pi-harness` from the generic BYO compiler path to a first-class `kagent.dev/v1alpha3` Harness runtime.

This change is intentionally stacked on the Pi runtime PR. The base PR owns the Actor-side Pi runtime, image, RPC lifecycle, skills, MCP bridge, resume, checkpoint/fork behavior, cancellation, and E2E infrastructure. This PR owns the public `spec.pi` API, the controller-side native compilation path, and the minimal Pi config/adapter changes required to consume that native compiler output directly.

The end state mirrors the existing Codex and Claude Harness architecture:

```text
Harness.spec.pi
      |
      v
resolved translator.HarnessInput
      |
      v
translator/pi.Compiler
      |
      +-- provider/auth validation
      +-- reserved environment validation
      +-- skill compilation
      +-- native MCP compilation
      +-- egress calculation
      +-- provenance
      |
      v
versioned go/harness/pi/config.Config
      |
      v
KAGENT_CONFIG_JSON
      |
      v
kagent-pi Actor runtime from PR1
```

Once this PR lands, Pi no longer depends on the BYO compiler in the production native path.

## Goals

1. Add `spec.pi: {}` as a first-class Harness runtime selector.
2. Add a dedicated Pi Harness compiler alongside Codex and Claude.
3. Make the controller emit Pi's versioned runtime config directly.
4. Move unsupported-Pi validation from Actor startup to AgentTemplate compilation wherever the required Kubernetes inputs are available in the controller.
5. Preserve `RemoteMCPServer` identity so Pi MCP tools can use deterministic native names such as `mcp__<server>__<tool>`.
6. Make credentials, egress destinations, provenance, skills, MCP, Harness environment variables, and runtime config compiler-owned in the same style as the other native Harnesses.
7. Convert the existing Pi E2E Harness fixture from `byo: {}` to `pi: {}` so the already-built Pi conformance tests exercise the first-class path.
8. Keep the API surface minimal. `PiHarness` is empty in the first release.

## Non-goals

This PR does not add new Pi runtime capabilities solely because the Harness becomes first-class.

Specifically out of scope:

- Shared/local AgentTemplate tools.
- Dedicated AgentTemplate tools.
- Amazon Bedrock support.
- Vertex AI support.
- SSE or stdio MCP.
- Human-in-the-loop approvals.
- API-key passthrough.
- Custom model TLS or arbitrary model headers.
- Configurable Pi permissions/trust policy.
- kagent memory/context configuration.

Shared agents and Bedrock are the highest-priority parity candidates after the native compiler exists, and should be separate follow-up PRs so each contribution remains reviewable.

## Stacking and dependency model

The branch for this PR is created directly from the latest `feat/pi-harness` commit. The pull request base is `feat/pi-harness`, not `main`.

```text
main
  |
  +-- PR1 feat/pi-harness
        |
        +-- PR2 feat/pi-harness-native
```

PR2 therefore contains no duplicated Pi runtime implementation. If PR1 is rebased or changes during review, PR2 is rebased onto the updated PR1 branch. Once PR1 lands upstream, PR2 can be retargeted to upstream `main`.

## Public API

Add an empty runtime selector matching the shape of the existing native Harnesses:

```go
// PiHarness selects the Pi runtime adapter.
type PiHarness struct{}
```

Add it to `HarnessSpec`:

```go
Pi *PiHarness `json:"pi,omitempty"`
```

Update the CEL exact-one validation to require exactly one of:

- `kagent`
- `codex`
- `claude`
- `pi`
- `byo`

The BYO-specific `workload.command` rule remains BYO-only. A native Pi Harness uses the image entrypoint by default, exactly like Codex and Claude.

No Pi-specific knobs are added under `spec.pi` in this PR. Existing `ModelConfig`, `AgentTemplate`, `RemoteMCPServer`, skill/plugin, Harness environment, workload, and Substrate policy fields are sufficient for the first release.

Generated deepcopy and CRD artifacts must be regenerated rather than hand-maintained where repository tooling permits it. The committed generated CRD base and Helm CRD must both include the new field and CEL rule.

## Compiler routing

Add:

```go
HarnessTypePi HarnessType = "pi"
```

Update `harnessType()` so `spec.pi` routes to it.

Register `pitranslator.NewCompiler(ctx, collections)` in the controller's Harness compiler map alongside kagent, Codex, Claude, and BYO.

The gRPC Harness API adds the denormalized runtime string `pi` and returns it when `object.Spec.Pi != nil`.

Tests must cover all three routing surfaces:

- central compiler selection,
- controller compiler registration/reconciliation,
- gRPC Harness runtime reporting.

## Pi compiler

Create `go/core/internal/translator/pi` with a dedicated `Compiler` implementing `translator.HarnessCompiler`.

The compiler consumes the already-resolved `translator.HarnessInput`; it does not re-fetch AgentTemplates or model references that the shared translator already resolved.

### Required root inputs

Compilation fails if any of the following are absent:

- Harness,
- root AgentTemplate,
- resolved ModelConfig,
- non-empty model ID.

Unsupported public configuration must fail with `translator.NewValidationError` so the AgentTemplate status reports the incompatibility before an Actor is started.

### Provider support

The first native Pi compiler supports exactly the providers already proven by PR1.

#### OpenAI

Supported:

- Secret-backed API key.
- `openAI.apiFormat = chatCompletions` or `responses`.
- Default OpenAI endpoint.
- Absolute HTTP(S) custom `baseUrl`.

Compile to a compiler-owned Pi provider:

- provider name: `kagent-openai`
- API: `openai-completions` or `openai-responses`
- credential environment: `OPENAI_API_KEY`

The compiler validates the Secret and key exist and are non-empty.

#### Anthropic

Supported:

- Secret-backed API key.
- Default Anthropic endpoint.
- Absolute HTTP(S) custom `baseUrl`.

Compile to:

- provider name: `kagent-anthropic`
- API: `anthropic-messages`
- credential environment: `ANTHROPIC_API_KEY`

The compiler validates the Secret and key exist and are non-empty.

#### Rejected provider/model features

The compiler rejects the same semantic set the PR1 runtime currently rejects through `FromAgentConfig`, including unsupported tuning, API-key passthrough, custom model TLS, arbitrary provider headers, and unsupported providers.

Where CRD defaults are meaningful, the compiler normalizes those defaults before determining whether the remaining options are unsupported.

### Provider egress

Provider compilation emits deterministic egress hostnames:

- OpenAI default: `api.openai.com`
- Anthropic default: `api.anthropic.com`
- custom endpoint: parsed hostname from the validated absolute HTTP(S) URL

Credentials and egress are derived together so a custom endpoint cannot retain the default provider egress accidentally.

## Compiler-owned environment

The Pi compiler owns at least:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- the `KAGENT_PI_MCP_CREDENTIAL_` prefix used for generated MCP credential environment variables

The Pi home plus offline/telemetry variables remain Actor/runtime-owned and are not accepted as controller-generated provider configuration.

If `Harness.spec.env` attempts to override a compiler-owned name or prefix, compilation fails with a validation error.

All other Harness environment entries are preserved. Literal values remain literal. `credentialRef` entries remain Kubernetes `EnvVarSource` references until the standard revision environment resolution stage.

## Skills and plugins

Use the shared `translator.CompileSkillResources(root.Template)` helper, matching Codex and Claude.

The resulting `agentplugin.Resources` is attached directly to the versioned Pi config. Skill/package egress from the helper is added to the revision egress set.

For `AgentTemplate.spec.plugins`, Pi consumes only the explicitly selected `skills` represented by that shared resource contract. Plugin hooks, commands, executables, and implicit plugin MCP servers are not compiled or executed by the Pi Harness.

The runtime continues to disable ambient Pi skill discovery and explicitly loads only compiler-owned materialized skills.

## Native MCP compilation

PR1's BYO bridge necessarily loses `RemoteMCPServer` identity. The native compiler fixes that by compiling directly from `[]translator.ResolvedMCPTool`.

Extend `piconfig.MCPServer` with a compiler-owned server name:

```go
type MCPServer struct {
    Name           string
    URL            string
    Headers        map[string]string
    EnabledTools   []string
    TimeoutSeconds float64
}
```

The compiler supports the runtime behavior already implemented and tested in PR1:

- Streamable HTTP only.
- Whole-server bindings or explicit selected tools.
- Literal, ConfigMap-backed, and Secret-backed request headers.
- Positive request timeout.
- `terminateOnClose=true` or its platform default.

It rejects:

- SSE and stdio,
- approval policies,
- `terminateOnClose=false`,
- custom MCP TLS semantics not implemented by the Pi bridge,
- unsupported request-header propagation semantics.

### MCP credentials

Secret-backed header values are represented in Pi config as environment references, never plaintext Secret values.

Generated environment names use the deterministic reserved prefix:

```text
KAGENT_PI_MCP_CREDENTIAL_<hash>
```

The compiler validates referenced ConfigMaps/Secrets and keys using the same controller-owned resolution style as Codex/Claude, adds the needed EnvVars, and includes those references in provenance.

### Tool names

The compiler preserves the Kubernetes `RemoteMCPServer` name in Pi config. The Pi extension derives the native namespace using the Codex convention of replacing `-` with `_` in the server name, then registers each tool as:

```text
mcp__<server-name-with-hyphens-replaced-by-underscores>__<tool-name>
```

The underlying MCP `tools/call` request still uses the original MCP tool name.

This removes the PR1 BYO collision restriction. Duplicate final native names are rejected deterministically during extension initialization.

## Runtime config boundary

PR1 already defines strict, versioned `go/harness/pi/config.Config` parsing.

PR2 changes the Actor adapter to consume that config directly from `KAGENT_CONFIG_JSON`:

```text
KAGENT_CONFIG_JSON -> piconfig.Parse -> adapter materialization -> driver
```

The native path no longer performs:

```text
ADK AgentConfig -> piconfig.FromAgentConfig
```

PR2 removes `FromAgentConfig`, the BYO-only provider/MCP conversion helpers, and their BYO-specific unit tests after equivalent native compiler tests are in place. The final Pi runtime has one configuration contract, matching Codex and Claude.

The Actor remains responsible for runtime-only validation such as pinned Pi executable/version, filesystem ownership, compiler-owned resource paths, and process lifecycle.

## Agent card

The Pi compiler emits the same private A2A AgentCard shape used by the existing native compilers:

- normalized AgentTemplate name,
- AgentTemplate description,
- private loopback interface on port 80,
- streaming capability,
- text input/output modes.

No public network endpoint is encoded into the card.

## Provenance

Pi gets native revision provenance rather than BYO provenance.

At minimum provenance covers:

- Harness,
- root AgentTemplate,
- ModelConfig,
- generated Pi config,
- generated AgentCard,
- referenced provider credential Secrets/keys,
- MCP RemoteMCPServers,
- MCP header ConfigMaps/Secrets/keys.

Skill/plugin source definitions are already part of the AgentTemplate spec and therefore covered by AgentTemplate provenance. No separate synthetic provenance object is added solely for those embedded source fields.

Follow the existing Codex/Claude provenance ordering and hashing conventions rather than creating a Pi-specific provenance format.

## Revision output

The Pi compiler returns a normal `translator.CompileResult` with:

- Harness workload image,
- no BYO command requirement,
- resolved environment,
- Pi config JSON,
- AgentCard JSON,
- worker pool,
- snapshot location,
- provenance,
- deduplicated/sorted egress destinations.

Unsupported semantics are compilation errors; the first native Pi compiler does not introduce a Pi-specific warning class.

The output shape is otherwise indistinguishable from the other native compilers to downstream reconciliation code.

## E2E migration

Change the existing Pi lifecycle Harness fixture from:

```yaml
byo: {}
workload:
  command: ["/usr/local/bin/kagent-pi"]
```

to:

```yaml
pi: {}
```

and rely on the Pi image's entrypoint.

All existing Pi E2E scenarios from PR1 then become native-Harness conformance tests without duplicating test logic:

- streaming and persisted task state,
- exact Pi session resume,
- checkpoint/fork resume,
- built-in `bash` tool execution,
- Streamable HTTP MCP,
- Secret-backed MCP headers,
- streamed/persisted tool events,
- active-task cancellation.

The tests must no longer pass merely because generic BYO compilation works.

## API and compiler tests

Test-first coverage includes:

1. CRD/CEL accepts exactly one `pi` runtime and rejects `pi` combined with any other runtime.
2. Deepcopy preserves `HarnessSpec.Pi`.
3. `harnessType()` selects `HarnessTypePi`.
4. gRPC runtime reporting returns `pi`.
5. OpenAI Chat Completions compile success.
6. OpenAI Responses compile success.
7. Anthropic compile success.
8. custom provider endpoint egress.
9. missing or empty credential Secret/key failure.
10. unsupported provider/tuning/header/TLS/API-key-passthrough failure.
11. reserved Harness environment conflict failure.
12. skill resources and skill egress.
13. whole-server MCP compilation.
14. selected MCP tool compilation.
15. literal and Secret/ConfigMap-backed MCP headers without Secret persistence.
16. deterministic MCP names and server identity preservation.
17. unsupported MCP protocol/approval/TLS/termination semantics failure.
18. deterministic provenance changes when relevant referenced inputs change.
19. Pi compiler registration in controller reconciliation.
20. existing Pi E2E suite using `spec.pi`.

## Generated artifacts

Expected generated/API files include at least:

- `go/api/v1alpha3/zz_generated.deepcopy.go`
- `go/api/config/crd/bases/kagent.dev_harnesses.yaml`
- `helm/kagent-crds/templates/kagent.dev_harnesses.yaml`

Use the repository generation command that owns these outputs when a runnable checkout is available and verify generated-output drift is zero. If generation cannot run in the current connector-only environment, do not claim generated artifacts are authoritative until CI or a real checkout verifies them.

## Pull request boundaries

PR2 changes one conceptual thing: Pi becomes a first-class native Harness.

It may modify the existing Pi runtime config/adapter only where required to consume the new native compiler output or preserve server identity. It must not add Shared-agent or new cloud-provider features at the same time.

Follow-up parity work is intentionally split:

```text
PR1  Pi runtime + BYO proof
PR2  first-class spec.pi + native compiler
PR3  Pi Shared/local AgentTemplate tools
PR4  Pi Amazon Bedrock provider
```

## Acceptance criteria

The PR is ready for upstream review when:

- `spec.pi: {}` is a valid native Harness selector.
- the exact-one CRD rule includes Pi.
- Pi has its own `HarnessType` and registered compiler.
- the controller emits `piconfig.Config` directly, without BYO compilation in the native path.
- supported provider/MCP/skill semantics are validated before Actor creation.
- MCP server identity and deterministic names are preserved.
- gRPC reports runtime `pi`.
- generated API/CRD artifacts are current.
- all existing Pi E2Es run through the native Harness fixture.
- focused API/compiler/controller/grpc tests pass.
- full Go, image-build, and E2E CI is observed passing on the actual branch.
- every commit carries the required DCO sign-off.

Until authoritative CI executes, the PR remains draft.