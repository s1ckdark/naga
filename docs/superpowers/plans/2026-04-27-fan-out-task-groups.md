# Fan-out Task Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `POST /api/tasks/batch` and `GET /api/groups/:id` so clients can submit N independent tasks as one request and poll a single endpoint for aggregate progress, while introducing write-through SQLite persistence for both tasks and groups.

**Architecture:** Two new SQLite tables (`tasks`, `task_groups`) populated through a `domain.TaskRepository` write-through that hooks into every `TaskQueue` mutation. The in-memory queue stays the source of truth for in-flight scheduling; the DB is the durable shadow used by `GET /api/groups/:id` and by operators for ad-hoc SQL inspection. Group status is derived at read time from the joined task rows — no counter to drift.

**Tech Stack:** Go (Echo, database/sql with mattn/go-sqlite3), the existing repository / Repositories / Transaction pattern in `internal/repository/`.

**Spec:** [docs/superpowers/specs/2026-04-26-fan-out-task-groups-design.md](../specs/2026-04-26-fan-out-task-groups-design.md)

---

## Task 1: TaskGroup domain types + DeriveGroupStatus

**Files:**
- Create: `internal/domain/task_group.go`
- Create: `internal/domain/task_group_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/domain/task_group_test.go`:

```go
package domain

import "testing"

func TestDeriveGroupStatus_AllCompleted(t *testing.T) {
	tasks := []*Task{
		{Status: TaskStatusCompleted}, {Status: TaskStatusCompleted}, {Status: TaskStatusCompleted},
	}
	if got := DeriveGroupStatus(tasks, 3); got != TaskGroupStatusCompleted {
		t.Errorf("got %s, want completed", got)
	}
}

func TestDeriveGroupStatus_AllFailed(t *testing.T) {
	tasks := []*Task{
		{Status: TaskStatusFailed}, {Status: TaskStatusCancelled},
	}
	if got := DeriveGroupStatus(tasks, 2); got != TaskGroupStatusFailed {
		t.Errorf("got %s, want failed", got)
	}
}

func TestDeriveGroupStatus_Partial(t *testing.T) {
	tasks := []*Task{
		{Status: TaskStatusCompleted}, {Status: TaskStatusFailed},
	}
	if got := DeriveGroupStatus(tasks, 2); got != TaskGroupStatusPartial {
		t.Errorf("got %s, want partial", got)
	}
}

func TestDeriveGroupStatus_OneRunning(t *testing.T) {
	tasks := []*Task{
		{Status: TaskStatusCompleted}, {Status: TaskStatusRunning},
	}
	if got := DeriveGroupStatus(tasks, 2); got != TaskGroupStatusRunning {
		t.Errorf("got %s, want running", got)
	}
}

func TestDeriveGroupStatus_LessTasksThanTotal(t *testing.T) {
	// total=3 but only 2 visible (e.g. one was deleted) → still treated as running
	tasks := []*Task{
		{Status: TaskStatusCompleted}, {Status: TaskStatusCompleted},
	}
	if got := DeriveGroupStatus(tasks, 3); got != TaskGroupStatusRunning {
		t.Errorf("got %s, want running (terminal=%d < total=%d)", got, 2, 3)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/domain -run TestDeriveGroupStatus -v
```

Expected: `undefined: DeriveGroupStatus` build error.

- [ ] **Step 3: Implement types and function**

Create `internal/domain/task_group.go`:

```go
package domain

import "time"

// TaskGroupStatus is the derived aggregate status of a TaskGroup.
type TaskGroupStatus string

const (
	TaskGroupStatusRunning   TaskGroupStatus = "running"
	TaskGroupStatusCompleted TaskGroupStatus = "completed"
	TaskGroupStatusPartial   TaskGroupStatus = "partial"
	TaskGroupStatusFailed    TaskGroupStatus = "failed"
)

// TaskGroup is the immutable identity of a fan-out batch. Aggregate progress
// is computed at read time from member tasks — there is no counter on the
// group to drift out of sync.
type TaskGroup struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name,omitempty"`
	CreatedAt  time.Time              `json:"createdAt"`
	CreatedBy  string                 `json:"createdBy,omitempty"`
	TotalTasks int                    `json:"totalTasks"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// TaskGroupSnapshot is the response shape of GET /api/groups/:id — group
// identity plus derived progress and (optionally) member tasks.
type TaskGroupSnapshot struct {
	TaskGroup
	Status    TaskGroupStatus `json:"status"`
	Completed int             `json:"completed"`
	Failed    int             `json:"failed"`
	Running   int             `json:"running"`
	Queued    int             `json:"queued"`
	Tasks     []*Task         `json:"tasks,omitempty"`
}

// DeriveGroupStatus computes the group's aggregate status from its member
// tasks. `total` is the original batch size; if fewer tasks are passed the
// missing ones are treated as still running (the safest interpretation).
// Cancelled tasks count as failed for the group's purposes.
func DeriveGroupStatus(tasks []*Task, total int) TaskGroupStatus {
	var completed, failed, terminal int
	for _, t := range tasks {
		switch t.Status {
		case TaskStatusCompleted:
			completed++
			terminal++
		case TaskStatusFailed, TaskStatusCancelled:
			failed++
			terminal++
		}
	}
	if terminal < total {
		return TaskGroupStatusRunning
	}
	if failed == 0 {
		return TaskGroupStatusCompleted
	}
	if completed == 0 {
		return TaskGroupStatusFailed
	}
	return TaskGroupStatusPartial
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/domain -run TestDeriveGroupStatus -v
```

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/task_group.go internal/domain/task_group_test.go
git commit -m "feat(domain): add TaskGroup types + DeriveGroupStatus

