import { readFile } from "node:fs/promises";

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { Type } from "typebox";

import { initializeMcpBridge } from "./kagent-mcp-core.mjs";

const CONFIG_ENV = "KAGENT_PI_MCP_CONFIG";

export default async function kagentMcpExtension(pi: ExtensionAPI) {
  const configPath = process.env[CONFIG_ENV];
  if (!configPath) {
    throw new Error(`${CONFIG_ENV} is required when the kagent MCP extension is loaded`);
  }

  const config = JSON.parse(await readFile(configPath, "utf8"));
  const bridge = await initializeMcpBridge({
    config,
    env: process.env,
    createClient,
    registerTool(tool) {
      pi.registerTool({
        name: tool.name,
        label: tool.name,
        description: tool.description ?? tool.name,
        parameters: Type.Unsafe(tool.inputSchema),
        async execute(_toolCallId, params, signal) {
          return tool.execute(params, signal);
        },
      });
    },
  });

  pi.on("session_shutdown", async () => {
    await bridge.close();
  });
}

async function createClient(server) {
  const client = new Client(
    { name: "kagent-pi", version: "1" },
    { capabilities: {} },
  );
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
