# Pi Harness MCP and Resources Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add compiler-owned skills and remote Streamable HTTP MCP tools to the Pi BYO Harness prototype without weakening kagent's isolation, credential, egress, or A2A semantics.

**Architecture:** Extend the existing versioned Pi runtime config with skill resources and a normalized list of direct remote MCP servers. The Go adapter materializes skills and a private non-secret `/data/pi/mcp.json`, then the Pi driver explicitly loads only compiler-owned skills and one immutable image-bundled extension. The extension uses `@modelcontextprotocol/sdk@1.25.2` to discover/filter tools and registers them through Pi's native `pi.registerTool()` path, so existing Pi RPC tool events continue to feed kagent's shared A2A translator.

**Tech Stack:** Go 1.x repository code, Pi Coding Agent `0.85.0`, Node 24, `@modelcontextprotocol/sdk@1.25.2`, `typebox@1.3.7`, kagent `agentplugins.Materialize`, Pi RPC, A2A, Substrate E2E.

**Spec:** `docs/superpowers/specs/2026-09-06-pi-mcp-resources-design.md`

## Global Constraints

- Keep the Harness selected through `spec.byo`; do not add public `spec.pi` CRD/API fields.
- Keep Pi Coding Agent pinned to `0.85.0`.
- Keep ambient Pi resources disabled with `--no-extensions`, `--no-skills`, `--no-context-files`, `--no-prompt-templates`, and `--no-themes`.
- Load only compiler-owned extension and skill paths explicitly.
- Support direct remote MCP over Streamable HTTP only in this increment.
- Reject SSE, stdio, approval/HITL, custom MCP TLS, and non-default MCP runtime options.
- Never serialize Secret values into `mcp.json` or any durable file.
- Preserve existing BYO egress/provenance behavior; do not create a second egress compiler.
- MCP tool application errors remain tool results; task terminal state remains Pi's settled outcome.
- Add capability claims only after authoritative E2E passes.

---

## File Structure

**Modify**
- `go/harness/pi/config/config.go` - versioned runtime contract, BYO mapping, MCP validation/normalization.
- `go/harness/pi/config/config_test.go` - contract and fail-closed mapping tests.
- `go/harness/pi/internal/adapter/adapter.go` - skill materialization, generated MCP config, compiler-owned runtime paths.
- `go/harness/pi/internal/adapter/adapter_test.go` - filesystem/privacy/materialization tests.
- `go/harness/pi/internal/driver/process.go` - explicit extension/skill CLI arguments only.
- `go/harness/pi/internal/driver/process_test.go` - exact Pi argument and isolation tests.
- `go/harness/pi/Dockerfile` - immutable MCP extension runtime dependencies and image-build extension tests.
- `go/core/test/e2e/pi_interaction_test.go` - real Pi MCP E2E tests.
- `go/harness/pi/README.md` - implemented capability and limitation documentation after tests exist.

**Create**
- `go/harness/pi/extensions/kagent-mcp-core.mjs` - dependency-injected MCP registration/core behavior.
- `go/harness/pi/extensions/kagent-mcp-core.test.mjs` - Node built-in unit tests with fake MCP clients.
- `go/harness/pi/extensions/kagent-mcp.ts` - thin Pi extension entrypoint using the real SDK and TypeBox.
- `go/harness/pi/extensions/package.json` - exact immutable extension dependencies.
- `go/core/test/e2e/mocks/invoke_pi_mcp.json` - OpenAI Chat Completions fixture that forces MCP invocation and final response.

The extension package lives under `/usr/local/lib/kagent-pi` in the runtime image. The production entrypoint path is fixed at `/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts`.

---

### Task 1: Extend the versioned Pi config with skills and direct MCP servers

**Files:**
- Modify: `go/harness/pi/config/config.go`
- Modify: `go/harness/pi/config/config_test.go`

**Interfaces:**
- Produces:

