#!/usr/bin/env node

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { registerDeviceTools } from "./tools/devices.js";
import { registerClusterTools } from "./tools/clusters.js";
import { apiCall, Device, Cluster } from "./client.js";

const server = new McpServer({
  name: "gpu-cluster",
  version: "0.1.0",
});

// Register tools
registerDeviceTools(server);
registerClusterTools(server);

// Register resources
server.resource(
  "device-list",
  "cluster://devices",
  { description: "All devices in the Tailscale network with GPU info" },
  async () => {
    const devices = await apiCall<Device[]>("/api/devices");
    return {
      contents: [
        {
          uri: "cluster://devices",
          mimeType: "application/json",
          text: JSON.stringify(devices, null, 2),
        },
      ],
    };
  },
);

server.resource(
  "cluster-list",
  "cluster://clusters",
  { description: "All clusters with status" },
  async () => {
    const clusters = await apiCall<Cluster[]>("/api/clusters");
    return {
      contents: [
        {
          uri: "cluster://clusters",
          mimeType: "application/json",
          text: JSON.stringify(clusters, null, 2),
        },
      ],
    };
  },
);

async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("GPU Cluster MCP server running on stdio");
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
