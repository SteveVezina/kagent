# First-Class Pi Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote the Pi runtime from the BYO compiler path to a first-class `spec.pi` Harness with a dedicated controller-side compiler and native E2E coverage.

**Architecture:** Add an empty public `PiHarness` selector and route it through a new `translator/pi.Compiler` that consumes resolved `HarnessInput` and emits the existing versioned `go/harness/pi/config.Config` directly. Keep all Actor/runtime implementation in PR1; PR2 owns API selection, compile-time validation, native MCP identity, environment/egress/provenance, generated artifacts, and migration of the existing Pi E2Es from `byo` to `pi`.

**Tech Stack:** Go, Kubernetes CRDs/controller-gen, KRT translator/controller, A2A, Pi runtime config from PR1, kagent mock LLM/MCP E2E infrastructure.

**Spec:** `docs/superpowers/specs/2026-09-06-pi-native-harness-design.md`

## Global Constraints

- PR base remains `feat/pi-harness`; do not duplicate PR1 runtime/image implementation.
- Public selector is exactly `spec.pi: {}` with no Pi-specific knobs in this PR.
- Native Pi supports only the OpenAI and Anthropic behavior already proven by PR1.
- Native MCP is Streamable HTTP only and preserves `RemoteMCPServer` identity.
- Native MCP tool names are `mcp__<server-name-with-hyphens-replaced-by-underscores>__<tool-name>`.
- Plugin support remains skill-only materialization; no hooks, commands, executables, or implicit plugin MCP servers.
- Shared/local agents, Bedrock, Vertex, Dedicated agents, SSE/stdio MCP, approvals, API-key passthrough, model custom TLS/headers, and memory/context remain out of scope.
- Secret values must never be serialized into Pi config or provenance payloads.
- Every commit must contain `Signed-off-by: Steve Vezina <steve.vezina@gmail.com>`.
- Do not claim generated artifacts or full E2E are verified until real repository generation/CI executes.

---

### Task 1: Add the public `spec.pi` Harness selector

**Files:**
- Modify: `go/api/v1alpha3/harness_types.go`
- Modify: `go/api/v1alpha3/configuration_crd_cel_test.go`
- Generated later: `go/api/v1alpha3/zz_generated.deepcopy.go`
- Generated later: `go/api/config/crd/bases/kagent.dev_harnesses.yaml`
- Generated later: `helm/kagent-crds/templates/kagent.dev_harnesses.yaml`

**Interfaces:**
- Produces: `type PiHarness struct{}` and `HarnessSpec.Pi *PiHarness`.
- Produces: exact-one validation over `kagent`, `codex`, `claude`, `pi`, `byo`.
- Consumed by: Tasks 2, 5, and 6.

- [ ] **Step 1: Write failing CEL/API tests**

Add cases equivalent to:

```go
{
    name: "Harness accepts Pi runtime",
    object: validHarness(namespace, "pi", HarnessSpec{Pi: &PiHarness{}}),
},
{
    name: "Harness rejects Pi with another runtime",
    object: validHarness(namespace, "pi-codex", HarnessSpec{Pi: &PiHarness{}, Codex: &CodexHarness{}}),
    wantReject: "exactly one of kagent, codex, claude, pi, or byo must be specified",
},
```

- [ ] **Step 2: Run focused API tests and confirm RED**

Run:

```bash
cd go && go test ./api/v1alpha3 -run 'Harness|CRD|CEL' -count=1
```

Expected before implementation: compile failure because `PiHarness` / `HarnessSpec.Pi` do not exist, or CEL validation still omits `pi`.

- [ ] **Step 3: Add the minimal API selector**

Add:

```go
// PiHarness selects the Pi runtime adapter.
type PiHarness struct{}
```

and:

```go
// +optional
Pi *PiHarness `json:"pi,omitempty"`
```

Update the HarnessSpec CEL expression/message to count `pi` and say `exactly one of kagent, codex, claude, pi, or byo must be specified`.

- [ ] **Step 4: Run focused API tests and confirm GREEN**

```bash
cd go && go test ./api/v1alpha3 -run 'Harness|CRD|CEL' -count=1
```

- [ ] **Step 5: Commit**

```bash
git add go/api/v1alpha3/harness_types.go go/api/v1alpha3/configuration_crd_cel_test.go
git commit -s -m 'feat(api): add Pi Harness selector'
```

---

### Task 2: Route Pi through the shared compiler and gRPC surfaces

**Files:**
- Modify: `go/core/internal/translator/compiler.go`
- Modify: `go/core/internal/translator/compiler_test.go`
- Modify: `go/core/internal/grpcserver/harness.go`
- Modify: `go/core/internal/grpcserver/agenttemplate_harness_test.go`

