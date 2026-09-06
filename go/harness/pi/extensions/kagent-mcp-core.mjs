const ENV_MARKER = /^__KAGENT_ENV\[([A-Z0-9_]+)\]__$/;

export function expandHeaderValue(value, env) {
  if (typeof value !== "string") {
    throw new TypeError("MCP header values must be strings");
  }
  const match = ENV_MARKER.exec(value);
  if (!match) {
    return value;
  }
  const expanded = env[match[1]];
  if (typeof expanded !== "string" || expanded.length === 0) {
    throw new Error(`MCP credential environment variable ${match[1]} is missing or empty`);
  }
  return expanded;
}

export function mcpResultToPi(result) {
  if (!result || !Array.isArray(result.content)) {
    throw new Error("MCP tools/call result must contain a content array");
  }
  const content = result.content.map((block) => {
    if (block?.type === "text" && typeof block.text === "string") {
      return { type: "text", text: block.text };
    }
    if (
      block?.type === "image" &&
      typeof block.data === "string" &&
      typeof block.mimeType === "string"
    ) {
      return { type: "image", data: block.data, mimeType: block.mimeType };
    }
    throw new Error(`unsupported MCP content type ${JSON.stringify(block?.type ?? null)}`);
  });

  const details = {};
  if (result.structuredContent !== undefined) {
    details.structuredContent = result.structuredContent;
  }
  return {
    content,
    details,
    isError: result.isError === true,
  };
}

export async function initializeMcpBridge({ config, env, createClient, registerTool }) {
  if (!config || !Array.isArray(config.servers)) {
    throw new Error("Pi MCP configuration must contain a servers array");
  }
  if (typeof createClient !== "function" || typeof registerTool !== "function") {
    throw new TypeError("Pi MCP bridge requires createClient and registerTool callbacks");
  }

  const clients = [];
  const registrations = [];
  const toolOrigins = new Map();

  try {
    for (const rawServer of config.servers) {
      const server = expandServerHeaders(rawServer, env ?? {});
      const client = await createClient(server);
      clients.push(client);

      const available = await client.listTools();
      if (!Array.isArray(available)) {
        throw new Error(`MCP server ${server.url} returned an invalid tools/list result`);
      }
      const toolsByName = new Map();
      for (const tool of available) {
        if (!tool || typeof tool.name !== "string" || tool.name.length === 0) {
          throw new Error(`MCP server ${server.url} exposed a tool without a valid name`);
        }
        if (toolsByName.has(tool.name)) {
          throw new Error(`MCP server ${server.url} exposed duplicate tool ${JSON.stringify(tool.name)}`);
        }
        if (!tool.inputSchema || typeof tool.inputSchema !== "object" || Array.isArray(tool.inputSchema)) {
          throw new Error(`MCP tool ${JSON.stringify(tool.name)} from ${server.url} has an invalid input schema`);
        }
        toolsByName.set(tool.name, tool);
      }

      const selectedNames = Array.isArray(server.enabled_tools) && server.enabled_tools.length > 0
        ? server.enabled_tools
        : [...toolsByName.keys()];
      for (const name of selectedNames) {
        if (!toolsByName.has(name)) {
          throw new Error(`MCP server ${server.url} requested tool ${JSON.stringify(name)} is not exposed by tools/list`);
        }
      }

      for (const name of selectedNames) {
        const previous = toolOrigins.get(name);
        if (previous) {
          throw new Error(`duplicate MCP tool ${JSON.stringify(name)} exposed by ${previous} and ${server.url}`);
        }
        toolOrigins.set(name, server.url);
        registrations.push({ server, client, tool: toolsByName.get(name) });
      }
    }

    for (const { client, tool } of registrations) {
      registerTool({
        name: tool.name,
        description: tool.description,
        inputSchema: tool.inputSchema,
        async execute(args, signal) {
          const converted = mcpResultToPi(await client.callTool(tool.name, args, signal));
          if (converted.isError) {
            throw new Error(mcpErrorText(tool.name, converted));
          }
          return { content: converted.content, details: converted.details };
        },
      });
    }
  } catch (error) {
    await closeClients(clients, true);
    throw error;
  }

  let closed = false;
  return {
    async close() {
      if (closed) {
        return;
      }
      closed = true;
      await closeClients(clients, false);
    },
  };
}

function expandServerHeaders(server, env) {
  if (!server || typeof server.url !== "string" || server.url.length === 0) {
    throw new Error("Pi MCP server URL is required");
  }
  const headers = {};
  for (const [name, value] of Object.entries(server.headers ?? {})) {
    headers[name] = expandHeaderValue(value, env);
  }
  return { ...server, headers };
}

function mcpErrorText(toolName, result) {
  const text = result.content
    .filter((block) => block.type === "text")
    .map((block) => block.text)
    .filter(Boolean)
    .join("\n")
    .trim();
  return text || `MCP tool ${toolName} returned an error`;
}

async function closeClients(clients, ignoreErrors) {
  const errors = [];
  for (let index = clients.length - 1; index >= 0; index--) {
    try {
      await clients[index].close();
    } catch (error) {
      errors.push(error);
    }
  }
  if (!ignoreErrors && errors.length > 0) {
    throw new AggregateError(errors, "failed to close Pi MCP clients");
  }
}