Tri-state aggregate status (running/completed/partial/failed) derived
from member tasks at read time. Cancelled tasks count as failed for
the group; missing tasks (total > len) keep the group running so a
partially-deleted group never falsely reports complete."
```

---

## Task 2: Add Task.GroupID field

**Files:**
- Modify: `internal/domain/task.go`

- [ ] **Step 1: Add the field**

In `internal/domain/task.go`, append `GroupID` to the `Task` struct after `BlockedDeviceIDs`:

```go
type Task struct {
	// ...existing fields up to AISchedule
	AISchedule *bool `json:"aiSchedule,omitempty"`
	// GroupID links this task to a fan-out batch (TaskGroup). Empty when
	// the task was submitted via the single-task POST endpoint.
	GroupID string `json:"groupId,omitempty"`
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 3: Verify existing task tests still pass**

```bash
go test ./internal/domain ./internal/usecase ./internal/web/handler ./internal/infra/ai/...
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/task.go
git commit -m "feat(domain): add Task.GroupID for fan-out membership"
```

---

## Task 3: TaskRepository interface (in domain to avoid cycle)

**Files:**
- Create: `internal/domain/task_repository.go`

- [ ] **Step 1: Define the interface**

Create `internal/domain/task_repository.go`:

```go
package domain

import "context"

// TaskRepository is the persistence boundary for tasks. It lives in the
// domain package so the TaskQueue can depend on it without importing
// internal/repository (which would create a cycle).
type TaskRepository interface {
	// Save inserts or updates a task. Implementations should treat the
	// id as the identity key (UPSERT semantics).
	Save(ctx context.Context, task *Task) error

	// Delete removes a task by id. Currently unused; reserved for future
	// retention/cleanup logic.
	Delete(ctx context.Context, id string) error
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/task_repository.go
git commit -m "feat(domain): define TaskRepository interface for write-through"
```

---

## Task 4: SQLite migration for tasks and task_groups tables

**Files:**
- Modify: `internal/repository/sqlite/sqlite.go`

- [ ] **Step 1: Append CREATE TABLE statements to the inline migration list**

In `internal/repository/sqlite/sqlite.go::migrate()`, find the `migrations := []string{...}` slice and append the following two entries at the end of the slice (immediately before the closing `}`):

```go
		// Tasks table — write-through shadow of the in-memory TaskQueue.
		`CREATE TABLE IF NOT EXISTS tasks (
			id                     TEXT PRIMARY KEY,
			parent_id              TEXT NOT NULL DEFAULT '',
			orch_id                TEXT NOT NULL DEFAULT '',
			type                   TEXT NOT NULL,
			status                 TEXT NOT NULL,
			priority               TEXT NOT NULL DEFAULT 'normal',
			required_capabilities  TEXT NOT NULL DEFAULT '[]',
			preferred_device_id    TEXT NOT NULL DEFAULT '',
			assigned_device_id     TEXT NOT NULL DEFAULT '',
			payload                TEXT NOT NULL DEFAULT '{}',
			result                 TEXT NOT NULL DEFAULT '',
			error                  TEXT NOT NULL DEFAULT '',
			created_at             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			assigned_at            TIMESTAMP,
			started_at             TIMESTAMP,
			completed_at           TIMESTAMP,
			timeout_ns             INTEGER NOT NULL DEFAULT 0,
			retry_count            INTEGER NOT NULL DEFAULT 0,
			max_retries            INTEGER NOT NULL DEFAULT 0,
			created_by             TEXT NOT NULL DEFAULT '',
			resource_reqs          TEXT NOT NULL DEFAULT '',
			blocked_device_ids     TEXT NOT NULL DEFAULT '[]',
			ai_schedule            TEXT NOT NULL DEFAULT '',
			group_id               TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status     ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_group_id   ON tasks(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at)`,

		// Task groups table — fan-out batch identity.
		`CREATE TABLE IF NOT EXISTS task_groups (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_by  TEXT NOT NULL DEFAULT '',
			total_tasks INTEGER NOT NULL,
			metadata    TEXT NOT NULL DEFAULT '{}'
		)`,
```

- [ ] **Step 2: Verify build + migration runs**

```bash
go build ./...
rm -f /tmp/_hydra-mig-test.db
NAGA_DATABASE_DSN=/tmp/_hydra-mig-test.db go run ./cmd/server >/tmp/_hydra-mig.log 2>&1 &
PID=$!
sleep 2
kill $PID 2>/dev/null

sqlite3 /tmp/_hydra-mig-test.db ".tables" | tr -s ' ' '\n' | grep -E '^(tasks|task_groups)$'
sqlite3 /tmp/_hydra-mig-test.db ".schema tasks" | head -5
```

Expected: `tasks` and `task_groups` both present in `.tables` output.

- [ ] **Step 3: Commit**

```bash
git add internal/repository/sqlite/sqlite.go
git commit -m "feat(sqlite): add tasks and task_groups tables to migration

Inline CREATE TABLE strings appended to the existing migration list,
matching the codebase convention of keeping schema as Go strings rather
than separate .sql files. Three indexes on tasks for status, group_id,
and created_at to keep the GET /api/groups/:id JOIN cheap."
```

---

## Task 5: TaskRepository SQLite implementation

**Files:**
- Create: `internal/repository/sqlite/task.go`
- Create: `internal/repository/sqlite/task_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/repository/sqlite/task_test.go`:

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
)

func newTaskRepoForTest(t *testing.T) *TaskRepository {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewTaskRepository(db.db)
}

func TestTaskRepo_SaveInsertThenUpdate(t *testing.T) {
	r := newTaskRepoForTest(t)
	ctx := context.Background()

	task := &domain.Task{
		ID: "t1", Type: "shell", Status: domain.TaskStatusQueued,
		Priority: domain.TaskPriorityNormal,
		Payload:  map[string]interface{}{"cmd": "echo a"},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := r.Save(ctx, task); err != nil {
		t.Fatalf("insert: %v", err)
	}

	task.Status = domain.TaskStatusCompleted
	task.AssignedDeviceID = "dev-1"
	if err := r.Save(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := r.GetByID(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.TaskStatusCompleted || got.AssignedDeviceID != "dev-1" {
		t.Errorf("after update: %+v", got)
	}
}

func TestTaskRepo_JSONFieldRoundtrip(t *testing.T) {
	r := newTaskRepoForTest(t)
	ctx := context.Background()

	task := &domain.Task{
		ID: "t2", Type: "infer", Status: domain.TaskStatusQueued,
		Priority:             domain.TaskPriorityHigh,
		RequiredCapabilities: []string{"gpu", "cuda"},
		BlockedDeviceIDs:     []string{"bad-1"},
		Payload:              map[string]interface{}{"input": "x", "n": float64(42)},
		ResourceReqs:         &domain.ResourceRequirements{GPUMemoryMB: 16000, CPUCores: 4},
	}
	if err := r.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := r.GetByID(ctx, "t2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.RequiredCapabilities) != 2 || got.RequiredCapabilities[0] != "gpu" {
		t.Errorf("RequiredCapabilities lost: %v", got.RequiredCapabilities)
	}
	if got.Payload["n"] != float64(42) {
		t.Errorf("Payload lost: %v", got.Payload)
	}
	if got.ResourceReqs == nil || got.ResourceReqs.GPUMemoryMB != 16000 {
		t.Errorf("ResourceReqs lost: %+v", got.ResourceReqs)
	}
}

func TestTaskRepo_AISchedulePointerEncoding(t *testing.T) {
	r := newTaskRepoForTest(t)
	ctx := context.Background()
	tru, fal := true, false

	cases := []struct {
		id  string
		val *bool
	}{
		{"a", nil},
		{"b", &tru},
		{"c", &fal},
	}
	for _, c := range cases {
		task := &domain.Task{ID: c.id, Type: "shell", Status: domain.TaskStatusQueued, AISchedule: c.val}
		if err := r.Save(ctx, task); err != nil {
			t.Fatalf("save %s: %v", c.id, err)
		}
		got, err := r.GetByID(ctx, c.id)
		if err != nil {
			t.Fatalf("get %s: %v", c.id, err)
		}
		switch {
		case c.val == nil && got.AISchedule != nil:
			t.Errorf("%s: nil round-tripped to %v", c.id, *got.AISchedule)
		case c.val != nil && got.AISchedule == nil:
			t.Errorf("%s: %v round-tripped to nil", c.id, *c.val)
		case c.val != nil && got.AISchedule != nil && *c.val != *got.AISchedule:
			t.Errorf("%s: %v round-tripped to %v", c.id, *c.val, *got.AISchedule)
		}
	}
}

func TestTaskRepo_GetByGroup(t *testing.T) {
	r := newTaskRepoForTest(t)
	ctx := context.Background()
	for _, id := range []string{"x1", "x2", "x3"} {
		_ = r.Save(ctx, &domain.Task{ID: id, Type: "shell", Status: domain.TaskStatusQueued, GroupID: "g1"})
	}
	_ = r.Save(ctx, &domain.Task{ID: "y1", Type: "shell", Status: domain.TaskStatusQueued, GroupID: "g2"})

	got, err := r.GetByGroup(ctx, "g1")
	if err != nil {
		t.Fatalf("GetByGroup: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (got=%v)", len(got), got)
	}
}
```

- [ ] **Step 2: Run tests to confirm fail**

```bash
go test ./internal/repository/sqlite -run TestTaskRepo -v
```

Expected: build error `undefined: NewTaskRepository`.

- [ ] **Step 3: Implement TaskRepository**

Create `internal/repository/sqlite/task.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/s1ckdark/hydra/internal/domain"
)

// TaskRepository persists domain.Task rows. It is the write-through target
// for domain.TaskQueue mutations.
type TaskRepository struct {
	db dbExecutor
}

func NewTaskRepository(db dbExecutor) *TaskRepository {
	return &TaskRepository{db: db}
}

// Save inserts or updates a task by id (UPSERT).
func (r *TaskRepository) Save(ctx context.Context, t *domain.Task) error {
	reqCaps, _ := json.Marshal(t.RequiredCapabilities)
	if t.RequiredCapabilities == nil {
		reqCaps = []byte("[]")
	}
	blocked, _ := json.Marshal(t.BlockedDeviceIDs)
	if t.BlockedDeviceIDs == nil {
		blocked = []byte("[]")
	}
	payload, _ := json.Marshal(t.Payload)
	if t.Payload == nil {
		payload = []byte("{}")
	}
	var result []byte
	if t.Result != nil {
		result, _ = json.Marshal(t.Result)
	}
	var resourceReqs []byte
	if t.ResourceReqs != nil {
		resourceReqs, _ = json.Marshal(t.ResourceReqs)
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, parent_id, orch_id, type, status, priority,
			required_capabilities, preferred_device_id, assigned_device_id,
			payload, result, error,
			created_at, assigned_at, started_at, completed_at,
			timeout_ns, retry_count, max_retries, created_by,
			resource_reqs, blocked_device_ids, ai_schedule, group_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			priority = excluded.priority,
			assigned_device_id = excluded.assigned_device_id,
			result = excluded.result,
			error = excluded.error,
			assigned_at = excluded.assigned_at,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			retry_count = excluded.retry_count,
			blocked_device_ids = excluded.blocked_device_ids,
			ai_schedule = excluded.ai_schedule,
			group_id = excluded.group_id
	`,
		t.ID, t.ParentID, t.OrchID, t.Type, string(t.Status), string(t.Priority),
		string(reqCaps), t.PreferredDeviceID, t.AssignedDeviceID,
		string(payload), string(result), t.Error,
		t.CreatedAt, t.AssignedAt, t.StartedAt, t.CompletedAt,
		int64(t.Timeout), t.RetryCount, t.MaxRetries, t.CreatedBy,
		string(resourceReqs), string(blocked), encodeAISchedule(t.AISchedule), t.GroupID,
	)
	return err
}

// Delete removes a task by id. Currently unused at runtime.
func (r *TaskRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// GetByID fetches one task. Returns sql.ErrNoRows when missing.
func (r *TaskRepository) GetByID(ctx context.Context, id string) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx, taskSelectColumns+` WHERE id = ?`, id)
	return scanTask(row)
}

// GetByGroup returns every task for a group, oldest first.
func (r *TaskRepository) GetByGroup(ctx context.Context, groupID string) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		taskSelectColumns+` WHERE group_id = ? ORDER BY created_at ASC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

const taskSelectColumns = `
	SELECT id, parent_id, orch_id, type, status, priority,
		   required_capabilities, preferred_device_id, assigned_device_id,
		   payload, result, error,
		   created_at, assigned_at, started_at, completed_at,
		   timeout_ns, retry_count, max_retries, created_by,
		   resource_reqs, blocked_device_ids, ai_schedule, group_id
	FROM tasks`

type taskRowScanner interface {
	Scan(dest ...interface{}) error
}

func scanTask(row taskRowScanner) (*domain.Task, error) {
	var (
		t                                          domain.Task
		status, priority                           string
		reqCaps, blocked, payload, result, resReqs string
		aiSched                                    string
		assignedAt, startedAt, completedAt         sql.NullTime
		timeoutNS                                  int64
	)
	err := row.Scan(
		&t.ID, &t.ParentID, &t.OrchID, &t.Type, &status, &priority,
		&reqCaps, &t.PreferredDeviceID, &t.AssignedDeviceID,
		&payload, &result, &t.Error,
		&t.CreatedAt, &assignedAt, &startedAt, &completedAt,
		&timeoutNS, &t.RetryCount, &t.MaxRetries, &t.CreatedBy,
		&resReqs, &blocked, &aiSched, &t.GroupID,
	)
	if err != nil {
		return nil, err
	}
	t.Status = domain.TaskStatus(status)
	t.Priority = domain.TaskPriority(priority)
	t.Timeout = parseDurationNS(timeoutNS)
	if assignedAt.Valid {
		v := assignedAt.Time
		t.AssignedAt = &v
	}
	if startedAt.Valid {
		v := startedAt.Time
		t.StartedAt = &v
	}
	if completedAt.Valid {
		v := completedAt.Time
		t.CompletedAt = &v
	}
	if reqCaps != "" {
		_ = json.Unmarshal([]byte(reqCaps), &t.RequiredCapabilities)
	}
	if blocked != "" {
		_ = json.Unmarshal([]byte(blocked), &t.BlockedDeviceIDs)
	}
	if payload != "" {
		_ = json.Unmarshal([]byte(payload), &t.Payload)
	}
	if result != "" {
		var r domain.TaskResult
		if json.Unmarshal([]byte(result), &r) == nil {
			t.Result = &r
		}
	}
	if resReqs != "" {
		var rr domain.ResourceRequirements
		if json.Unmarshal([]byte(resReqs), &rr) == nil {
			t.ResourceReqs = &rr
		}
	}
	t.AISchedule = decodeAISchedule(aiSched)
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]*domain.Task, error) {
	var out []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func parseDurationNS(ns int64) time.Duration { return time.Duration(ns) }

func encodeAISchedule(p *bool) string {
	if p == nil {
		return ""
	}
	if *p {
		return "true"
	}
	return "false"
}

func decodeAISchedule(s string) *bool {
	switch s {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	default:
		return nil
	}
}
```

Add the missing import for `time`:

```go
import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
)
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/repository/sqlite -run TestTaskRepo -v
```

Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/sqlite/task.go internal/repository/sqlite/task_test.go
git commit -m "feat(sqlite): TaskRepository with UPSERT + JSON column round-trip

JSON columns (payload, result, required_capabilities, blocked_device_ids,
resource_reqs) marshalled on Save and unmarshalled on Get. AISchedule
*bool encoded into a tri-valued text column ('', 'true', 'false') so the
nil case is distinguishable from explicit false.

GetByGroup returns all tasks for a group ordered oldest-first — used by
the upcoming GET /api/groups/:id handler."
```

---

## Task 6: TaskGroupRepository SQLite implementation

**Files:**
- Create: `internal/repository/sqlite/task_group.go`
- Create: `internal/repository/sqlite/task_group_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/repository/sqlite/task_group_test.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
)

func newGroupRepoForTest(t *testing.T) *TaskGroupRepository {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewTaskGroupRepository(db.db)
}

func TestTaskGroupRepo_SaveAndGet(t *testing.T) {
	r := newGroupRepoForTest(t)
	ctx := context.Background()

	g := &domain.TaskGroup{
		ID:         "g1",
		Name:       "morning-batch",
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "dave",
		TotalTasks: 7,
		Metadata:   map[string]interface{}{"owner": "dave"},
	}
	if err := r.Save(ctx, g); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := r.GetByID(ctx, "g1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "morning-batch" || got.TotalTasks != 7 || got.Metadata["owner"] != "dave" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestTaskGroupRepo_GetByID_NotFound(t *testing.T) {
	r := newGroupRepoForTest(t)
	_, err := r.GetByID(context.Background(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}
```

- [ ] **Step 2: Run tests to confirm fail**

```bash
go test ./internal/repository/sqlite -run TestTaskGroupRepo -v
```

Expected: build error.

- [ ] **Step 3: Implement TaskGroupRepository**

Create `internal/repository/sqlite/task_group.go`:

```go
package sqlite

import (
	"context"
	"encoding/json"

	"github.com/s1ckdark/hydra/internal/domain"
)

// TaskGroupRepository persists fan-out batch identity. Aggregate progress
// is derived from the joined tasks at read time, not stored here.
type TaskGroupRepository struct {
	db dbExecutor
}

func NewTaskGroupRepository(db dbExecutor) *TaskGroupRepository {
	return &TaskGroupRepository{db: db}
}

// Save inserts or updates (UPSERT). Updating an existing group only changes
// name and metadata; identity fields (id, created_at, created_by, total_tasks)
// are immutable by convention.
func (r *TaskGroupRepository) Save(ctx context.Context, g *domain.TaskGroup) error {
	metadata, _ := json.Marshal(g.Metadata)
	if g.Metadata == nil {
		metadata = []byte("{}")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO task_groups (
			id, name, created_at, created_by, total_tasks, metadata
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			metadata = excluded.metadata
	`, g.ID, g.Name, g.CreatedAt, g.CreatedBy, g.TotalTasks, string(metadata))
	return err
}

// GetByID fetches one group. Returns sql.ErrNoRows when missing.
func (r *TaskGroupRepository) GetByID(ctx context.Context, id string) (*domain.TaskGroup, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, created_at, created_by, total_tasks, metadata
		FROM task_groups WHERE id = ?`, id)

	var (
		g        domain.TaskGroup
		metadata string
	)
	if err := row.Scan(&g.ID, &g.Name, &g.CreatedAt, &g.CreatedBy, &g.TotalTasks, &metadata); err != nil {
		return nil, err
	}
	if metadata != "" {
		_ = json.Unmarshal([]byte(metadata), &g.Metadata)
	}
	return &g, nil
}
```

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/repository/sqlite -run TestTaskGroupRepo -v
```

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/sqlite/task_group.go internal/repository/sqlite/task_group_test.go
git commit -m "feat(sqlite): TaskGroupRepository with hybrid persistence

Identity (id, name, created_at, created_by, total_tasks, metadata)
persists; aggregate progress remains derived from tasks at read time.
Update-on-conflict only touches name and metadata to preserve the
batch-creation invariants."
```

---

## Task 7: Wire repos into Repositories struct + Transaction

**Files:**
- Modify: `internal/repository/repository.go`
- Modify: `internal/repository/sqlite/sqlite.go`

- [ ] **Step 1: Add repo references to the Repositories struct**

Replace the `Repositories` struct in `internal/repository/repository.go` (around line 117-124) with:

```go
// Repositories provides access to all repositories
type Repositories struct {
	Devices    DeviceRepository
	Orchs      OrchRepository
	OrchNodes  OrchNodeRepository
	Metrics    MetricsRepository
	Tasks      domain.TaskRepository
	TaskGroups TaskGroupRepository
	UnitOfWork UnitOfWork
}

// TaskGroupRepository defines operations for task group persistence.
// (Tasks use domain.TaskRepository so domain.TaskQueue can depend on it
// without importing this package.)
type TaskGroupRepository interface {
	Save(ctx context.Context, group *domain.TaskGroup) error
	GetByID(ctx context.Context, id string) (*domain.TaskGroup, error)
}
```

(`domain` is already imported at the top of the file.)

- [ ] **Step 2: Populate repos in Repositories() and Transaction methods**

In `internal/repository/sqlite/sqlite.go::Repositories()`:

```go
func (d *DB) Repositories() *repository.Repositories {
	return &repository.Repositories{
		Devices:    NewDeviceRepository(d.db),
		Orchs:      NewOrchRepository(d.db),
		OrchNodes:  NewOrchNodeRepository(d.db),
		Metrics:    NewMetricsRepository(d.db),
		Tasks:      NewTaskRepository(d.db),
		TaskGroups: NewTaskGroupRepository(d.db),
		UnitOfWork: d,
	}
}
```

Update the `Transaction` interface in `internal/repository/repository.go` to expose the new repos:

```go
type Transaction interface {
	Commit() error
	Rollback() error
	Devices() DeviceRepository
	Orchs() OrchRepository
	OrchNodes() OrchNodeRepository
	Metrics() MetricsRepository
	Tasks() domain.TaskRepository
	TaskGroups() TaskGroupRepository
}
```

And in `internal/repository/sqlite/sqlite.go`, add the corresponding methods on `Transaction`:

```go
func (t *Transaction) Tasks() domain.TaskRepository {
	return &TaskRepository{db: t.tx}
}

func (t *Transaction) TaskGroups() repository.TaskGroupRepository {
	return &TaskGroupRepository{db: t.tx}
}
```

(Add `"github.com/s1ckdark/hydra/internal/domain"` to the import block of `sqlite.go` if not already there.)

- [ ] **Step 3: Verify build + existing tests**

```bash
go build ./...
go test ./internal/repository/... ./internal/usecase/... ./internal/web/handler/...
```

Expected: clean build, all tests pass (config tests aside — pre-existing failure unrelated).

- [ ] **Step 4: Commit**

```bash
git add internal/repository/repository.go internal/repository/sqlite/sqlite.go
git commit -m "feat(repos): wire Tasks and TaskGroups into Repositories + Transaction"
```

---

## Task 8: TaskQueue write-through

**Files:**
- Modify: `internal/domain/taskqueue.go`
- Create: `internal/domain/taskqueue_writethrough_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/domain/taskqueue_writethrough_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"
)

type stubTaskRepo struct {
	saveCalls int
	saveErr   error
	lastTask  *Task
}

func (r *stubTaskRepo) Save(_ context.Context, t *Task) error {
	r.saveCalls++
	r.lastTask = t
	return r.saveErr
}
func (r *stubTaskRepo) Delete(_ context.Context, _ string) error { return nil }

func TestTaskQueue_EnqueueWritesThrough(t *testing.T) {
	q := NewTaskQueue()
	repo := &stubTaskRepo{}
	q.WithRepo(repo)

	q.Enqueue(&Task{ID: "t1", Type: "shell"})

	if repo.saveCalls != 1 {
		t.Errorf("save calls = %d, want 1", repo.saveCalls)
	}
	if repo.lastTask == nil || repo.lastTask.ID != "t1" {
		t.Errorf("lastTask = %+v", repo.lastTask)
	}
}

func TestTaskQueue_RepoFailureDoesNotBlockEnqueue(t *testing.T) {
	q := NewTaskQueue()
	repo := &stubTaskRepo{saveErr: errors.New("db down")}
	q.WithRepo(repo)

	q.Enqueue(&Task{ID: "t2", Type: "shell"})

	// In-memory still has it.
	got := q.Get("t2")
	if got == nil {
		t.Fatal("task missing from in-memory queue after repo failure")
	}
	if got.Status != TaskStatusQueued {
		t.Errorf("status = %s, want queued", got.Status)
	}
}

func TestTaskQueue_UpdateStatusWritesThrough(t *testing.T) {
	q := NewTaskQueue()
	repo := &stubTaskRepo{}
	q.WithRepo(repo)
	q.Enqueue(&Task{ID: "t3", Type: "shell"})
	repo.saveCalls = 0 // reset

	q.UpdateStatus("t3", TaskStatusRunning)

	if repo.saveCalls != 1 {
		t.Errorf("save calls = %d, want 1", repo.saveCalls)
	}
}
```

- [ ] **Step 2: Run tests to confirm fail**

```bash
go test ./internal/domain -run TestTaskQueue_ -v
```

Expected: build error `WithRepo undefined`.

- [ ] **Step 3: Add repo plumbing to TaskQueue**

In `internal/domain/taskqueue.go`, modify the `TaskQueue` struct and add helpers:

```go
type TaskQueue struct {
	mu    sync.RWMutex
	tasks map[string]*Task
	queue []*Task
	repo  TaskRepository // optional write-through; nil disables persistence
}

// WithRepo enables write-through persistence on this queue. Returns the
// queue for fluent chaining at construction sites.
func (q *TaskQueue) WithRepo(r TaskRepository) *TaskQueue {
	q.mu.Lock()
	q.repo = r
	q.mu.Unlock()
	return q
}

// persist writes a task to the repo if one is configured. Errors are logged
// and swallowed — DB downtime must not stall the in-memory scheduler.
func (q *TaskQueue) persist(t *Task) {
	if q.repo == nil || t == nil {
		return
	}
	if err := q.repo.Save(context.Background(), t); err != nil {
		log.Printf("[taskqueue] persist failed for %s: %v", t.ID, err)
	}
}
```

Add the imports `"context"` and `"log"` to the file's import block.

Then sprinkle `q.persist(task)` after every mutation. Specifically, modify these methods (keep the existing logic, just add one line each):

- `Enqueue`: after `q.insertByPriority(task)`, append `q.persist(task)`
- `FindMatchingTask`: inside the if-match block right before the `return task`, append `q.persist(task)`
- `UpdateStatus`: right before the final `return task`, append `q.persist(task)`
- `SetResult`: right before the final `return task`, append `q.persist(task)`
- `AssignToDevice`: right before the final `return task`, append `q.persist(task)`
- `FindMatchingTaskWithAI`: inside the assigned block right before `return task`, append `q.persist(task)`
- `CheckTimeouts`: inside the inner mutation block (both the failure path and the requeue path) right after the mutation, append `q.persist(task)`
- `ReassignTasksFromDevice`: same as above — both paths

> **Important:** all `q.persist()` calls must run while still inside the existing mutex (writes are atomic), but after the in-memory mutation. Do not call persist from outside the locked section.

- [ ] **Step 4: Run tests to confirm pass**

```bash
go test ./internal/domain -run TestTaskQueue_ -v
go test ./internal/domain -run TestDeriveGroupStatus -v
go test ./internal/usecase -run TestScheduleWith -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/taskqueue.go internal/domain/taskqueue_writethrough_test.go
git commit -m "feat(domain): TaskQueue write-through to TaskRepository

Each mutation method (Enqueue/FindMatching*/UpdateStatus/SetResult/
AssignToDevice/CheckTimeouts/ReassignTasksFromDevice) calls a persist
helper after the in-memory update. Repo failures are logged and
swallowed so the scheduler keeps running even if the DB is down.

WithRepo() is fluent so cmd/server/main.go can chain it onto
NewTaskQueue() without changing the constructor signature."
```

---

## Task 9: APIGetGroup handler

**Files:**
- Create: `internal/web/handler/task_group_handler.go`
- Create: `internal/web/handler/task_group_handler_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/web/handler/task_group_handler_test.go`:

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/internal/domain"
)

// In-memory fakes good enough for handler tests.
type fakeTaskGroupRepo struct {
	groups map[string]*domain.TaskGroup
}

func (f *fakeTaskGroupRepo) Save(_ echo.Context, _ *domain.TaskGroup) error { return nil }
func newFakeGroupRepo() *fakeTaskGroupRepo { return &fakeTaskGroupRepo{groups: map[string]*domain.TaskGroup{}} }

func TestAPIGetGroup_NotFound(t *testing.T) {
	h := newGroupHandlerForTest(t, nil, nil)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/groups/missing", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("missing")

	if err := h.APIGetGroup(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestAPIGetGroup_DerivedStatus(t *testing.T) {
	group := &domain.TaskGroup{ID: "g1", TotalTasks: 3, CreatedAt: time.Now()}
	tasks := []*domain.Task{
		{ID: "t1", GroupID: "g1", Status: domain.TaskStatusCompleted},
		{ID: "t2", GroupID: "g1", Status: domain.TaskStatusFailed},
		{ID: "t3", GroupID: "g1", Status: domain.TaskStatusCompleted},
	}
	h := newGroupHandlerForTest(t, []*domain.TaskGroup{group}, tasks)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("g1")

	if err := h.APIGetGroup(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"partial"`) {
		t.Errorf("body missing partial status: %s", body)
	}
	if !strings.Contains(body, `"completed":2`) || !strings.Contains(body, `"failed":1`) {
		t.Errorf("body counts wrong: %s", body)
	}
	// Lightweight default — tasks[] should be absent.
	if strings.Contains(body, `"tasks":[`) {
		t.Errorf("default response should not include tasks[]: %s", body)
	}
}

func TestAPIGetGroup_DetailFull(t *testing.T) {
	group := &domain.TaskGroup{ID: "g2", TotalTasks: 1, CreatedAt: time.Now()}
	tasks := []*domain.Task{{ID: "t1", GroupID: "g2", Status: domain.TaskStatusCompleted}}
	h := newGroupHandlerForTest(t, []*domain.TaskGroup{group}, tasks)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g2?detail=full", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("g2")
	c.QueryParams().Set("detail", "full")

	if err := h.APIGetGroup(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"tasks":[`) {
		t.Errorf("?detail=full should include tasks[]: %s", rec.Body.String())
	}
}
```

Add a test helper at the end of the file:

```go
// stub repos used by handler tests.
type stubTaskGroupRepo struct{ data map[string]*domain.TaskGroup }
type stubTaskRepoForGroup struct{ data map[string][]*domain.Task }

func (s *stubTaskGroupRepo) Save(_ context.Context, g *domain.TaskGroup) error {
	if s.data == nil {
		s.data = map[string]*domain.TaskGroup{}
	}
	s.data[g.ID] = g
	return nil
}
func (s *stubTaskGroupRepo) GetByID(_ context.Context, id string) (*domain.TaskGroup, error) {
	g, ok := s.data[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return g, nil
}
func (s *stubTaskRepoForGroup) Save(_ context.Context, _ *domain.Task) error  { return nil }
func (s *stubTaskRepoForGroup) Delete(_ context.Context, _ string) error      { return nil }
func (s *stubTaskRepoForGroup) GetByGroup(_ context.Context, gid string) ([]*domain.Task, error) {
	return s.data[gid], nil
}

func newGroupHandlerForTest(t *testing.T, groups []*domain.TaskGroup, tasks []*domain.Task) *Handler {
	t.Helper()
	gRepo := &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}}
	for _, g := range groups {
		gRepo.data[g.ID] = g
	}
	tRepo := &stubTaskRepoForGroup{data: map[string][]*domain.Task{}}
	for _, t := range tasks {
		tRepo.data[t.GroupID] = append(tRepo.data[t.GroupID], t)
	}
	return &Handler{
		taskGroupRepo: gRepo,
		taskGroupTasks: tRepo,
	}
}
```

(Add `import "context"` and `import "database/sql"` to the test file.)

- [ ] **Step 2: Run tests to confirm fail**

```bash
go test ./internal/web/handler -run TestAPIGetGroup -v
```

Expected: build error — `APIGetGroup`, `taskGroupRepo`, `taskGroupTasks` undefined.

- [ ] **Step 3: Add Handler fields and the APIGetGroup method**

In `internal/web/handler/handler.go`, add to the `Handler` struct:

```go
type Handler struct {
	// ...existing fields
	taskGroupRepo  taskGroupReader     // GetByID
	taskGroupTasks taskGroupTasksReader // GetByGroup
}

// taskGroupReader is the subset of repository.TaskGroupRepository this
// handler needs. Defined locally so tests can supply a stub without
// pulling the sqlite package.
type taskGroupReader interface {
	GetByID(ctx context.Context, id string) (*domain.TaskGroup, error)
}

// taskGroupTasksReader is the subset of TaskRepository for group reads.
type taskGroupTasksReader interface {
	GetByGroup(ctx context.Context, groupID string) ([]*domain.Task, error)
}

// SetTaskGroupRepos wires the read-side dependencies for /api/groups.
func (h *Handler) SetTaskGroupRepos(g taskGroupReader, t taskGroupTasksReader) {
	h.taskGroupRepo = g
	h.taskGroupTasks = t
}
```

(Add `"github.com/s1ckdark/hydra/internal/domain"` and `"context"` to the imports if not there.)

Create `internal/web/handler/task_group_handler.go`:

```go
package handler

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/internal/domain"
)

// APIGetGroup returns a TaskGroupSnapshot. With ?detail=full the response
// also embeds every member task; the default lightweight response only
// includes counters and derived status.
func (h *Handler) APIGetGroup(c echo.Context) error {
	if h.taskGroupRepo == nil || h.taskGroupTasks == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task group service not available"})
	}
	id := c.Param("id")
	ctx := c.Request().Context()

	group, err := h.taskGroupRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "group not found"})
		}
		return internalError(c, "failed to load group", err)
	}

	tasks, err := h.taskGroupTasks.GetByGroup(ctx, id)
	if err != nil {
		return internalError(c, "failed to load group tasks", err)
	}

	snap := buildGroupSnapshot(group, tasks)
	if c.QueryParam("detail") != "full" {
		snap.Tasks = nil
	}
	return c.JSON(http.StatusOK, snap)
}