```go
type MCPServer struct {
    URL          string            `json:"url"`
    Headers      map[string]string `json:"headers,omitempty"`
    EnabledTools []string          `json:"enabled_tools,omitempty"`
}

// Config additions:
SkillResources *agentplugin.Resources `json:"skill_resources,omitempty"`
MCPServers     []MCPServer            `json:"mcp_servers,omitempty"`
```

- `FromAgentConfig(*adk.AgentConfig) (Config, error)` maps `AgentPlugins` to `SkillResources`, direct `HttpTools` to `MCPServers`, and rejects unsupported MCP transports/options.
- `Config.Validate()` validates and normalizes the runtime-owned fields before adapter use.

- [ ] **Step 1: Write failing strict round-trip tests**

Add tests that construct `Production(...)`, assign `SkillResources` and `MCPServers`, marshal, call `Parse`, and require deep equality.

```go
func TestParseRoundTripsMCPAndSkills(t *testing.T) {
    cfg := Production(
        Provider{Name: "kagent-openai", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY"},
        "gpt-5.4",
        "help",
    )
    cfg.SkillResources = &agentplugin.Resources{
        Skills: []agentplugin.Skill{{Name: "deploy"}},
    }
    cfg.MCPServers = []MCPServer{{
        URL: "https://mcp.example.com/mcp",
        Headers: map[string]string{"Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__"},
        EnabledTools: []string{"lookup", "search"},
    }}

    raw, err := json.Marshal(cfg)
    require.NoError(t, err)
    got, err := Parse(raw)
    require.NoError(t, err)
    require.Equal(t, cfg, got)
}
```

- [ ] **Step 2: Write failing BYO mapping tests**

Cover these exact cases:

```go
func TestFromAgentConfigMapsStreamableHTTPMCP(t *testing.T)
func TestFromAgentConfigMapsSkillResources(t *testing.T)
func TestFromAgentConfigSortsAndDeduplicatesSelectedTools(t *testing.T)
func TestFromAgentConfigPreservesMCPEnvironmentMarkers(t *testing.T)
func TestFromAgentConfigRejectsSSEMCP(t *testing.T)
func TestFromAgentConfigRejectsStdioMCP(t *testing.T)
func TestFromAgentConfigRejectsMCPApproval(t *testing.T)
func TestFromAgentConfigRejectsMCPTLS(t *testing.T)
func TestFromAgentConfigRejectsMCPRuntimeOverrides(t *testing.T)
func TestFromAgentConfigRejectsInvalidMCPURL(t *testing.T)
func TestFromAgentConfigRejectsDuplicateMCPServerDefinition(t *testing.T)
```

The Streamable HTTP fixture should use:

```go
HttpTools: []adk.HttpMcpServerConfig{{
    Params: adk.StreamableHTTPConnectionParams{
        Url: "https://mcp.example.com/mcp",
        Headers: map[string]string{
            "Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__",
        },
    },
    Tools: []string{"search", "lookup", "search"},
}},
```

Expected selected tools are `[]string{"lookup", "search"}`.

- [ ] **Step 3: Run the config tests and confirm RED**

Run from `go/`:

```bash
go test ./harness/pi/config -count=1
```

Expected: compile/test failures because `SkillResources`, `MCPServers`, and `MCPServer` do not exist and current `FromAgentConfig` rejects MCP/plugins.

- [ ] **Step 4: Implement the minimal config contract**

Add the `agentplugin` import, new fields/types, and these validation rules:

```go
func validateMCPServer(server MCPServer) error {
    if err := validateHTTPURL(server.URL); err != nil {
        return err
    }
    seen := map[string]struct{}{}
    for _, tool := range server.EnabledTools {
        if strings.TrimSpace(tool) == "" {
            return fmt.Errorf("MCP server %q has an empty enabled tool", server.URL)
        }
        if _, ok := seen[tool]; ok {
            return fmt.Errorf("MCP server %q has duplicate enabled tool %q", server.URL, tool)
        }
        seen[tool] = struct{}{}
    }
    return nil
}
```

Before creating each `MCPServer`, copy/sort/compact `HttpMcpServerConfig.Tools`. Reject if any of these are present:

