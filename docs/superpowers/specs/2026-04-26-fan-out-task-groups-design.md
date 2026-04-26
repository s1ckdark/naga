# Fan-out Task Groups (Parallel Batch) — Design

## Context

Hydra's task system today is one-task-at-a-time: every `POST /api/tasks` produces a single `domain.Task` that the supervisor dispatches to one worker. Real workloads almost always come as a **batch of independent tasks** — "run these 50 inferences across the GPU pool, tell me when they're all done" — and the only way to express that today is to fire 50 separate POSTs and track 50 task IDs by hand.

This design adds a parallel-batch primitive: clients submit N tasks as one request, the server returns a single `groupId`, the supervisor distributes the tasks across capable workers, and clients poll one endpoint to see aggregate progress and a final status. Sharded inference (C-α) and DAG workflows (C-γ) are explicitly **not** in scope — those are separate features with different data models. This spec covers only the C-β path.

### Pre-existing limitation this spec addresses

Tasks today live only in the in-memory `domain.TaskQueue` — there is no `tasks` table in SQLite. That makes operational debugging painful (after a server restart there is no record of what happened) and makes a hybrid-persisted group model impossible without persisting tasks too. This spec therefore introduces a `tasks` table alongside the new `task_groups` table and routes every TaskQueue mutation through a write-through repository. Restart-time queue hydration (re-enqueueing rows that were `queued`/`running` at boot) is intentionally left to a follow-up: this PR makes the data inspectable and durable, but does not yet rebuild the in-memory queue from disk.

## Goals

- Submit N independent tasks in one HTTP call; receive a single `groupId`.
- Poll `GET /api/groups/:id` to see aggregate progress and per-task results.
- Surface a tri-state outcome (`completed` / `partial` / `failed`) so 99/100-success cases are distinguishable from a total failure.
- Reuse the existing capability filter, rule-based scorer, and AI scheduler — no fork in the scheduling path.
- Strict additive: existing single-task POST continues to work unchanged.
- Persist tasks and groups to SQLite so operators can inspect/debug/repair state with plain SQL — no more "everything is in memory and gone after restart."

## Non-Goals

- Sharded inference / data-slicing of a single task across workers (C-α).
- DAG workflows / task-to-task dependencies (C-γ).
- Group cancellation (`DELETE /api/groups/:id`).
- Group webhook callbacks or WebSocket subscription — polling only.
- Group-level retention/TTL — groups and tasks live forever in v1.
- A `GET /api/groups` list endpoint — usable backlog item once a UI exists, but not for v1.
- Group-level scheduling priority — per-task `priority` already covers this.
- Per-group AI scheduling override — per-task `aiSchedule` already exists; clients that want a uniform policy set it on every task in the batch.
- **Restart-time queue hydration** — DB rows survive a restart, but the in-memory `TaskQueue` is rebuilt empty. Tasks left as `queued`/`assigned`/`running` at the time of the crash become orphans in the DB; the operator inspects them via SQL but the supervisor does not automatically retry them. Adding hydration is a focused follow-up that depends on worker-side cancel/abandon semantics that are not yet defined.
- **Task archiving / pruning** — once written, task and group rows stay forever. A retention policy is a separate concern.

## Architecture overview

```
                                                         ┌─────────────────┐
 Client ── POST /api/tasks/batch ──┐                    │  task_groups    │
                                   │                    ├─────────────────┤
                                   ▼                    │ id, name        │
                          ┌────────────────┐            │ created_at      │
                          │ APITaskBatch-  │ tx insert  │ created_by      │
                          │ Create         │───────────▶│ total_tasks     │
                          └────────────────┘            │ metadata JSON   │
                                   │                    └─────────────────┘
                                   │ Enqueue N tasks            ▲
                                   ▼                            │
                          ┌────────────────┐                    │ JOIN
                          │  TaskQueue     │ write-through      │
                          │  (in-memory) ──┼────────────┐       │
                          └────────────────┘            ▼       │
                                   │            ┌─────────────┐ │
                                   ▼            │  tasks      │ │
                          ┌────────────────┐    │  (table)    │ │
                          │ TaskSupervisor │    ├─────────────┤ │
                          │ ScheduleNow x1 │───▶│ id, group_id│─┘
                          │ (existing)     │    │ status, ... │
                          └────────────────┘    └─────────────┘
                                                       ▲
                                                       │  SELECT
 Client ── GET /api/groups/:id ──────────────────────┘
                                  status: running|completed|partial|failed
                                  totals: completed N, failed M, queued K, running R
                                  tasks[] (optional: ?detail=full)
```