**Interfaces:**
- Consumes: `HarnessSpec.Pi` from Task 1.
- Produces: `translator.HarnessTypePi` and `harnessRuntimePi = "pi"`.
- Consumed by: controller compiler registration in Task 4.

- [ ] **Step 1: Write failing routing tests**

Add a compiler selection test that constructs a Harness with `Spec.Pi = &v1alpha3.PiHarness{}` and registers a fake compiler under `HarnessTypePi`; assert the fake compiler is called.

Add a gRPC/runtime test equivalent to existing Codex/Claude cases and assert `harnessRuntime(piHarness) == "pi"`.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
cd go && go test ./core/internal/translator ./core/internal/grpcserver -run 'Pi|HarnessRuntime|Compiler' -count=1
```

Expected: missing `HarnessTypePi`, missing runtime string, or Pi Harness reported unsupported.

- [ ] **Step 3: Add Pi routing**

In `translator/compiler.go` add:

```go
HarnessTypePi HarnessType = "pi"
```

and:

```go
case harness.Spec.Pi != nil:
    return HarnessTypePi
```

In `grpcserver/harness.go` add:

```go
harnessRuntimePi = "pi"
```

and:

```go
case object.Spec.Pi != nil:
    return harnessRuntimePi
```

- [ ] **Step 4: Run focused tests and confirm GREEN**

```bash
cd go && go test ./core/internal/translator ./core/internal/grpcserver -run 'Pi|HarnessRuntime|Compiler' -count=1
```

- [ ] **Step 5: Commit**

```bash
git add go/core/internal/translator/compiler.go go/core/internal/translator/compiler_test.go go/core/internal/grpcserver/harness.go go/core/internal/grpcserver/agenttemplate_harness_test.go
git commit -s -m 'feat(core): route native Pi Harness'
```

---

### Task 3: Implement the native Pi compiler

**Files:**
- Create: `go/core/internal/translator/pi/compiler.go`
- Create: `go/core/internal/translator/pi/compiler_test.go`
- Modify: `go/harness/pi/config/config.go`
- Modify: `go/harness/pi/config/mcp_test.go`

**Interfaces:**
- Consumes: `translator.HarnessInput`, `translator.CompileSkillResources`, PR1 `piconfig.Config`.
- Produces: `func NewCompiler(ctx krt.HandlerContext, collections translator.Collections) *Compiler`.
- Produces: `Compile(context.Context, *translator.HarnessInput) (*translator.CompileResult, error)`.
- Produces: `piconfig.MCPServer.Name string` for native MCP identity.
- Consumed by: Task 4 controller registration and Task 6 E2E.

- [ ] **Step 1: Write failing provider compiler tests**

Cover:

```go
func TestCompileOpenAIChatCompletions(t *testing.T)
func TestCompileOpenAIResponses(t *testing.T)
func TestCompileAnthropic(t *testing.T)
func TestCompileCustomEndpointEgress(t *testing.T)
func TestCompileRejectsMissingCredential(t *testing.T)
func TestCompileRejectsUnsupportedModelOptions(t *testing.T)
func TestCompileRejectsReservedHarnessEnv(t *testing.T)
```

Success tests must unmarshal `Revision.ConfigJSON` with `piconfig.Parse`, assert the compiler-owned provider (`kagent-openai` / `kagent-anthropic`), model/API/base URL, environment SecretKeyRef, and egress hostname.

- [ ] **Step 2: Run provider tests and confirm RED**

```bash
cd go && go test ./core/internal/translator/pi -run 'Provider|OpenAI|Anthropic|Credential|Environment' -count=1
```

Expected: package/compiler missing.

- [ ] **Step 3: Implement provider/env compilation**

Create a `Compiler` containing the KRT handler context and collections. Follow the Codex/Claude native compiler shape:

```go
type Compiler struct {
    ctx         krt.HandlerContext
    collections translator.Collections
}