```go
len(agent.SseTools) != 0
len(agent.StdioTools) != 0
len(tool.RequireApproval) != 0
tool.Params.Timeout != nil
tool.Params.SseReadTimeout != nil
tool.Params.TerminateOnClose != nil
tool.Params.TLSInsecureSkipVerify != nil
tool.Params.TLSCACertPath != nil
tool.Params.TLSDisableSystemCAs != nil
```

Assign:

```go
if agent.AgentPlugins != nil {
    resources := *agent.AgentPlugins
    cfg.SkillResources = &resources
}
```

Do not reject `HttpTools` or `AgentPlugins` after these mappings succeed.

Normalize the final server slice deterministically by URL, enabled-tool list, then stable header JSON. Reject exact duplicate normalized definitions.

- [ ] **Step 5: Run config tests and confirm GREEN**

```bash
go test ./harness/pi/config -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add go/harness/pi/config/config.go go/harness/pi/config/config_test.go
git commit -s -m "feat(pi-harness): compile skills and MCP config"
```

---

### Task 2: Materialize compiler-owned skills and private MCP configuration

**Files:**
- Modify: `go/harness/pi/internal/adapter/adapter.go`
- Modify: `go/harness/pi/internal/adapter/adapter_test.go`

**Interfaces:**
- Consumes: `config.Config.SkillResources`, `config.Config.MCPServers`.
- Produces driver config fields:

```go
ExtensionPaths []string
SkillPaths     []string
```

- Constants:

```go
const (
    piHomeEnv       = "PI_CODING_AGENT_DIR"
    mcpConfigEnv    = "KAGENT_PI_MCP_CONFIG"
    mcpExtensionPath = "/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts"
)
```

- Generated file shape:

```json
{
  "servers": [
    {
      "url": "https://mcp.example.com/mcp",
      "headers": {"Authorization":"__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__"},
      "enabled_tools": ["lookup"]
    }
  ]
}
```

- [ ] **Step 1: Write failing adapter tests**

Add:

```go
func TestNewMaterializesPrivateMCPConfig(t *testing.T)
func TestNewDoesNotPersistSecretMarkerValue(t *testing.T)
func TestNewMaterializesCompilerOwnedSkills(t *testing.T)
func TestNewRejectsAgentPluginMCPUntilSupported(t *testing.T)
```

For the MCP test, create BYO JSON containing one `http_tools` entry and assert:

```go
modelsPath := filepath.Join(durable, "pi", "mcp.json")
info, err := os.Stat(modelsPath)
require.NoError(t, err)
require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
require.NotContains(t, string(contents), "actual-secret-value")
```

The `Environment` passed to `New` should contain the actual Secret-derived env value while the JSON config contains only the `__KAGENT_ENV[...]__` marker.

For skills, use a local test artifact source already supported by the repository's `agentplugins` test helpers or a minimal file-backed fixture pattern used by existing `agentplugins.Materialize` tests. Assert selected skill content ends under `/data/pi/skills/<name>/SKILL.md`, never under the workspace.

- [ ] **Step 2: Run adapter tests and confirm RED**

```bash
go test ./harness/pi/internal/adapter -count=1
```

Expected: failures because the adapter currently only writes `models.json` and has no skills/MCP path plumbing.

- [ ] **Step 3: Implement compiler-owned directories and materialization**

Create/reconcile:

```text
/data/pi/packages
/data/pi/skills
```

Before materialization, remove stale compiler-owned entries from `skills` and `packages` using a Pi-local safe reconciliation helper that rejects symlinks before `RemoveAll`, matching the Codex adapter's generated-directory behavior.

Materialize:

```go
materialization, err := agentplugins.Materialize(ctx, *cfg.SkillResources, agentplugins.Paths{
    Packages: filepath.Join(piHome, "packages"),
    Skills:   filepath.Join(piHome, "skills"),
})
```

If `cfg.SkillResources` contains plugin packages, call:

```go
pluginMCP, err := agentplugins.LoadMCP(ctx, materialization, filepath.Join(piHome, "plugin-data"))
```

