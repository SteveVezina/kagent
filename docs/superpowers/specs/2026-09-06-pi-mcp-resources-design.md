# Pi Harness MCP and Resources Design

## Status

Proposed design for the `feat/pi-harness` prototype. This design keeps Pi selected through `spec.byo` and does not add a public `spec.pi` Harness variant.

## Goal

Add kagent-managed skills and remote MCP tools to the Pi Harness while preserving the same compiler ownership, credential isolation, egress accounting, runtime isolation, and A2A tool-event semantics used by the native Codex and Claude Harness adapters.

## Constraints

- Keep the current BYO Harness selection for this prototype.
- Do not add public CRD fields for Pi.
- Keep Pi `0.85.0` pinned until the runtime pin is intentionally updated.
- Preserve `--no-extensions`, `--no-skills`, `--no-context-files`, `--no-prompt-templates`, and `--no-themes` so ambient workspace or user resources cannot affect compiled behavior.
- Only compiler-owned extensions and skills may be explicitly loaded.
- Support remote MCP over Streamable HTTP only in this increment.
- Reject SSE and stdio MCP transports.
- Reject unsupported MCP TLS behavior rather than silently ignoring it.
- Do not persist secret values in Pi configuration or durable state.
- Do not claim MCP or skill capabilities until corresponding conformance/E2E tests pass.

## Existing Architecture

The current Pi prototype receives the BYO compiler's `adk.AgentConfig` through `KAGENT_CONFIG_JSON`, converts the supported subset into a strict versioned Pi runtime configuration, and starts `pi --mode rpc` behind kagent's shared private A2A runtime.

The BYO `adk.HttpMcpServerConfig` retains the MCP URL, headers, selected tools, timeout and approval-related fields, but it does not retain the original `RemoteMCPServer` Kubernetes object name. This means the Pi BYO bridge cannot reproduce the native Codex server namespace exactly.

The design therefore treats missing server identity as an explicit prototype limitation rather than inventing a lossy namespace.

## Architecture

```text
AgentTemplate
    |
    +-- Skills / AgentPlugin resources
    |       |
    |       v
    |  existing BYO compiler
    |       |
    |       v
    |  Pi runtime config
    |       |
    |       v
    |  agentplugins.Materialize
    |       |
    |       v
    |  /data/pi/skills
    |       |
    |       v
    |  pi --no-skills --skill <compiler-owned-path>
    |
    +-- RemoteMCPServer bindings
            |
            v
       existing BYO compiler
            |
            v
       Pi runtime config
            |
            v
       /data/pi/mcp.json
            |
            v
       bundled kagent Pi MCP extension
            |
            +-- connect Streamable HTTP
            +-- expand environment-backed headers
            +-- discover server tools
            +-- enforce selected-tool filters
            +-- reject duplicate exposed tool names
            +-- pi.registerTool(...)
```

The bundled MCP extension is part of the Pi Harness image and is loaded explicitly with Pi's `-e` flag. Ambient extension discovery remains disabled.

## Versioned Runtime Configuration

The Pi runtime config gains two compiler-owned sections:

```go
type Config struct {
    // existing fields...
    SkillResources *agentplugin.Resources `json:"skill_resources,omitempty"`
    MCPServers     []MCPServer            `json:"mcp_servers,omitempty"`
}

type MCPServer struct {
    URL          string            `json:"url"`
    Headers      map[string]string `json:"headers,omitempty"`
    EnabledTools []string          `json:"enabled_tools,omitempty"`
}
```

`MCPServers` is a list rather than a map because the BYO `AgentConfig` no longer contains the original Kubernetes server name. Ordering is normalized deterministically by URL plus selected tools before serialization/materialization.

Validation rules:

- `URL` must be an absolute HTTP(S) URL with no embedded credentials or fragment.
- Duplicate server definitions after normalization are rejected.
- `EnabledTools` is sorted and deduplicated.
- Empty enabled-tool names are rejected.
- SSE and stdio entries remain rejected by the BYO-to-Pi mapper.
- `RequireApproval` remains rejected until Pi approval semantics are mapped to kagent's A2A approval flow.
- Non-default timeout, SSE-read-timeout, terminate-on-close and custom TLS fields remain rejected unless their semantics are implemented exactly.