func NewCompiler(ctx krt.HandlerContext, collections translator.Collections) *Compiler {
    return &Compiler{ctx: ctx, collections: collections}
}
```

Implement exact OpenAI/Anthropic validation from the spec, Secret existence/non-empty key validation, `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` EnvVars, reserved environment rejection, provider egress, config validation and JSON serialization.

- [ ] **Step 4: Run provider tests and confirm GREEN**

```bash
cd go && go test ./core/internal/translator/pi -run 'Provider|OpenAI|Anthropic|Credential|Environment' -count=1
```

- [ ] **Step 5: Write failing skill and MCP compiler tests**

Cover:

```go
func TestCompileSkillResources(t *testing.T)
func TestCompileMCPWholeServer(t *testing.T)
func TestCompileMCPSelectedTools(t *testing.T)
func TestCompileMCPSecretHeaderDoesNotPersistSecret(t *testing.T)
func TestCompileMCPConfigMapHeader(t *testing.T)
func TestCompileMCPPreservesServerIdentity(t *testing.T)
func TestCompileRejectsUnsupportedMCP(t *testing.T)
```

For server identity require:

```go
if got := cfg.MCPServers[0].Name; got != "math-server" {
    t.Fatalf("server name = %q, want math-server", got)
}
```

- [ ] **Step 6: Run skill/MCP tests and confirm RED**

```bash
cd go && go test ./core/internal/translator/pi -run 'Skill|MCP' -count=1
```

- [ ] **Step 7: Extend Pi config with native MCP identity**

Add:

```go
type MCPServer struct {
    Name           string            `json:"name,omitempty"`
    URL            string            `json:"url"`
    Headers        map[string]string `json:"headers,omitempty"`
    EnabledTools   []string          `json:"enabled_tools,omitempty"`
    TimeoutSeconds float64           `json:"timeout_seconds,omitempty"`
}
```

Allow empty `Name` only for the PR1 BYO compatibility path; native compiler always sets it. Update deterministic normalization/identity tests accordingly.

- [ ] **Step 8: Implement skills/MCP compilation**

Use `translator.CompileSkillResources(root.Template)`. Compile each `ResolvedMCPTool` directly from its `RemoteMCPServer`, preserving `Server.Name`; generate deterministic `KAGENT_PI_MCP_CREDENTIAL_<hash>` EnvVars for Secret-backed headers, preserve literal/ConfigMap-backed header values without persisting Secret contents, validate Streamable HTTP semantics, and collect MCP/skill egress.

- [ ] **Step 9: Add provenance and AgentCard**

Follow Codex/Claude deterministic object/hash ordering. Include Harness, AgentTemplate, ModelConfig, generated config/card, credential references, MCP objects/header references, and portable skill inputs without including Secret plaintext.

Return a complete `translator.CompileResult` with sorted/deduplicated egress.

- [ ] **Step 10: Run all Pi compiler/config tests and confirm GREEN**

```bash
cd go && go test ./core/internal/translator/pi ./harness/pi/config -count=1
```

- [ ] **Step 11: Commit**

```bash
git add go/core/internal/translator/pi go/harness/pi/config
git commit -s -m 'feat(pi): add native Harness compiler'
```

---

### Task 4: Register the Pi compiler in controller reconciliation

**Files:**
- Modify: `go/core/internal/controller/reconciler.go`
- Modify: `go/core/internal/controller/collections_test.go` and/or `go/core/internal/controller/reconciler_test.go`

**Interfaces:**
- Consumes: `translator.HarnessTypePi` from Task 2 and `pitranslator.NewCompiler` from Task 3.
- Produces: actual controller reconciliation support for native Pi Harnesses.

- [ ] **Step 1: Write a failing controller test**

Construct a Pi Harness/AgentTemplate pair and assert reconciliation compiles a desired revision instead of returning `Harness runtime is not supported by any compiler`.

- [ ] **Step 2: Run focused controller test and confirm RED**

```bash
cd go && go test ./core/internal/controller -run 'Pi' -count=1
```

- [ ] **Step 3: Register the compiler**

Import the native Pi translator and add:

```go
v2translator.HarnessTypePi: pitranslator.NewCompiler(ctx, collections),
```

beside the existing kagent/Codex/Claude/BYO compiler registrations.

- [ ] **Step 4: Run focused controller test and confirm GREEN**

```bash
cd go && go test ./core/internal/controller -run 'Pi' -count=1
```

- [ ] **Step 5: Commit**

```bash
git add go/core/internal/controller
git commit -s -m 'feat(controller): register Pi Harness compiler'
```

---

### Task 5: Make the Actor consume native Pi config and namespace MCP tools

**Files:**
- Modify: `go/harness/pi/cmd/main.go`
- Modify: `go/harness/pi/internal/adapter/adapter.go`
- Modify: `go/harness/pi/internal/adapter/adapter_test.go`
- Modify: `go/harness/pi/extensions/kagent-mcp-core.mjs`
- Modify: `go/harness/pi/extensions/kagent-mcp-core.test.mjs`
- Modify: `go/harness/pi/README.md`

**Interfaces:**
- Consumes: native `piconfig.Config` and `MCPServer.Name` from Task 3.
- Produces: `KAGENT_CONFIG_JSON -> piconfig.Parse -> adapter` native runtime path.
- Produces: Pi MCP tool names `mcp__<sanitized-server>__<tool>` while `tools/call` still sends the original MCP tool name.

- [ ] **Step 1: Write failing native-config adapter tests**

Make `cmd/main`/adapter tests pass already-serialized `piconfig.Config`, and assert malformed/unknown native config fails strict parsing. Add an MCP extension test asserting server `math-server` tool `add_numbers` registers as `mcp__math_server__add_numbers` and calls MCP with `add_numbers`.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
cd go && go test ./harness/pi/cmd ./harness/pi/internal/adapter -count=1
node --test go/harness/pi/extensions/kagent-mcp-core.test.mjs
```