func buildGroupSnapshot(g *domain.TaskGroup, tasks []*domain.Task) domain.TaskGroupSnapshot {
	snap := domain.TaskGroupSnapshot{TaskGroup: *g, Tasks: tasks}
	for _, t := range tasks {
		switch t.Status {
		case domain.TaskStatusCompleted:
			snap.Completed++
		case domain.TaskStatusFailed, domain.TaskStatusCancelled:
			snap.Failed++
		case domain.TaskStatusRunning:
			snap.Running++
		case domain.TaskStatusQueued, domain.TaskStatusAssigned, domain.TaskStatusPending:
			snap.Queued++
		}
	}
	snap.Status = domain.DeriveGroupStatus(tasks, g.TotalTasks)
	return snap
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/web/handler -run TestAPIGetGroup -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/handler/handler.go internal/web/handler/task_group_handler.go internal/web/handler/task_group_handler_test.go
git commit -m "feat(handler): GET /api/groups/:id with derived status

Lightweight default response carries counters and tri-state status; a
?detail=full query embeds the full member task list. Local interfaces
on Handler keep test stubs straightforward without pulling sqlite or
the full Repositories struct into handler tests."
```

---

## Task 10: APITaskBatchCreate handler

**Files:**
- Modify: `internal/web/handler/task_group_handler.go` (extend with new handler)
- Modify: `internal/web/handler/task_group_handler_test.go` (extend with new tests)
- Modify: `internal/web/handler/handler.go` (Handler stores taskGroupSaver)

- [ ] **Step 1: Add the failing test**

Append to `internal/web/handler/task_group_handler_test.go`:

```go
func TestAPITaskBatchCreate_HappyPath(t *testing.T) {
	gRepo := &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}}
	tRepo := &stubTaskRepoForGroup{data: map[string][]*domain.Task{}}
	tq := domain.NewTaskQueue()
	h := &Handler{
		taskGroupRepo:    gRepo,
		taskGroupTasks:   tRepo,
		taskGroupSaver:   gRepo,
		taskQueue:        tq,
	}

	e := echo.New()
	body := `{"name":"qa","tasks":[{"type":"shell"},{"type":"shell"},{"type":"shell"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.APITaskBatchCreate(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(gRepo.data) != 1 {
		t.Errorf("group not saved")
	}
	if cnt := tq.PendingCount(); cnt != 3 {
		t.Errorf("queued count = %d, want 3", cnt)
	}
}

func TestAPITaskBatchCreate_EmptyTasks(t *testing.T) {
	tq := domain.NewTaskQueue()
	h := &Handler{
		taskGroupRepo:  &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}},
		taskGroupTasks: &stubTaskRepoForGroup{data: map[string][]*domain.Task{}},
		taskGroupSaver: &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}},
		taskQueue:      tq,
	}
	e := echo.New()
	body := `{"tasks":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.APITaskBatchCreate(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAPITaskBatchCreate_InvalidTaskRollsBack(t *testing.T) {
	gRepo := &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}}
	tq := domain.NewTaskQueue()
	h := &Handler{
		taskGroupRepo:  &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}},
		taskGroupTasks: &stubTaskRepoForGroup{data: map[string][]*domain.Task{}},
		taskGroupSaver: gRepo,
		taskQueue:      tq,
	}
	e := echo.New()
	body := `{"tasks":[{"type":"shell"},{"type":""}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.APITaskBatchCreate(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if len(gRepo.data) != 0 {
		t.Errorf("group should not be saved on validation failure: %+v", gRepo.data)
	}
	if tq.PendingCount() != 0 {
		t.Errorf("queue should be empty after rollback, got %d", tq.PendingCount())
	}
}
```