## MCP Header Handling

The existing BYO compiler already materializes literal and ConfigMap-backed MCP headers into `adk.AgentConfig` and encodes Secret-backed values as environment references.

The Pi mapper preserves those references instead of resolving them into `mcp.json`.

The generated MCP configuration therefore contains values such as:

```json
{
  "Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__"
}
```

The bundled Pi extension expands only this exact compiler-owned marker format at runtime using `process.env`. Missing referenced variables cause the MCP server to fail registration before an agent turn begins.

Literal header values may be written to the generated private configuration because they are already explicit non-Secret configuration. Secret values must never be written to disk.

## MCP Tool Registration

At Pi startup, the bundled extension performs the following sequence for every compiled server:

1. Validate and expand environment-backed headers.
2. Connect using MCP Streamable HTTP transport.
3. Call `tools/list`.
4. If `EnabledTools` is non-empty, retain only those exact tool names and fail startup if any requested tool is absent.
5. Convert each MCP JSON Schema into a Pi-compatible TypeBox schema without changing the schema semantics.
6. Register each tool through `pi.registerTool()`.
7. On execution, call the corresponding MCP `tools/call` operation and convert MCP content into Pi tool-result content.
8. Close MCP clients during Pi session shutdown.

The extension owns no model configuration and does not modify prompts or session history.

## Tool Identity and Collision Policy

Because BYO loses the original server name, this prototype exposes each MCP tool using its original MCP tool name.

Before registering tools, the extension builds a global tool-name index. If two configured servers expose the same selected tool name, startup fails with an error identifying both server URLs and the conflicting tool name.

This is intentionally stricter than Codex. It prevents silent shadowing, unstable ordering, or invented names that would change the AgentTemplate's expected tool semantics.

A future first-class Pi compiler can preserve `RemoteMCPServer` names and introduce deterministic namespacing without changing the underlying MCP client implementation.

## Skill Resources

Skills use kagent's existing `agentplugins.Materialize` implementation, matching the native Harness adapters.

The adapter creates and reconciles compiler-owned directories under:

```text
/data/pi/packages
/data/pi/skills
```

The current compiled `SkillResources` is materialized into those paths. Stale compiler-owned skills are removed when the runtime revision changes.

Pi continues to start with ambient skills disabled. The driver explicitly supplies the materialized compiler-owned skill paths with `--skill` arguments. No workspace `.pi` or global skill directory participates in discovery.

If materialization fails, the Actor fails readiness rather than starting without the requested skills.

## Generated Extension and Configuration

The Harness image contains a static extension source at a fixed image path such as:

```text
/usr/local/lib/kagent-pi/extensions/kagent-mcp.ts
```

The adapter writes the revision-specific non-secret MCP configuration to:

```text
/data/pi/mcp.json
```

and sets a compiler-owned environment variable pointing to it:

```text
KAGENT_PI_MCP_CONFIG=/data/pi/mcp.json
```

The driver adds exactly one explicit extension argument when MCP servers are configured:

```text
--no-extensions -e /usr/local/lib/kagent-pi/extensions/kagent-mcp.ts
```

When no MCP server is configured, the extension is not loaded.

The extension file itself is immutable image content. Only `mcp.json` is revision-specific durable configuration.

## Runtime Dependencies

The Pi Harness image adds a pinned MCP SDK dependency compatible with the Pi 0.85.0 runtime. The dependency is installed during image build and must be exact-version pinned in the image build rather than dynamically installed at Actor startup.

No npm or network package installation is allowed during Actor startup.

## Security and Isolation

- Pi ambient extensions and skills remain disabled.
- Only the image-bundled MCP extension is explicitly loaded.
- Only compiler-materialized skill paths are explicitly loaded.
- The generated `mcp.json` uses the existing private-file helper and mode `0600`.
- Secret-backed headers remain process-environment values.
- MCP URLs contribute to the compiled revision's egress destinations through the existing BYO compiler path.
- The MCP extension may connect only to URLs present in compiler-owned configuration.
- No shell command is used to resolve headers or MCP configuration.
- MCP clients are scoped to the Pi process/session and closed during shutdown.
- Unsupported TLS and approval semantics fail closed.