and fail closed if any `StreamableHTTP`, `SSE`, or `Stdio` plugin MCP entries exist. This prevents silently dropping AgentPlugin MCP semantics in this increment.

When `cfg.MCPServers` is non-empty, write `/data/pi/mcp.json` with `utils.ReplacePrivateFile`, set `KAGENT_PI_MCP_CONFIG` to that path, and pass exactly one extension path:

```go
extensionPaths = []string{mcpExtensionPath}
```

For skills, pass the materialized `SkillsDirectory` as one explicit Pi `--skill` path:

```go
skillPaths = []string{materialization.SkillsDirectory}
```

Do not pass any extension or skill path when the corresponding config is empty.

- [ ] **Step 4: Run adapter tests and confirm GREEN**

```bash
go test ./harness/pi/internal/adapter -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add go/harness/pi/internal/adapter/adapter.go go/harness/pi/internal/adapter/adapter_test.go
git commit -s -m "feat(pi-harness): materialize MCP and skills"
```

---

### Task 3: Add explicit compiler-owned extension and skill flags to the Pi process driver

**Files:**
- Modify: `go/harness/pi/internal/driver/process.go`
- Modify: `go/harness/pi/internal/driver/process_test.go`

**Interfaces:**
- `ProcessConfig` additions:

```go
ExtensionPaths []string
SkillPaths     []string
```

- [ ] **Step 1: Write failing process argument tests**

Extend the existing fake Pi process test so `ProcessConfig` includes:

```go
ExtensionPaths: []string{"/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts"},
SkillPaths: []string{"/data/pi/skills"},
```

Assert captured argv includes all of:

```text
--no-extensions
-e
/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts
--no-skills
--skill
/data/pi/skills
```

Also assert a config with no resource paths does not emit `-e` or `--skill`.

- [ ] **Step 2: Run driver tests and confirm RED**

```bash
go test ./harness/pi/internal/driver -count=1
```

Expected: resource-path assertions fail because the fields/arguments do not exist.

- [ ] **Step 3: Implement deterministic CLI argument emission**

Keep the existing ambient-disable flags unconditionally. After the base isolation flags, append each explicit path in stable input order:

```go
for _, path := range d.config.ExtensionPaths {
    args = append(args, "-e", path)
}
for _, path := range d.config.SkillPaths {
    args = append(args, "--skill", path)
}
```

Reject empty/relative compiler-owned paths in `NewProcessDriver` or adapter construction; only absolute paths are accepted.

- [ ] **Step 4: Run driver tests and confirm GREEN**

```bash
go test ./harness/pi/internal/driver -count=1
```

Expected: PASS.

- [ ] **Step 5: Run all Pi Go unit tests**

