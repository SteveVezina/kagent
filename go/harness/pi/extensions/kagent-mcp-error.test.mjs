import assert from "node:assert/strict";
import test from "node:test";

import { initializeMcpBridge } from "./kagent-mcp-core.mjs";

test("registered tool throws when MCP returns isError", async () => {
  const registered = [];
  const bridge = await initializeMcpBridge({
    config: { servers: [{ url: "https://one.example/mcp" }] },
    env: {},
    createClient: async () => ({
      async listTools() {
        return [{
          name: "deploy",
          description: "deploy",
          inputSchema: { type: "object", additionalProperties: false },
        }];
      },
      async callTool() {
        return {
          isError: true,
          content: [{ type: "text", text: "permission denied" }],
        };
      },
      async close() {},
    }),
    registerTool: (tool) => registered.push(tool),
  });

  await assert.rejects(
    registered[0].execute({}, new AbortController().signal),
    /permission denied/,
  );
  await bridge.close();
});