## A2A Event Semantics

The existing Pi RPC driver already translates Pi tool lifecycle events into the shared kagent runtime's function-call/function-response events.

MCP tools use normal Pi registered-tool execution, so they flow through the same event translator as Pi's built-in `bash`, `read`, `write`, and `edit` tools.

No MCP-specific A2A path is added.

For a successful MCP call, public A2A must preserve:

```text
working
  -> function_call(tool_name, call_id, args)
  -> function_response(tool_name, call_id, result)
  -> assistant artifact(s)
  -> exactly one terminal status
```

The call/result correlation ID must remain the Pi tool-call ID generated for that model turn.

## Error Handling

The following fail before serving an agent turn:

- invalid MCP URL;
- unsupported transport or runtime options;
- missing Secret-backed header environment variable;
- requested tool missing from `tools/list`;
- duplicate exposed tool names across servers;
- invalid MCP tool JSON Schema that cannot be represented safely;
- failure to initialize the MCP client;
- skill materialization failure.

A failure during `tools/call` is returned as a Pi tool error/result so the model observes a normal failed tool invocation. The A2A task is not automatically failed solely because a tool returned an application-level error; terminal task state follows Pi's final settled agent outcome.

Cancellation propagates through the existing Pi RPC abort/process-group shutdown path. The MCP tool implementation receives Pi's abort signal and aborts its in-flight SDK request where supported.

## Testing Strategy

### Config tests

Add RED/GREEN tests proving:

- `SkillResources` and `MCPServers` survive strict config round-trip.
- Streamable HTTP MCP maps from BYO `HttpTools`.
- selected tools are sorted/deduplicated.
- Secret environment markers are preserved.
- SSE, stdio, approvals, unsupported TLS and unsupported runtime options fail closed.
- duplicate normalized server definitions and invalid URLs are rejected.

### Adapter tests

Prove:

- skills materialize only under `/data/pi/skills` and `/data/pi/packages`;
- stale generated skills are reconciled;
- `mcp.json` is mode `0600`;
- secret values do not appear in generated files;
- the driver receives only explicit compiler-owned extension and skill paths.

### Extension tests

Use a local test MCP server to prove:

- headers are expanded from the environment;
- selected-tool filtering is exact;
- missing selected tools fail initialization;
- duplicate names across servers fail initialization;
- MCP JSON Schema is passed to the registered Pi tool without semantic weakening;
- `tools/call` results and errors are converted into valid Pi tool results;
- cancellation aborts an in-flight MCP call.

### E2E

Mirror the existing Codex resource test using kagent's real MCP fixture:

1. Start the existing mock LLM and MCP server fixtures.
2. Create an AgentTemplate with one remote Streamable HTTP MCP binding.
3. Run the real Pi Harness image.
4. Make the model request the selected MCP tool.
5. Assert the Pi task completes with the expected result.
6. Assert both streamed and persisted A2A contain a correlated `function_call` and `function_response` for the MCP tool.
7. Add a selected-tool case proving an unselected server tool is unavailable.
8. Add a Secret-backed header case proving authenticated MCP works without persisting the credential.

Capabilities are updated only after these tests pass in the repository's authoritative E2E environment.

## Out of Scope

This increment does not add:

- a public `spec.pi` Harness variant;
- a dedicated Pi compiler;
- SSE MCP;
- stdio MCP;
- MCP approval/HITL mapping;
- custom MCP TLS configuration;
- dynamic MCP server discovery outside compiled AgentTemplate bindings;
- Shared or Dedicated child-agent tools;
- arbitrary user Pi extensions;
- prompt templates or themes from Agent Plugins.

## Future First-Class Pi Compiler

Once maintainers approve Pi as a built-in Harness, the Pi compiler should consume `ResolvedMCPTool` directly, as Codex and Claude do. It can then preserve `RemoteMCPServer` identity, own credential-environment names, provenance, warnings and egress generation directly, and emit the same versioned Actor-side `Config` used by this prototype.

The Actor-side MCP extension and skill materialization paths should remain reusable. Moving from BYO to a first-class compiler should therefore be a compiler/API change, not a rewrite of Pi's runtime integration.