- [ ] **Step 2: Add Handler.taskGroupSaver field**

Append to `internal/web/handler/handler.go`'s Handler struct:

```go
type Handler struct {
	// ...existing fields
	taskGroupSaver taskGroupSaver
}

type taskGroupSaver interface {
	Save(ctx context.Context, group *domain.TaskGroup) error
}
```

Update `SetTaskGroupRepos` to also accept the saver:

```go
func (h *Handler) SetTaskGroupRepos(g interface {
	taskGroupReader
	taskGroupSaver
}, t taskGroupTasksReader) {
	h.taskGroupRepo = g
	h.taskGroupSaver = g
	h.taskGroupTasks = t
}
```

- [ ] **Step 3: Run tests to confirm fail**

```bash
go test ./internal/web/handler -run TestAPITaskBatchCreate -v
```

Expected: build error — `APITaskBatchCreate` undefined.

- [ ] **Step 4: Implement the handler**

Append to `internal/web/handler/task_group_handler.go`:

```go
// taskBatchRequest is the inbound JSON for POST /api/tasks/batch.
type taskBatchRequest struct {
	Name     string                 `json:"name"`
	Metadata map[string]interface{} `json:"metadata"`
	Tasks    []taskBatchEntry       `json:"tasks"`
}

type taskBatchEntry struct {
	Type                 string                 `json:"type"`
	Priority             string                 `json:"priority"`
	RequiredCapabilities []string               `json:"requiredCapabilities"`
	PreferredDeviceID    string                 `json:"preferredDeviceId"`
	Payload              map[string]interface{} `json:"payload"`
	Timeout              int                    `json:"timeout"`
	MaxRetries           int                    `json:"maxRetries"`
	AISchedule           *bool                  `json:"aiSchedule"`
}

// APITaskBatchCreate creates a TaskGroup and N tasks atomically. On any
// validation failure no group row is written and no task is enqueued.
// After successful enqueue the supervisor's ScheduleNow is invoked once
// so the whole batch is considered for placement in a single pass.
func (h *Handler) APITaskBatchCreate(c echo.Context) error {
	if h.taskQueue == nil || h.taskGroupSaver == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task batch service not available"})
	}

	var req taskBatchRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if len(req.Tasks) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "tasks: must contain at least one task"})
	}

	for i, e := range req.Tasks {
		if e.Type == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "tasks[" + itoa(i) + "]: type is required",
			})
		}
	}

	ctx := c.Request().Context()
	now := time.Now()

	group := &domain.TaskGroup{
		ID:         generateID(),
		Name:       req.Name,
		CreatedAt:  now,
		TotalTasks: len(req.Tasks),
		Metadata:   req.Metadata,
	}
	if err := h.taskGroupSaver.Save(ctx, group); err != nil {
		return internalError(c, "failed to save group", err)
	}

	created := make([]*domain.Task, 0, len(req.Tasks))
	for _, e := range req.Tasks {
		task := &domain.Task{
			ID:                   generateID(),
			Type:                 e.Type,
			Status:               domain.TaskStatusPending,
			Priority:             domain.TaskPriority(e.Priority),
			RequiredCapabilities: e.RequiredCapabilities,
			PreferredDeviceID:    e.PreferredDeviceID,
			Payload:              e.Payload,
			Timeout:              time.Duration(e.Timeout) * time.Second,
			MaxRetries:           e.MaxRetries,
			AISchedule:           e.AISchedule,
			GroupID:              group.ID,
		}
		if task.Priority == "" {
			task.Priority = domain.TaskPriorityNormal
		}
		h.taskQueue.Enqueue(task)
		created = append(created, task)
	}

	if h.taskSupervisor != nil {
		h.taskSupervisor.ScheduleNow(ctx)
	}

	// Refresh state from queue (some may have been assigned by ScheduleNow).
	for i, t := range created {
		if updated := h.taskQueue.Get(t.ID); updated != nil {
			created[i] = updated
		}
	}

	snap := domain.TaskGroupSnapshot{
		TaskGroup: *group,
		Tasks:     created,
	}
	for _, t := range created {
		switch t.Status {
		case domain.TaskStatusCompleted:
			snap.Completed++
		case domain.TaskStatusFailed, domain.TaskStatusCancelled:
			snap.Failed++
		case domain.TaskStatusRunning:
			snap.Running++
		default:
			snap.Queued++
		}
	}
	snap.Status = domain.DeriveGroupStatus(created, group.TotalTasks)

	return c.JSON(http.StatusCreated, snap)
}

// itoa avoids strconv import for a one-shot integer-to-string conversion.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var (
		buf [12]byte
		n   = len(buf)
		neg = i < 0
	)
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
```

