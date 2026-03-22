/**
 * HTTP client for the Go cluster manager API (localhost:8080)
 */

const BASE_URL = process.env.CLUSTER_API_URL || "http://localhost:8080";

interface RequestOptions {
  method?: string;
  body?: unknown;
  timeout?: number;
}

export async function apiCall<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const { method = "GET", body, timeout = 30000 } = opts;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);

  try {
    const response = await fetch(`${BASE_URL}${path}`, {
      method,
      headers: body ? { "Content-Type": "application/json" } : undefined,
      body: body ? JSON.stringify(body) : undefined,
      signal: controller.signal,
    });

    if (!response.ok) {
      const errBody = await response.text();
      throw new Error(`API error ${response.status}: ${errBody}`);
    }

    return (await response.json()) as T;
  } finally {
    clearTimeout(timer);
  }
}

// Device types matching Go domain
export interface Device {
  id: string;
  name: string;
  hostname: string;
  ipAddresses: string[];
  tailscaleIp: string;
  os: string;
  status: string;
  isExternal: boolean;
  tags: string[] | null;
  user: string;
  lastSeen: string;
  sshEnabled: boolean;
  hasGpu: boolean;
  gpuModel: string;
  gpuCount: number;
}

export interface Cluster {
  id: string;
  name: string;
  description: string;
  status: string;
  headNodeId: string;
  workerIds: string[];
  dashboardUrl: string;
  rayPort: number;
  dashboardPort: number;
  createdAt: string;
  updatedAt: string;
}

export interface TaskResult {
  deviceId: string;
  deviceName: string;
  gpu: string;
  output: string;
  error: string;
  durationMs: number;
}

export interface ExecuteResponse {
  cluster_id: string;
  command: string;
  worker_count: number;
  results: TaskResult[];
}
