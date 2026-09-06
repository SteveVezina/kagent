import assert from "node:assert/strict";
import test from "node:test";

import { initializeMcpBridge } from "./kagent-mcp-core.mjs";

test("namespaces native MCP tools while calling the original MCP tool name", async () => {
  const registered = [];
  let calledName;
  const bridge = await initializeMcpBridge({
    config: {
      servers: [{
        name: "math-server",
        url: "https://math.example/mcp",
        enabled_tools: ["add_numbers"],
      }],
    },
    env: {},
    createClient: async () => ({
      async listTools() {
        return [{
          name: "add_numbers",
          description: "add",
          inputSchema: { type: "object", properties: {} },
        }];
      },
      async callTool(name) {
        calledName = name;
        return { content: [{ type: "text", text: "8" }] };
      },
      async close() {},
    }),
    registerTool: (tool) => registered.push(tool),
  });

  assert.equal(registered.length, 1);
  assert.equal(registered[0].name, "mcp__math_server__add_numbers");
  await registered[0].execute({}, new AbortController().signal);
  assert.equal(calledName, "add_numbers");
  await bridge.close();
});