(`time` should already be imported; `generateID` is reused from `task_handler.go`.)

- [ ] **Step 5: Run tests to confirm pass**

```bash
go test ./internal/web/handler -run TestAPITaskBatchCreate -v
go test ./internal/web/handler -run TestAPIGetGroup -v
```

Expected: 3 + 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/handler/handler.go internal/web/handler/task_group_handler.go internal/web/handler/task_group_handler_test.go
git commit -m "feat(handler): POST /api/tasks/batch with single-pass scheduling

Inserts the task_groups row first; on any task validation failure
returns 400 with no group row written. After enqueueing all N tasks
calls taskSupervisor.ScheduleNow once so the whole batch goes through
the supervisor's AI-aware placement pass in a single tick rather than
N separate calls."
```

---

## Task 11: Single-task POST response surfaces groupId

**Files:**
- Verify: `internal/web/handler/task_handler.go`

The Task struct now has `GroupID string \`json:"groupId,omitempty"\``, so JSON marshalling includes it automatically when set. The single-task POST endpoint always returns a Task with `GroupID == ""`, which is omitted by `omitempty`.

- [ ] **Step 1: Confirm with a smoke test**

```bash
go build -o /tmp/hydra-server-test ./cmd/server
# kill any running server first
/bin/ps auxww | grep hydra-server-test | grep -v grep | awk '{print $2}' | xargs -r kill -9 2>&1
sleep 1
nohup /tmp/hydra-server-test > /tmp/hydra-server.log 2>&1 & disown
sleep 2

curl -s -X POST http://127.0.0.1:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{"type":"shell","payload":{"command":"echo single"}}' \
  | python3 -m json.tool | grep -i group
```