```bash
go test ./harness/pi/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 3**

```bash
git add go/harness/pi/internal/driver/process.go go/harness/pi/internal/driver/process_test.go
git commit -s -m "feat(pi-harness): load compiled Pi resources"
```

---

### Task 4: Implement the image-bundled Pi MCP extension with dependency-injected core tests

**Files:**
- Create: `go/harness/pi/extensions/kagent-mcp-core.mjs`
- Create: `go/harness/pi/extensions/kagent-mcp-core.test.mjs`
- Create: `go/harness/pi/extensions/kagent-mcp.ts`
- Create: `go/harness/pi/extensions/package.json`
- Modify: `go/harness/pi/Dockerfile`

**Interfaces:**
- `kagent-mcp-core.mjs` exports:

```js
export function expandHeaderValue(value, env)
export async function initializeMcpBridge({ config, env, createClient, registerTool })
export function mcpResultToPi(result)
```

- `createClient(server)` resolves to:

```js
{
  listTools: async () => [{ name, description, inputSchema }],
  callTool: async (name, args, signal) => result,
  close: async () => undefined,
}
```

- `registerTool({ name, description, inputSchema, execute })` receives a runtime-neutral registration from the core.
- `kagent-mcp.ts` adapts those interfaces to the real MCP SDK, `Type.Unsafe`, and `pi.registerTool`.

- [ ] **Step 1: Write the RED Node core tests before the core module**

Use `node:test` and `node:assert/strict`. Cover:

```js
test("expands compiler-owned environment header marker", ...)
test("rejects missing environment header", ...)
test("filters exactly the selected MCP tools", ...)
test("rejects a requested tool missing from tools/list", ...)
test("rejects duplicate exposed tool names across servers", ...)
test("forwards MCP arguments and abort signal to tools/call", ...)
test("converts MCP text content to Pi text content", ...)
test("preserves MCP application error as an error tool result", ...)
test("closes every initialized client", ...)
```

Use fake clients only; no network is allowed in these tests.

Example marker test:

```js
assert.equal(
  expandHeaderValue("__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__", {
    KAGENT_CREDENTIAL_0123ABCD: "secret",
  }),
  "secret",
)
```

- [ ] **Step 2: Run the Node test and confirm RED**

From `go/`:

```bash
node --test harness/pi/extensions/kagent-mcp-core.test.mjs
```

Expected: module-not-found because `kagent-mcp-core.mjs` does not exist yet.

- [ ] **Step 3: Implement the dependency-injected core**

`expandHeaderValue` accepts either a literal or the exact marker regex:

```js
/^__KAGENT_ENV\[([A-Z0-9_]+)\]__$/
```

If a marker references a missing/empty env var, throw before connecting.

`initializeMcpBridge` must:

1. Create one client per configured server.
2. Call `listTools()` before registering anything.
3. If `enabled_tools` is non-empty, require every requested name to exist.
4. Build a global tool-name index and throw on any duplicate exposed name.
5. Register each selected tool with its exact raw `inputSchema` object.
6. Forward `execute(args, signal)` to `client.callTool(name, args, signal)`.
7. Return `{ close }`, where `close()` closes all successfully initialized clients in reverse initialization order and is idempotent.
8. On initialization failure, close clients already opened before rethrowing.

- [ ] **Step 4: Run the core tests and confirm GREEN**

```bash
node --test harness/pi/extensions/kagent-mcp-core.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Implement the thin real Pi extension entrypoint**

`kagent-mcp.ts` imports:

```ts
import { readFile } from "node:fs/promises";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { Type } from "typebox";
import { initializeMcpBridge, mcpResultToPi } from "./kagent-mcp-core.mjs";
```

The real client adapter is:

```ts
async function createClient(server) {
  const client = new Client({ name: "kagent-pi", version: "1" }, { capabilities: {} });
  const transport = new StreamableHTTPClientTransport(new URL(server.url), {
    requestInit: { headers: server.headers },
  });
  await client.connect(transport);
  return {
    async listTools() {
      const result = await client.listTools();
      return result.tools;
    },
    async callTool(name, args, signal) {
      return client.callTool({ name, arguments: args }, undefined, { signal });
    },
    async close() {
      await client.close();
    },
  };
}
```

The default export reads `process.env.KAGENT_PI_MCP_CONFIG`, parses `{servers:[...]}`, calls `initializeMcpBridge`, and bridges registrations with:

```ts
pi.registerTool({
  name: tool.name,
  label: tool.name,
  description: tool.description ?? tool.name,
  parameters: Type.Unsafe(tool.inputSchema),
  async execute(_toolCallId, params, signal) {
    return tool.execute(params, signal);
  },
});
```

Register an idempotent `session_shutdown` handler that calls `bridge.close()`.

- [ ] **Step 6: Add exact extension runtime dependencies**

Create:

```json
{
  "name": "kagent-pi-mcp-extension",
  "private": true,
  "type": "module",
  "dependencies": {
    "@modelcontextprotocol/sdk": "1.25.2",
    "typebox": "1.3.7"
  }
}
```

Do not add runtime package installation logic to the Actor.

- [ ] **Step 7: Wire the extension into the image and execute its unit tests during image build**

In `go/harness/pi/Dockerfile`, before switching to UID 65532:

```dockerfile
RUN mkdir -p /usr/local/lib/kagent-pi/extensions
COPY harness/pi/extensions/package.json /usr/local/lib/kagent-pi/package.json
RUN npm install --prefix /usr/local/lib/kagent-pi --omit=dev --ignore-scripts --no-audit --no-fund --package-lock=false
COPY harness/pi/extensions/kagent-mcp-core.mjs /usr/local/lib/kagent-pi/extensions/kagent-mcp-core.mjs
COPY harness/pi/extensions/kagent-mcp-core.test.mjs /usr/local/lib/kagent-pi/extensions/kagent-mcp-core.test.mjs
COPY harness/pi/extensions/kagent-mcp.ts /usr/local/lib/kagent-pi/extensions/kagent-mcp.ts
RUN node --test /usr/local/lib/kagent-pi/extensions/kagent-mcp-core.test.mjs \
    && rm /usr/local/lib/kagent-pi/extensions/kagent-mcp-core.test.mjs
```

After dependency install and file copy:

```dockerfile
RUN chown -R root:root /usr/local/lib/kagent-pi \
    && chmod -R go-w /usr/local/lib/kagent-pi
```

The non-root Actor must not be able to modify the bundled extension or dependency tree.

- [ ] **Step 8: Build the Pi image**

```bash
make build-pi-harness
```

Expected: Docker build succeeds, Pi version/help checks pass, and the Node MCP core tests pass during image build.

- [ ] **Step 9: Commit Task 4**

```bash
git add go/harness/pi/extensions go/harness/pi/Dockerfile
git commit -s -m "feat(pi-harness): add MCP extension"
```

---

### Task 5: Add real Pi MCP E2E coverage matching the native Harness tests

**Files:**
- Create: `go/core/test/e2e/mocks/invoke_pi_mcp.json`
- Modify: `go/core/test/e2e/pi_interaction_test.go`

**Interfaces:**
- Reuse existing kagent mock MCP server fixture and Pi `createPiMockTemplate`/stream helpers.
- New helper may be introduced only if it removes repeated fixture setup, e.g.:

```go
func createPiMCPTemplate(t *testing.T, modelURL, mcpURL string, selectedTools []string, headers []v1alpha3.ValueRef) string
```

- [ ] **Step 1: Write the RED E2E test**

Add:

```go
func TestE2EPiMCPTool(t *testing.T)
```

The test must:

1. Start the existing mock LLM with `invoke_pi_mcp.json`.
2. Start/reuse the existing Streamable HTTP MCP fixture that exposes `add_numbers`.
3. Create `RemoteMCPServer` and `AgentTemplate` binding it to Pi.
4. Select only `add_numbers`.
5. Send `Add 3 and 5 using the configured MCP server.` through public A2A streaming.
6. Require final text `PI_MCP_DONE result is 8`.
7. Require at least one streamed `function_call` and matching `function_response` for `add_numbers` with the same call ID.
8. Fetch the persisted task and require the same call/response pair is present there.

The mock LLM response should use OpenAI Chat Completions tool-call format with tool name `add_numbers` and arguments `{"a":3,"b":5}`; the second mock response matches the tool result and returns the final text.

- [ ] **Step 2: Add selected-tool fail-closed coverage**

Add:

```go
func TestE2EPiMCPSelectedTools(t *testing.T)
```

Bind a server exposing multiple tools but select only `add_numbers`. The test must verify the model-facing tool list does not contain an unselected tool by making the mock request match only when `add_numbers` is present and the excluded tool is absent, using the existing mock-server matching capabilities. If the mock server cannot express negative tool-list matching, add an extension-core unit test for exact filtering and keep this E2E focused on the selected tool succeeding.

- [ ] **Step 3: Add Secret-backed header E2E coverage**

Add:

```go
func TestE2EPiMCPSecretHeader(t *testing.T)
```

Use the existing authenticated MCP fixture pattern from Codex/Claude. The `RemoteMCPServer.headersFrom` should reference a Kubernetes Secret, and the MCP fixture must reject requests without the expected header. After task completion, inspect only generated non-secret configuration surfaces available to the test; do not log Secret values.