Three persistence shapes:

- **`tasks` table** (NEW) — every task `domain.TaskQueue` mutation is mirrored here through a write-through `TaskRepository`. Status transitions, assignments, and result captures all UPDATE the row. Memory remains the working set; DB is durable shadow.
- **`task_groups` table** (NEW) — one row per group, immutable identity (id, name, created_at, created_by, total_tasks, metadata). Survives task cleanup or queue eviction.
- **`tasks.group_id` column** — every task either belongs to one group (FK) or stands alone (NULL).

Aggregate group status is **derived** from member tasks at every read — there is no counter to drift out of sync. The `total_tasks` column is the only batch-level fact stored separately, kept immutable so partial completion stays meaningful even if individual task rows are later deleted.

The in-memory `TaskQueue` continues to serve all in-flight operations (enqueue, dequeue, supervisor scheduling). The DB is a write-through audit/inspection layer; no read path under normal operation goes through the DB. The `GET /api/groups/:id` handler is the one exception — it reads `task_groups` and `tasks` from SQLite so that a group's full member list (including completed tasks the queue has already let go of) is accessible.

## Data model

### Migration

Two new tables added to the inline migration list in `internal/repository/sqlite/sqlite.go::migrate()` (Hydra keeps schema as Go strings, not standalone `.sql` files; we follow the existing convention):

```sql
-- New: tasks table (mirrors domain.Task fields written by TaskQueue mutations)
CREATE TABLE IF NOT EXISTS tasks (
    id                     TEXT PRIMARY KEY,
    parent_id              TEXT NOT NULL DEFAULT '',
    orch_id                TEXT NOT NULL DEFAULT '',
    type                   TEXT NOT NULL,
    status                 TEXT NOT NULL,
    priority               TEXT NOT NULL DEFAULT 'normal',
    required_capabilities  TEXT NOT NULL DEFAULT '[]',  -- JSON array
    preferred_device_id    TEXT NOT NULL DEFAULT '',
    assigned_device_id     TEXT NOT NULL DEFAULT '',
    payload                TEXT NOT NULL DEFAULT '{}',  -- JSON object
    result                 TEXT NOT NULL DEFAULT '',    -- JSON object, empty until set
    error                  TEXT NOT NULL DEFAULT '',
    created_at             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_at            TIMESTAMP,
    started_at             TIMESTAMP,
    completed_at           TIMESTAMP,
    timeout_ns             INTEGER NOT NULL DEFAULT 0,
    retry_count            INTEGER NOT NULL DEFAULT 0,
    max_retries            INTEGER NOT NULL DEFAULT 0,
    created_by             TEXT NOT NULL DEFAULT '',
    resource_reqs          TEXT NOT NULL DEFAULT '',    -- JSON object, empty when nil
    blocked_device_ids     TEXT NOT NULL DEFAULT '[]',  -- JSON array
    ai_schedule            TEXT NOT NULL DEFAULT '',    -- '', 'true', 'false' for tri-state *bool
    group_id               TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tasks_status     ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_group_id   ON tasks(group_id);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);

-- New: task_groups table (hybrid: identity persisted, status derived)
CREATE TABLE IF NOT EXISTS task_groups (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by  TEXT NOT NULL DEFAULT '',
    total_tasks INTEGER NOT NULL,
    metadata    TEXT NOT NULL DEFAULT '{}'
);
```

**Why a separate `group_id` text column with default `''` (not NULL FK)**: SQLite's `IF NOT EXISTS` table creation in our existing migration list is idempotent across server restarts; making `group_id` a real foreign key adds insertion-order constraints that complicate the migration (`task_groups` must be created before `tasks` and the constraint declaration is awkward in our inline string format). The constraint is enforced in code at write time (`tasks.group_id` is either `''` or matches a row in `task_groups`).

### Domain types — `internal/domain/task_group.go` (new file)

