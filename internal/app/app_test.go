package app

import (
	"path/filepath"
	"testing"
	"time"

	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

func TestApp_AddTask_List_Done_Delete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	st := store.New(path)
	defer st.Close()
	a := New(st)

	task, err := a.AddTask("buy milk", "", domain.PriorityMed, nil, nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if task.ID != 1 || task.Title != "buy milk" || task.Status != domain.StatusTodo {
		t.Errorf("unexpected task: %+v", task)
	}

	// Wait for debounced autosave
	time.Sleep(400 * time.Millisecond)

	tasks, err := a.ListTasks(FilterAll, SortByCreatedAt, false, false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "buy milk" {
		t.Errorf("expected one task; got %+v", tasks)
	}

	if err := a.Done(1); err != nil {
		t.Fatalf("Done: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	tasks, _ = a.ListTasks(FilterDone, SortByCreatedAt, false, false)
	if len(tasks) != 1 || tasks[0].Status != domain.StatusDone {
		t.Errorf("expected task done; got %+v", tasks)
	}

	if err := a.Delete(1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	tasks, _ = a.ListTasks(FilterAll, SortByCreatedAt, false, false)
	if len(tasks) != 0 {
		t.Errorf("expected no tasks; got %d", len(tasks))
	}
}

func TestApp_AddTask_EmptyTitle(t *testing.T) {
	dir := t.TempDir()
	st := store.New(filepath.Join(dir, "tasks.json"))
	defer st.Close()
	a := New(st)

	_, err := a.AddTask("", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestApp_Done_NotFound(t *testing.T) {
	dir := t.TempDir()
	st := store.New(filepath.Join(dir, "tasks.json"))
	defer st.Close()
	a := New(st)

	err := a.Done(99)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestApp_ClearDone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	st := store.New(path)
	defer st.Close()
	a := New(st)

	_, _ = a.AddTask("one", "", "", nil, nil)
	time.Sleep(400 * time.Millisecond)
	_, _ = a.AddTask("two", "", "", nil, nil)
	time.Sleep(400 * time.Millisecond)
	_ = a.Done(1)
	time.Sleep(400 * time.Millisecond)

	count, err := a.ClearDone()
	if err != nil {
		t.Fatalf("ClearDone: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1; got %d", count)
	}
	time.Sleep(400 * time.Millisecond)
	tasks, _ := a.ListTasks(FilterAll, SortByCreatedAt, false, false)
	if len(tasks) != 1 || tasks[0].ID != 2 {
		t.Errorf("expected one task left (id=2); got %+v", tasks)
	}
}