- [ ] **Step 3: Switch the runtime to native config parsing**

Replace the production `adk.AgentConfig -> piconfig.FromAgentConfig` path with strict `piconfig.Parse([]byte(KAGENT_CONFIG_JSON))`. Keep PR1 BYO conversion helpers only if PR1 tests on the stacked base still require them; native Actor startup must not invoke them.

- [ ] **Step 4: Namespace MCP registrations**

For named native servers compute:

```text
mcp__<strings.ReplaceAll(server.Name, "-", "_")>__<tool.Name>
```

Keep the underlying MCP client call using the original `tool.Name`. Retain PR1 collision rejection only for unnamed BYO servers.

- [ ] **Step 5: Run focused runtime tests and confirm GREEN**

```bash
cd go && go test ./harness/pi/cmd ./harness/pi/internal/adapter ./harness/pi/internal/driver -count=1
node --test go/harness/pi/extensions/kagent-mcp-core.test.mjs
```

- [ ] **Step 6: Update README native example**

Change the primary example to `spec.pi: {}` and document that BYO was the proof path in PR1, while first-class compilation now owns provider/MCP/skill semantics and MCP namespaces.

- [ ] **Step 7: Commit**

```bash
git add go/harness/pi
git commit -s -m 'feat(pi): consume native Harness config'
```

---

### Task 6: Generate API artifacts and migrate Pi E2E to `spec.pi`

**Files:**
- Generate/modify: `go/api/v1alpha3/zz_generated.deepcopy.go`
- Generate/modify: `go/api/config/crd/bases/kagent.dev_harnesses.yaml`
- Generate/modify: `helm/kagent-crds/templates/kagent.dev_harnesses.yaml`
- Modify: `go/core/test/e2e/manifests/lifecycle.yaml.tmpl`
- Modify as needed: `go/core/test/e2e/pi_interaction_test.go`
- Modify as needed: `go/core/test/e2e/pi_mcp_interaction_test.go`
- Modify: PR #2 description

**Interfaces:**
- Consumes: completed public/native compiler/runtime path.
- Produces: generated API artifacts and end-to-end native Pi conformance coverage.

- [ ] **Step 1: Change the Pi Harness fixture to native selection**

Replace:

```yaml
byo: {}
workload:
  image: ${KAGENT_E2E_PI_IMAGE}
  command: ["/usr/local/bin/kagent-pi"]
```

with:

```yaml
pi: {}
workload:
  image: ${KAGENT_E2E_PI_IMAGE}
```

Update MCP E2E expected tool names to the native namespace, e.g. `mcp__<server>__add_numbers`.

- [ ] **Step 2: Generate deepcopy and CRD base**

Run from `go/`:

```bash
make generate manifests
```

Expected: `zz_generated.deepcopy.go` includes `PiHarness` / `HarnessSpec.Pi` and the CRD base includes `pi` plus the updated CEL rule.

- [ ] **Step 3: Refresh Helm CRD copy using the repository's normal CRD sync/generation flow**

Use the existing repository generation target/script that copies generated CRDs into `helm/kagent-crds/templates`. If no dedicated target exists, compare the Helm template to the generated base and apply only the generated Pi/CEL delta, then let CI generation checks validate it.

- [ ] **Step 4: Run focused Go tests**

```bash
cd go && go test ./api/v1alpha3 ./core/internal/translator/... ./core/internal/controller ./core/internal/grpcserver ./harness/pi/... -count=1
```

- [ ] **Step 5: Run Pi extension tests**

```bash
node --test go/harness/pi/extensions/kagent-mcp-core.test.mjs
```

- [ ] **Step 6: Run build/E2E when the environment supports it**

```bash
make build-pi-harness
```

and the repository's Substrate E2E job with `KAGENT_E2E_PI_IMAGE` set to the built digest. Required scenarios: streaming/persistence, resume, checkpoint/fork, built-in tool events, MCP, Secret MCP header, and active cancellation.

- [ ] **Step 7: Inspect the stacked diff**

```bash
git diff feat/pi-harness...HEAD --stat
git diff feat/pi-harness...HEAD
```

Expected: only first-class Pi API/compiler/runtime-boundary/generated/E2E/docs changes. No Shared-agent or Bedrock implementation.

- [ ] **Step 8: Commit generated/E2E changes**

```bash
git add go/api go/core/test/e2e helm/kagent-crds
git commit -s -m 'test(pi): exercise native Pi Harness path'
```

- [ ] **Step 9: Refresh draft PR #2**

Update the PR body with implemented scope, explicit stack dependency on #1, verification commands actually executed, and remaining CI limitations. Keep it draft until authoritative Go/image/E2E CI passes.
