package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"taskmanager/internal/domain"
)

func TestStore_Load_Save(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := New(path)
	defer s.Close()

	// Load empty -> new list
	list, err := s.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if list == nil || list.NextID != 1 || len(list.Tasks) != 0 {
		t.Errorf("expected new TaskList; got %+v", list)
	}

	// Add task and save
	list.Tasks = append(list.Tasks, domain.Task{
		ID: 1, Title: "test", Status: domain.StatusTodo,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	list.NextID = 2
	if err := s.Save(list); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	// Load again
	list2, err := s.Load()
	if err != nil {
		t.Fatalf("Load() second time: %v", err)
	}
	if len(list2.Tasks) != 1 || list2.Tasks[0].Title != "test" || list2.NextID != 2 {
		t.Errorf("expected one task and NextID=2; got %+v", list2)
	}
}

func TestStore_Load_NoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	s := New(path)
	defer s.Close()

	list, err := s.Load()
	if err != nil {
		t.Fatalf("Load() on missing file should return new list, got err: %v", err)
	}
	if list == nil || list.NextID != 1 {
		t.Errorf("expected new TaskList; got %+v", list)
	}
}

func TestStore_Save_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "tasks.json")
	s := New(path)
	defer s.Close()

	list := domain.NewTaskList()
	list.Tasks = append(list.Tasks, domain.Task{
		ID: 1, Title: "x", Status: domain.StatusTodo,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	list.NextID = 2

	if err := s.Save(list); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save() should create directory and file: %v", err)
	}
}

func TestStore_Save_Backup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := New(path)
	defer s.Close()

	list := domain.NewTaskList()
	list.Tasks = append(list.Tasks, domain.Task{
		ID: 1, Title: "first", Status: domain.StatusTodo,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	list.NextID = 2
	if err := s.Save(list); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	list.Tasks[0].Title = "second"
	if err := s.Save(list); err != nil {
		t.Fatalf("Save() second: %v", err)
	}
	backupPath := path + ".bak"
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("Save() should create backup file: %v", err)
	}
}
