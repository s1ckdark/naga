import { z } from "zod";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { apiCall, Cluster, ExecuteResponse } from "../client.js";

export function registerClusterTools(server: McpServer) {
  server.tool(
    "list_clusters",
    "List all clusters with their status, head node, and worker count",
    {},
    async () => {
      const clusters = await apiCall<Cluster[]>("/api/clusters");
      return { content: [{ type: "text" as const, text: JSON.stringify(clusters, null, 2) }] };
    },
  );

  server.tool(
    "get_cluster",
    "Get detailed information about a specific cluster",
    { id: z.string().describe("Cluster ID or name") },
    async ({ id }) => {
      const cluster = await apiCall<Cluster>(`/api/clusters/${encodeURIComponent(id)}`);
      return { content: [{ type: "text" as const, text: JSON.stringify(cluster, null, 2) }] };
    },
  );

  server.tool(
    "create_cluster",
    "Create a new cluster with a head node and optional worker nodes",
    {
      name: z.string().describe("Cluster name"),
      head_id: z.string().describe("Head node device ID"),
      worker_ids: z.array(z.string()).optional().describe("Worker node device IDs"),
    },
    async ({ name, head_id, worker_ids }) => {
      const cluster = await apiCall<Cluster>("/api/clusters", {
        method: "POST",
        body: { name, head_id, worker_ids: worker_ids || [] },
      });
      return { content: [{ type: "text" as const, text: JSON.stringify(cluster, null, 2) }] };
    },
  );

  server.tool(
    "delete_cluster",
    "Delete a cluster",
    {
      id: z.string().describe("Cluster ID"),
      force: z.boolean().optional().describe("Force deletion even if running"),
    },
    async ({ id, force }) => {
      const params = force ? "?force=true" : "";
      const result = await apiCall<{ status: string }>(
        `/api/clusters/${encodeURIComponent(id)}${params}`,
        { method: "DELETE" },
      );
      return { content: [{ type: "text" as const, text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "execute_on_cluster",
    "Execute a command on all worker nodes in a cluster in parallel and return results from each worker",
    {
      cluster_id: z.string().describe("Cluster ID"),
      command: z.string().describe("Command to execute on all workers"),
      timeout_seconds: z.number().optional().describe("Timeout per worker in seconds (default: 30, max: 300)"),
    },
    async ({ cluster_id, command, timeout_seconds }) => {
      const to = timeout_seconds || 30;
      const result = await apiCall<ExecuteResponse>(
        `/api/clusters/${encodeURIComponent(cluster_id)}/execute`,
        { method: "POST", body: { command, timeout_seconds: to }, timeout: to * 1000 + 5000 },
      );
      return { content: [{ type: "text" as const, text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "get_cluster_health",
    "Get health status of all nodes in a cluster",
    { id: z.string().describe("Cluster ID") },
    async ({ id }) => {
      const health = await apiCall<unknown>(`/api/clusters/${encodeURIComponent(id)}/health`);
      return { content: [{ type: "text" as const, text: JSON.stringify(health, null, 2) }] };
    },
  );
}