- [ ] **Step 4: Run targeted E2E in the authoritative test environment**

After the standard kagent E2E cluster/registry/Substrate setup is active:

```bash
go test ./core/test/e2e -run 'TestE2EPi(MCPTool|MCPSelectedTools|MCPSecretHeader)$' -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Commit Task 5**

```bash
git add go/core/test/e2e/mocks/invoke_pi_mcp.json go/core/test/e2e/pi_interaction_test.go
git commit -s -m "test(pi-harness): cover MCP resources end to end"
```

---

### Task 6: Document only verified MCP/skill support and run final verification

**Files:**
- Modify: `go/harness/pi/README.md`
- Modify: PR #1 body only after verification evidence exists.

**Interfaces:** None; documentation must match tested behavior exactly.

- [ ] **Step 1: Run formatting/static source checks**

From repository root:

```bash
gofmt -w go/harness/pi/config/config.go go/harness/pi/config/config_test.go \
  go/harness/pi/internal/adapter/adapter.go go/harness/pi/internal/adapter/adapter_test.go \
  go/harness/pi/internal/driver/process.go go/harness/pi/internal/driver/process_test.go \
  go/core/test/e2e/pi_interaction_test.go
```

Then:

```bash
git diff --check
```

Expected: no output from `git diff --check`.

- [ ] **Step 2: Run all focused tests fresh**

```bash
cd go
go test ./harness/pi/... -count=1
node --test harness/pi/extensions/kagent-mcp-core.test.mjs
cd ..
```

Expected: all commands exit 0.

- [ ] **Step 3: Build the actual runtime image fresh**

```bash
make build-pi-harness
```

Expected: exit 0 including Pi version/help and MCP core test checks.

- [ ] **Step 4: Run targeted Pi E2E fresh**

In the repository's authoritative E2E environment:

```bash
cd go
go test ./core/test/e2e -run 'TestE2EPi' -count=1 -v
cd ..
```

Expected: all Pi E2E tests pass, including streaming/resume, checkpoint/fork, built-in bash, and MCP.

- [ ] **Step 5: Update README only with capabilities proven above**

Add support bullets for:

```text
- compiler-owned Agent Plugin/Skill resources materialized under /data/pi/skills
- direct RemoteMCPServer Streamable HTTP tools
- selected-tool filtering
- literal, ConfigMap, and Secret-backed MCP headers
- MCP cancellation through Pi tool AbortSignal
```

Keep limitations for:

```text
- SSE MCP
- stdio MCP
- MCP approval/HITL
- custom MCP TLS/runtime overrides
- AgentPlugin-declared MCP servers
- Shared/Dedicated child agents
- arbitrary user Pi extensions
```

- [ ] **Step 6: Re-run diff sanity against upstream**

```bash
git diff --check
git diff main...HEAD -- Makefile .github/workflows/ci.yaml
```

Expected: shared-file diffs remain limited to Pi build/E2E wiring; no unrelated edits.

- [ ] **Step 7: Commit documentation**

```bash
git add go/harness/pi/README.md
git commit -s -m "docs(pi-harness): document MCP resources"
```

- [ ] **Step 8: Update draft PR status with verification evidence**

Record the exact fresh commands and outcomes. If any authoritative E2E or image build cannot be executed, leave the PR draft and state that limitation explicitly rather than claiming completion.

---

## Self-Review

- Spec coverage: config mapping, skills, direct Streamable HTTP MCP, credentials, collision behavior, explicit extension isolation, cancellation, A2A events, and E2E all have tasks.
- Out-of-scope transports/approval/TLS/AgentPlugin MCP are explicitly rejected rather than silently ignored.
- Type consistency: `MCPServer`, `SkillResources`, `ExtensionPaths`, `SkillPaths`, and `KAGENT_PI_MCP_CONFIG` are defined once and consumed by later tasks with the same names.
- No public CRD/API change is introduced.
- The plan does not require ambient Pi extension/skill discovery or runtime npm installation.