Expected: no `groupId` in output (because it's empty + `omitempty`).

- [ ] **Step 2: No code change, no commit**

This task is a verification step only.

---

## Task 12: Wire batch routes + taskQueue.WithRepo in main.go

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Wire the write-through repo and the task-group repos**

Find the `taskQueue := domain.NewTaskQueue()` line and replace with:

```go
	taskQueue := domain.NewTaskQueue().WithRepo(repos.Tasks)
```

Find `h.SetTaskQueue(taskQueue)` and immediately after it add:

```go
	h.SetTaskGroupRepos(repos.TaskGroups, repos.Tasks.(interface {
		GetByGroup(ctx context.Context, groupID string) ([]*domain.Task, error)
	}))
```

(`context` and `domain` are already imported in `main.go`.)

- [ ] **Step 2: Register the new routes**

Find the existing task-routes block:

```go
	// Task API routes
	api.GET("/tasks", h.APITaskList)
```

and add right below the existing `apiWrite.POST("/tasks", ...)` line (so the batch endpoint inherits the same Tailscale auth):

```go
	apiWrite.POST("/tasks/batch", h.APITaskBatchCreate)
```

Then add the read endpoint near the existing `/api/config/...` group, but on the unauthenticated `api` group (group reads are read-only):

```go
	api.GET("/groups/:id", h.APIGetGroup)
```

- [ ] **Step 3: Build**

```bash
go build -o /tmp/hydra-server-test ./cmd/server
```

Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): register fan-out batch routes and wire task write-through