```go
type TaskGroupStatus string
const (
    TaskGroupStatusRunning   TaskGroupStatus = "running"
    TaskGroupStatusCompleted TaskGroupStatus = "completed"
    TaskGroupStatusPartial   TaskGroupStatus = "partial"
    TaskGroupStatusFailed    TaskGroupStatus = "failed"
)

type TaskGroup struct {
    ID         string                 `json:"id"`
    Name       string                 `json:"name,omitempty"`
    CreatedAt  time.Time              `json:"createdAt"`
    CreatedBy  string                 `json:"createdBy,omitempty"`
    TotalTasks int                    `json:"totalTasks"`
    Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type TaskGroupSnapshot struct {
    TaskGroup
    Status    TaskGroupStatus `json:"status"`
    Completed int             `json:"completed"`
    Failed    int             `json:"failed"`
    Running   int             `json:"running"`
    Queued    int             `json:"queued"`
    Tasks     []*Task         `json:"tasks,omitempty"`
}

func DeriveGroupStatus(tasks []*Task, total int) TaskGroupStatus {
    var completed, failed, terminal int
    for _, t := range tasks {
        switch t.Status {
        case TaskStatusCompleted:
            completed++; terminal++
        case TaskStatusFailed, TaskStatusCancelled:
            failed++; terminal++
        }
    }
    if terminal < total { return TaskGroupStatusRunning }
    if failed == 0      { return TaskGroupStatusCompleted }
    if completed == 0   { return TaskGroupStatusFailed }
    return TaskGroupStatusPartial
}
```

### Task addition

```go
type Task struct {
    // ...existing fields
    GroupID string `json:"groupId,omitempty" db:"group_id"`
}
```

## API surface

### `POST /api/tasks/batch` — new

Mounted under the same Tailscale-auth middleware group as the existing mutating task routes.

**Request body:**

```json
{
  "name": "morning-batch-001",
  "metadata": { "owner": "dave" },
  "tasks": [
    {
      "type": "infer",
      "priority": "normal",
      "requiredCapabilities": ["gpu"],
      "payload": { "input": "row1" },
      "timeout": 60,
      "maxRetries": 1,
      "aiSchedule": null
    },
    { "type": "infer", "payload": { "input": "row2" } },
    { "type": "infer", "payload": { "input": "row3" } }
  ]
}
```

`name` and `metadata` are optional. Each entry in `tasks` is the same shape the existing `POST /api/tasks` already accepts; per-task `aiSchedule` is honoured.

**Validation (returns 400 on failure, no group row written):**

- `tasks` empty or missing → "tasks: must contain at least one task"
- Any task missing `type` → "tasks[i]: type is required"
- `metadata` not a JSON object (string/array/null) → "metadata: must be a JSON object"

**Response 201:**

```json
{
  "id": "20260426010203-abc123",
  "name": "morning-batch-001",
  "createdAt": "2026-04-26T01:02:03+09:00",
  "totalTasks": 3,
  "metadata": { "owner": "dave" },
  "status": "running",
  "completed": 0, "failed": 0, "running": 0, "queued": 3,
  "tasks": [
    { "id": "...", "groupId": "20260426...", "status": "queued", ... },
    { "id": "...", "groupId": "20260426...", "status": "queued", ... },
    { "id": "...", "groupId": "20260426...", "status": "queued", ... }
  ]
}
```

The embedded `tasks[]` is the freshly-inserted set, in the **same order as the request**, so the client can pair task IDs back to its inputs without an extra lookup. Subsequent polls use the lightweight default response.

### `GET /api/groups/:id` — new

**Default lightweight response:**

```json
{
  "id": "...", "name": "...", "createdAt": "...", "createdBy": "...",
  "totalTasks": 3, "metadata": {...},
  "status": "partial",
  "completed": 2, "failed": 1, "running": 0, "queued": 0
}
```

**With `?detail=full`:** same shape plus `tasks: [...]` (every task object joined from `tasks WHERE group_id=?`).

**404** when no `task_groups` row exists.

### `POST /api/tasks` (existing) — unchanged

`groupId` is included in the response object (empty string for ungrouped tasks). No request shape change. No behaviour change.

## Scheduling integration

The supervisor's `scheduleQueue` does not need to know batches exist — it walks the priority-ordered queue and assigns each task on its own merits. The integration point is on the **POST handler**: enqueue all N tasks first, then call `taskSupervisor.ScheduleNow(ctx)` exactly once. Calling ScheduleNow per task would rebuild snapshots and re-fetch every device through Tailscale N times for no benefit.

```go
// internal/web/handler/task_handler.go
func (h *Handler) APITaskBatchCreate(c echo.Context) error {
    // 1. Parse + validate
    // 2. Begin DB transaction
    // 3. Insert task_groups row
    // 4. For each task: generateID, set GroupID, taskQueue.Enqueue, repos.Tasks.Save
    // 5. Commit (or rollback on any error)
    // 6. taskSupervisor.ScheduleNow(ctx)  -- exactly once
    // 7. Build TaskGroupSnapshot with embedded tasks
    // 8. 201 Created
}
```

