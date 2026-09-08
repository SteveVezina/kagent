import assert from "node:assert/strict";
import test from "node:test";

import {
  expandHeaderValue,
  initializeMcpBridge,
  mcpResultToPi,
} from "./kagent-mcp-core.mjs";

test("expands compiler-owned environment header marker", () => {
  assert.equal(
    expandHeaderValue("__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__", {
      KAGENT_CREDENTIAL_0123ABCD: "secret",
    }),
    "secret",
  );
  assert.equal(expandHeaderValue("Bearer literal", {}), "Bearer literal");
});

test("rejects missing environment header", () => {
  assert.throws(
    () => expandHeaderValue("__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__", {}),
    /KAGENT_CREDENTIAL_0123ABCD/,
  );
});

test("filters exactly the selected MCP tools", async () => {
  const registered = [];
  const bridge = await initializeMcpBridge({
    config: {
      servers: [{ url: "https://one.example/mcp", enabled_tools: ["add_numbers"] }],
    },
    env: {},
    createClient: async () => fakeClient([
      tool("add_numbers"),
      tool("subtract_numbers"),
    ]),
    registerTool: (value) => registered.push(value),
  });

  assert.deepEqual(registered.map((value) => value.name), ["add_numbers"]);
  await bridge.close();
});

test("rejects a requested tool missing from tools/list", async () => {
  let closed = false;
  await assert.rejects(
    initializeMcpBridge({
      config: {
        servers: [{ url: "https://one.example/mcp", enabled_tools: ["missing"] }],
      },
      env: {},
      createClient: async () => fakeClient([tool("add_numbers")], {
        close: async () => { closed = true; },
      }),
      registerTool: () => assert.fail("must not register tools"),
    }),
    /requested tool "missing".*not exposed/,
  );
  assert.equal(closed, true);
});

test("rejects duplicate exposed tool names across servers", async () => {
  let closes = 0;
  await assert.rejects(
    initializeMcpBridge({
      config: {
        servers: [
          { url: "https://one.example/mcp" },
          { url: "https://two.example/mcp" },
        ],
      },
      env: {},
      createClient: async () => fakeClient([tool("lookup")], {
        close: async () => { closes++; },
      }),
      registerTool: () => assert.fail("must not register tools"),
    }),
    /duplicate MCP tool "lookup".*one\.example.*two\.example/,
  );
  assert.equal(closes, 2);
});

test("expands headers before creating the MCP client", async () => {
  let observed;
  const bridge = await initializeMcpBridge({
    config: {
      servers: [{
        url: "https://one.example/mcp",
        headers: {
          Authorization: "__KAGENT_ENV[KAGENT_CREDENTIAL_0123ABCD]__",
          "X-Tenant": "public",
        },
      }],
    },
    env: { KAGENT_CREDENTIAL_0123ABCD: "Bearer secret" },
    createClient: async (server) => {
      observed = server;
      return fakeClient([]);
    },
    registerTool: () => {},
  });

  assert.deepEqual(observed.headers, {
    Authorization: "Bearer secret",
    "X-Tenant": "public",
  });
  await bridge.close();
});

test("forwards MCP arguments and abort signal to tools/call", async () => {
  const registered = [];
  let observed;
  const bridge = await initializeMcpBridge({
    config: { servers: [{ url: "https://one.example/mcp" }] },
    env: {},
    createClient: async () => fakeClient([tool("lookup")], {
      callTool: async (name, args, signal) => {
        observed = { name, args, signal };
        return { content: [{ type: "text", text: "done" }] };
      },
    }),
    registerTool: (value) => registered.push(value),
  });

  const controller = new AbortController();
  const result = await registered[0].execute({ query: "kagent" }, controller.signal);
  assert.deepEqual(observed, {
    name: "lookup",
    args: { query: "kagent" },
    signal: controller.signal,
  });
  assert.deepEqual(result.content, [{ type: "text", text: "done" }]);
  await bridge.close();
});

test("forwards configured timeout to tools/list and tools/call", async () => {
  const registered = [];
  const observed = {};
  const bridge = await initializeMcpBridge({
    config: { servers: [{ url: "https://one.example/mcp", timeout_seconds: 12.5 }] },
    env: {},
    createClient: async () => fakeClient([tool("lookup")], {
      listTools: async (timeoutSeconds) => {
        observed.list = timeoutSeconds;
        return [tool("lookup")];
      },
      callTool: async (name, args, signal, timeoutSeconds) => {
        observed.call = timeoutSeconds;
        return { content: [{ type: "text", text: "done" }] };
      },
    }),
    registerTool: (value) => registered.push(value),
  });

  await registered[0].execute({}, new AbortController().signal);
  assert.deepEqual(observed, { list: 12.5, call: 12.5 });
  await bridge.close();
});

test("converts MCP text and image content to Pi tool content", () => {
  const result = mcpResultToPi({
    content: [
      { type: "text", text: "hello" },
      { type: "image", data: "aGVsbG8=", mimeType: "image/png" },
    ],
    structuredContent: { value: 8 },
  });

  assert.deepEqual(result, {
    content: [
      { type: "text", text: "hello" },
      { type: "image", data: "aGVsbG8=", mimeType: "image/png" },
    ],
    details: { structuredContent: { value: 8 } },
    isError: false,
  });
});

test("preserves MCP application error as an error tool result", () => {
  const result = mcpResultToPi({
    isError: true,
    content: [{ type: "text", text: "permission denied" }],
  });
  assert.equal(result.isError, true);
  assert.equal(result.content[0].text, "permission denied");
});

test("rejects MCP content Pi cannot represent faithfully", () => {
  assert.throws(
    () => mcpResultToPi({ content: [{ type: "audio", data: "AA==", mimeType: "audio/wav" }] }),
    /unsupported MCP content type "audio"/,
  );
});

test("closes every initialized client in reverse order and is idempotent", async () => {
  const order = [];
  const bridge = await initializeMcpBridge({
    config: {
      servers: [
        { url: "https://one.example/mcp" },
        { url: "https://two.example/mcp" },
      ],
    },
    env: {},
    createClient: async (server) => fakeClient([], {
      close: async () => order.push(server.url),
    }),
    registerTool: () => {},
  });

  await bridge.close();
  await bridge.close();
  assert.deepEqual(order, ["https://two.example/mcp", "https://one.example/mcp"]);
});

function tool(name) {
  return {
    name,
    description: `${name} description`,
    inputSchema: {
      type: "object",
      properties: { value: { type: "string" } },
      additionalProperties: false,
    },
  };
}

function fakeClient(tools, overrides = {}) {
  return {
    async listTools() { return tools; },
    async callTool(name, args, signal) {
      return { content: [{ type: "text", text: JSON.stringify({ name, args, aborted: signal?.aborted ?? false }) }] };
    },
    async close() {},
    ...overrides,
  };
}
