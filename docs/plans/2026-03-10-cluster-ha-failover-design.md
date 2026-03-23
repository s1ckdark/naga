# Cluster HA — Head Failover + Session Management

## Overview
Automatic head node failover with AI-driven node selection, dual-layer failure detection
(server + worker self-detection), checkpoint-based job recovery, and systemd-managed node agents.

## Problem
- Head node failure kills all running Ray jobs
- SSH disconnection loses tmux-less sessions
- No automatic recovery — manual intervention required

## Architecture

```
┌─ naga server (primary monitor) ─────────────┐
│  heartbeat monitor → failure detection → AI select │
│  (Claude API / fallback: rule-based)               │
└────────────────────────────────────────────────────┘
         │ server also down? ↓
┌─ worker nodes (secondary monitor) ─────────────────┐
│  each worker checks head heartbeat                  │
│  if server also unresponsive → rule-based election  │
└────────────────────────────────────────────────────┘
```

## Components

### 1. Node Agent (systemd service)
Lightweight agent installed on each node, registered as systemd unit.

Responsibilities:
- Ray process management (start/stop/restart)
- Metrics collection (GPU, CPU, memory)
- Heartbeat send/receive (head ↔ worker, server ↔ node)
- Participate in head election voting

### 2. Heartbeat Protocol
- Head → workers: 3s interval
- Workers → head: 3s status report
- Server → all nodes: 5s health check
- Timeout: 15s no response = failure

### 3. Head Election Process
```
Head unresponsive 15s
    ↓
Primary: server detects → Claude API selects optimal candidate
    (metrics-based: low GPU usage, high memory availability, stable network)
    ↓ server also unresponsive
Secondary: workers self-detect → rule-based fallback
    (priority: worker with lowest GPU utilization)
    ↓
Start Ray head on new node
    ↓
Reconnect remaining workers to new head
    ↓
Resume jobs from checkpoint
```

### 4. AI Selection Logic
- Input: per-node GPU utilization, memory, running job count, network latency
- Output: new head node ID + reasoning
- Fallback: API unavailable → select node with lowest GPU utilization

### 5. Checkpoint Recovery
- Leverage Ray's native checkpoint mechanism
- On head switch: start Ray on new head → connect workers → mount checkpoint dir → resume
- Checkpoint storage: shared storage (NFS) or local + rsync

### 6. Manual Head Migration
- Existing: `naga cluster change-head <cluster> <new-head>`
- Enhanced: save checkpoint → switch → recover flow

## New Files
- `cmd/naga-agent/main.go` — agent binary entrypoint
- `internal/agent/agent.go` — node agent main loop
- `internal/agent/heartbeat.go` — heartbeat protocol
- `internal/agent/election.go` — head election logic (AI + fallback)
- `internal/agent/systemd.go` — systemd unit generation/installation
- `internal/usecase/failover_usecase.go` — failover business logic
- `internal/infra/ai/selector.go` — Claude API based head selection

## Constraints
- Agent must be self-contained single binary
- Graceful degradation: AI unavailable → rule-based election
- No split-brain: only one head at a time (fencing via lock file or distributed lock)
- Checkpoint storage path must be configurable