The existing `bumpRunningJobs(snaps, deviceID)` in `scheduleQueue` already updates the in-tick snapshot slice every time a task is assigned, so later tasks in the same pass see the freshly-assigned worker as having one more running job. This makes batch tasks naturally spread across the worker pool through the rule-based Queue weight (10%) — no new round-robin code is required.

Per-task `aiSchedule` (already implemented in B) keeps working inside batches: clients can mix AI-scheduled and rule-based tasks in one batch, and `aiCallBudget=5/tick` continues to cap how many tasks consult the AI per scheduling pass.

## Failure & lifecycle

### Status transitions

```
              (created)
                 │
                 ▼
            ┌────────┐
            │running │ ◀── any task in queued/assigned/running
            └────────┘
                 │
         all tasks terminal
                 │
        ┌────────┼────────┐
        ▼        ▼        ▼
   ┌────────┐ ┌──────┐ ┌──────┐
   │complete│ │partl │ │failed│
   └────────┘ └──────┘ └──────┘
   completed=N  comp+fail   failed=N
   failed=0     mixed       comp=0
```

- Status is computed at every GET. There is no transition trigger, no event hook, and no cached counter.
- `cancelled` tasks are counted as `failed` for the purpose of group status.

### One-way derivation

Group state always flows from tasks; there is no API or internal call that mutates a group's status directly. The only group fields a client can ever set are `name` and `metadata`, and only at batch creation time.

### Edge cases

| Scenario | Behaviour |
|---|---|
| `tasks` is empty | 400, no group row written |
| One task has `type: ""` | 400, full transaction rolled back |
| GET while a transition is mid-flight | Eventually consistent — next GET picks up the new status |
| Tailscale auth missing | 403 (existing middleware) |
| Very large batch (e.g. 10k tasks) | Accepted. Single SQLite transaction handles bulk INSERT well. Operational sizing is a follow-up. |
| Server restart with `running`/`assigned` tasks | DB rows survive. The in-memory queue is rebuilt empty on next boot, so those tasks become orphans — visible in SQL queries with their last known status, but never re-dispatched. Group GET still computes status from the persisted rows; a group with an orphan task stays `running` forever. Documented limitation; restart hydration is the unblock. |
| `TaskRepository.Save` returns an error | Logged via `log.Printf("[taskqueue] persist failed: %v", err)`. The in-memory mutation is still applied so the scheduler keeps running. Operator inspects logs; eventual repair via SQL. |
| Future task cleanup deletes rows | (Not in v1.) When a retention policy lands, `total_tasks` on the group stays as originally submitted; status derivation treats missing rows as "still running" (`terminal < total`), the safest interpretation. |

## TaskQueue write-through

The existing `domain.TaskQueue` is a pure in-memory structure with no awareness of persistence. Adding write-through cleanly:

```go
// internal/domain/taskqueue.go
type TaskQueue struct {
    // ...existing fields
    repo TaskRepository    // optional: nil disables write-through (used by tests)
}

// TaskRepository is the persistence boundary for tasks. Defined here in
// domain/ to avoid an import cycle with internal/repository.
type TaskRepository interface {
    Save(context.Context, *Task) error           // INSERT or UPDATE depending on existence
    Delete(context.Context, string) error        // unused in v1; future cleanup hook
}

func (q *TaskQueue) WithRepo(r TaskRepository) *TaskQueue {
    q.repo = r
    return q
}
```

Each existing mutation (`Enqueue`, `UpdateStatus`, `AssignToDevice`, `SetResult`, `CheckTimeouts`, `ReassignTasksFromDevice`) gains exactly one `_ = q.repo.Save(ctx, task)` line after its in-memory update. Errors are logged via `log.Printf` but do **not** roll back the in-memory mutation — DB downtime must not stall the scheduler. The same trade-off the rest of the codebase makes for write-through caches.

The repository implementation in `internal/repository/sqlite/task.go` does the JSON marshalling for the JSON-shaped columns (`payload`, `result`, `required_capabilities`, `blocked_device_ids`, `resource_reqs`) and translates `Task.AISchedule *bool` into the `'' | 'true' | 'false'` text column.

`cmd/server/main.go` wires it: after `taskQueue := domain.NewTaskQueue()`, call `taskQueue.WithRepo(repos.Tasks)`.

