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
