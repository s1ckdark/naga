package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/internal/domain"
)

// stubTaskGroupRepo / stubTaskRepoForGroup are local stubs used by handler
// tests so we don't pull sqlite or full Repositories into this package's tests.
type stubTaskGroupRepo struct {
	data map[string]*domain.TaskGroup
}

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

type stubTaskRepoForGroup struct {
	data map[string][]*domain.Task
}

func (s *stubTaskRepoForGroup) Save(_ context.Context, _ *domain.Task) error { return nil }
func (s *stubTaskRepoForGroup) Delete(_ context.Context, _ string) error     { return nil }
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
	for _, tk := range tasks {
		tRepo.data[tk.GroupID] = append(tRepo.data[tk.GroupID], tk)
	}
	h := &Handler{}
	h.SetTaskGroupRepos(gRepo, tRepo)
	return h
}

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

func TestAPITaskBatchCreate_HappyPath(t *testing.T) {
	gRepo := &stubTaskGroupRepo{data: map[string]*domain.TaskGroup{}}
	tRepo := &stubTaskRepoForGroup{data: map[string][]*domain.Task{}}
	tq := domain.NewTaskQueue()
	h := &Handler{
		taskGroupRepo:  gRepo,
		taskGroupTasks: tRepo,
		taskGroupSaver: gRepo,
		taskQueue:      tq,
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