POST /api/tasks/batch is mounted under the existing Tailscale-auth
group (same protections as POST /api/tasks). GET /api/groups/:id is on
the read-only api group. taskQueue is constructed with WithRepo so
every queue mutation persists to the new tasks table."
```

---

## Task 13: End-to-end verification

- [ ] **Step 1: Restart server with fresh build**

```bash
/bin/ps auxww | grep -E "/tmp/wstub|hydra-server-test" | grep -v grep | awk '{print $2}' | xargs -r kill -9 2>&1
sleep 2
nohup /tmp/hydra-server-test > /tmp/hydra-server.log 2>&1 & disown
sleep 2
grep -E "supervisor|Starting server" /tmp/hydra-server.log | head -5
```

- [ ] **Step 2: Connect 3 wstub workers with mixed capabilities**

```bash
nohup /tmp/wstub --device-ids="8778566099601701" --capabilities="gpu,compute,network" > /tmp/wstub-gpu.log 2>&1 & disown
nohup /tmp/wstub --device-ids="2990256844522528" --capabilities="compute,network" > /tmp/wstub-cpu1.log 2>&1 & disown
nohup /tmp/wstub --device-ids="2965676528612458" --capabilities="compute,network" > /tmp/wstub-cpu2.log 2>&1 & disown
sleep 4
```

- [ ] **Step 3: Submit a 6-task batch (mixed capability requirements)**

```bash
GROUP=$(curl -s -X POST http://127.0.0.1:8080/api/tasks/batch \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"e2e-1",
    "tasks":[
      {"type":"shell","payload":{"command":"echo 1"}},
      {"type":"shell","payload":{"command":"echo 2"}},
      {"type":"shell","payload":{"command":"echo 3"}},
      {"type":"shell","requiredCapabilities":["gpu"],"payload":{"command":"echo gpu1"}},
      {"type":"shell","requiredCapabilities":["gpu"],"payload":{"command":"echo gpu2"}},
      {"type":"shell","payload":{"command":"echo 6"}}
    ]
  }' | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
