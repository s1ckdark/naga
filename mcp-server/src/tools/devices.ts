import { z } from "zod";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { apiCall, Device, TaskResult } from "../client.js";

export function registerDeviceTools(server: McpServer) {
  server.tool(
    "list_devices",
    "List all devices in the Tailscale network with GPU status",
    {
      refresh: z.boolean().optional().describe("Force refresh from Tailscale API"),
      gpu_only: z.boolean().optional().describe("Only return devices with GPUs"),
    },
    async ({ refresh, gpu_only }) => {
      const params = refresh ? "?refresh=true" : "";
      let devices = await apiCall<Device[]>(`/api/devices${params}`);
      if (gpu_only) {
        devices = devices.filter((d) => d.hasGpu);
      }
      return { content: [{ type: "text" as const, text: JSON.stringify(devices, null, 2) }] };
    },
  );

  server.tool(
    "get_device",
    "Get details of a specific device by ID or name",
    { id: z.string().describe("Device ID or name") },
    async ({ id }) => {
      const device = await apiCall<Device>(`/api/devices/${encodeURIComponent(id)}`);
      return { content: [{ type: "text" as const, text: JSON.stringify(device, null, 2) }] };
    },
  );

  server.tool(
    "get_gpu_status",
    "Get real-time GPU status (utilization, memory, temperature) from all GPU nodes or a specific device",
    {
      device_id: z.string().optional().describe("Specific device ID (all GPU nodes if omitted)"),
    },
    async ({ device_id }) => {
      const nvsmiCmd = "nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits";

      if (device_id) {
        const result = await apiCall<TaskResult>(
          `/api/devices/${encodeURIComponent(device_id)}/execute`,
          { method: "POST", body: { command: nvsmiCmd, timeout_seconds: 10 } },
        );
        return { content: [{ type: "text" as const, text: JSON.stringify(result, null, 2) }] };
      }

      const devices = await apiCall<Device[]>("/api/devices");
      const gpuDevices = devices.filter((d) => d.hasGpu && d.status === "online");

      if (gpuDevices.length === 0) {
        return { content: [{ type: "text" as const, text: "No online GPU devices found" }] };
      }

      const results = await Promise.all(
        gpuDevices.map(async (d) => {
          try {
            return await apiCall<TaskResult>(
              `/api/devices/${encodeURIComponent(d.id)}/execute`,
              { method: "POST", body: { command: nvsmiCmd, timeout_seconds: 10 } },
            );
          } catch (e) {
            return { deviceId: d.id, deviceName: d.name, gpu: `${d.gpuCount}x ${d.gpuModel}`, output: "", error: String(e), durationMs: 0 };
          }
        }),
      );
      return { content: [{ type: "text" as const, text: JSON.stringify(results, null, 2) }] };
    },
  );

  server.tool(
    "execute_on_device",
    "Execute a command on a specific device via SSH",
    {
      device_id: z.string().describe("Device ID"),
      command: z.string().describe("Command to execute"),
      timeout_seconds: z.number().optional().describe("Timeout in seconds (default: 30, max: 300)"),
    },
    async ({ device_id, command, timeout_seconds }) => {
      const result = await apiCall<TaskResult>(
        `/api/devices/${encodeURIComponent(device_id)}/execute`,
        { method: "POST", body: { command, timeout_seconds: timeout_seconds || 30 } },
      );
      return { content: [{ type: "text" as const, text: JSON.stringify(result, null, 2) }] };
    },
  );
}
