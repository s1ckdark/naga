# Cluster GPU Monitor TUI

## Overview
Terminal-based GPU monitoring per cluster, inspired by `bottom` (btm).
Shows real-time GPU metrics for all nodes in a cluster via SSH + nvidia-smi.

## Command
```
clusterctl cluster monitor <cluster-name> [--interval 3]
```

## Data Collection
- SSH into each node, run `nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits`
- Parallel collection across nodes
- Workers always have GPUs; head node may or may not
- Skip nodes where nvidia-smi fails (mark as offline/no-gpu)
- Default polling interval: 3 seconds

## UI Modes

### Table Mode (default)
Compact overview of all GPUs across nodes in a sortable table.

### Detail Mode (toggle with `d`)
Per-GPU panels with utilization history sparkline, memory bar, temperature, and power.

## Key Bindings
- `d` — toggle table/detail mode
- `s` — cycle sort (node > util > memory > temp)
- `q` / `Ctrl+C` — quit
- `r` — force refresh

## TUI Stack
- bubbletea (Elm architecture)
- lipgloss (styling)
- bubbles (spinner, table components)

## New Files
- `internal/domain/gpu.go` — GPUNodeMetrics domain type
- `internal/infra/ssh/gpu_collector.go` — nvidia-smi SSH collection
- `internal/tui/monitor/model.go` — bubbletea model
- `internal/tui/monitor/view.go` — table + detail rendering
- `internal/tui/monitor/update.go` — key handling + tick
- CLI command wiring in `internal/cli/cluster.go`

## Constraints
- No Ray dependency — works whether cluster is running or not
- Graceful degradation if a node is unreachable
- Head node GPU display is optional (skip if no nvidia-smi)