echo "GROUP=$GROUP"
```

Expected: a non-empty GROUP id.

- [ ] **Step 4: Verify spread + capability filtering in the supervisor log**

```bash
grep -E "supervisor.*tick|task.*assigned" /tmp/hydra-server.log | tail -15
```

Expected: one tick line (single ScheduleNow), 6 assignment lines. The two gpu-tagged tasks should both land on `8778566099601701` (the only gpu-capable stub); the other 4 tasks should be distributed across all 3 workers (the first non-gpu task may go anywhere, subsequent ones spread because of the bumpRunningJobs adjustment in the in-tick snapshots).

- [ ] **Step 5: Verify the GET endpoint**

```bash
curl -s "http://127.0.0.1:8080/api/groups/$GROUP" | python3 -m json.tool
echo ""
curl -s "http://127.0.0.1:8080/api/groups/$GROUP?detail=full" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print('status', d['status'], 'completed', d['completed'], 'failed', d['failed'], 'queued', d['queued'])
print('tasks:')
for t in d['tasks']:
    print(' ', t['id'][:20], t['status'], 'on', t.get('assignedDeviceId',''))
"
```

Expected: lightweight response shows counters and `running` status; full response lists 6 tasks with their assignedDeviceIds.

- [ ] **Step 6: Verify DB persistence directly**

```bash
sqlite3 ~/.hydra/hydra.db "SELECT id, total_tasks, name FROM task_groups ORDER BY created_at DESC LIMIT 3"
echo "---"
sqlite3 ~/.hydra/hydra.db "SELECT id, status, group_id, assigned_device_id FROM tasks WHERE group_id='$GROUP' ORDER BY created_at"
```

Expected: one `task_groups` row matching `$GROUP` with `total_tasks=6`; six `tasks` rows all with `group_id=$GROUP`.

- [ ] **Step 7: Empty-batch validation**

```bash
curl -s -o /dev/null -w "HTTP %{http_code}\n" -X POST http://127.0.0.1:8080/api/tasks/batch \
  -H 'Content-Type: application/json' \
  -d '{"tasks":[]}'
```

Expected: `HTTP 400`.

- [ ] **Step 8: Invalid-task rollback**

```bash
BEFORE=$(sqlite3 ~/.hydra/hydra.db "SELECT COUNT(*) FROM task_groups")
curl -s -o /dev/null -w "HTTP %{http_code}\n" -X POST http://127.0.0.1:8080/api/tasks/batch \
  -H 'Content-Type: application/json' \
  -d '{"tasks":[{"type":"shell"},{"type":""}]}'
AFTER=$(sqlite3 ~/.hydra/hydra.db "SELECT COUNT(*) FROM task_groups")
echo "groups before=$BEFORE after=$AFTER"
```

Expected: `HTTP 400` and `before == after` (no group row added on validation failure).

- [ ] **Step 9: Push to PR**

```bash
git push origin claude/gifted-feynman-8f2d79
```

- [ ] **Step 10: Final note**

This finishes the C-β fan-out implementation. The cleanup the plan deferred — restart-time queue hydration, group cancellation, list endpoint, retention/TTL — should each be tracked as their own follow-up issues / PRs.

---

## Self-review checklist (run after writing the plan)

- **Spec coverage:** Goals (batch POST, polling, tri-state, scheduler reuse, additive, persistence) → Tasks 1–10 + 12. Non-Goals are not implemented (correct).
- **Type consistency:** `TaskGroupStatus` constants used identically in domain, handlers, tests. `TaskRepository` interface signature consistent across domain/, sqlite/, taskqueue.
- **Placeholder scan:** No "TBD"/"add error handling"/"similar to". Each handler/repo step has full code.
- **Order:** Domain types → repo interface → migration → repo impl → wiring → write-through → handlers → server wiring → e2e. Each task compiles independently of the next.