## Files touched

### New
- `internal/domain/task_group.go` — types + `DeriveGroupStatus`
- `internal/domain/task_repository.go` — `TaskRepository` interface (in domain to avoid import cycle)
- `internal/repository/repository.go` — `TaskGroupRepository` + `TaskRepository` interfaces (re-exported)
- `internal/repository/sqlite/task.go` — `TaskRepository` Save/Delete
- `internal/repository/sqlite/task_test.go` — round-trip tests
- `internal/repository/sqlite/task_group.go` — `TaskGroupRepository` Save/GetByID/GetTasks
- `internal/repository/sqlite/task_group_test.go`
- `internal/web/handler/task_group_handler.go` — `APITaskBatchCreate`, `APIGetGroup`
- `internal/web/handler/task_group_handler_test.go`

### Modified
- `internal/domain/task.go` — `GroupID` field
- `internal/domain/taskqueue.go` — `repo` field + `WithRepo` + write-through call sites in 6 mutation methods
- `internal/repository/repository.go` — `Repositories` struct gains `Tasks` and `TaskGroups`
- `internal/repository/sqlite/sqlite.go` — append two new `CREATE TABLE` strings to the inline migration list; populate `Tasks`/`TaskGroups` in `Repositories()`; same in `Transaction` methods
- `cmd/server/main.go` — register `POST /api/tasks/batch` + `GET /api/groups/:id`; wire `taskQueue.WithRepo(repos.Tasks)`
- `internal/web/handler/task_handler.go` — include `groupId` in single-task POST response (1 line)

## Testing

### Unit
- `TestDeriveGroupStatus_AllCompleted`, `_AllFailed`, `_Partial`, `_OneRunning`, `_LessTasksThanTotal`
- `TestTaskRepo_SaveInsertThenUpdate` — same id roundtrip, status transitions persisted
- `TestTaskRepo_JSONFieldRoundtrip` — `payload`, `result`, `requiredCapabilities` survive marshal/unmarshal
- `TestTaskRepo_AISchedulePointerEncoding` — `nil` ↔ `''`, `*true` ↔ `'true'`, `*false` ↔ `'false'`
- `TestTaskQueue_EnqueueWritesThrough` — `WithRepo(stub)` then `Enqueue` triggers `Save`
- `TestTaskQueue_RepoFailureDoesNotBlockEnqueue` — repo returning error still leaves task in memory
- `TestTaskGroupRepo_SaveAndGet`, `_GetByID_NotFound`, `_GetTasksByGroup`
- `TestAPITaskBatchCreate_HappyPath`, `_EmptyTasks`, `_InvalidTaskRollsBackTransaction`
- `TestAPIGetGroup_DerivedStatus`, `_DetailFull`, `_NotFound`

### Manual end-to-end
1. **Spread** — three wstubs + 6-task batch → each worker receives roughly 2 tasks.
2. **Capability routing inside batch** — gpu+cpu mixed batch → gpu tasks land only on gpu workers.
3. **Partial transition** — drive selected tasks into `failed` state, confirm group reports `partial`.
4. **DB consistency** — after batch, `SELECT COUNT(*) FROM tasks WHERE group_id=?` matches `total_tasks`.

## Reuse

- `domain.TaskQueue.Enqueue` — already takes a `*Task`; just set `GroupID` before calling.
- `internal/usecase/task_supervisor.TaskSupervisor.ScheduleNow` — added in the prior PR for single-POST scheduling; reused as-is.
- `internal/web/middleware/apikey.go` and the existing Tailscale auth group — same protections as `POST /api/tasks`.
- `domain.TaskStatus` constants — group status derivation depends only on existing terminal states.

## Migration / rollout

- Backwards compatible: clients that only use `POST /api/tasks` and `GET /api/tasks/:id` see no change.
- The migration adds two tables (`tasks`, `task_groups`). Existing servers run the migration on next boot via `sqlite.DB.migrate()`. No data backfill — pre-existing in-memory tasks are not retroactively persisted (they live and die in memory as before; only tasks created after the deploy land in the new table).
- The new endpoints are gated behind the same Tailscale auth as the existing mutating routes — no new auth surface.
- After deploy, operators can immediately verify the system works by running `sqlite3 ~/.hydra/hydra.db 'SELECT id, status, group_id FROM tasks ORDER BY created_at DESC LIMIT 10'` — a debug capability the system has never had before.
