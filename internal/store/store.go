// Package store provides persistent JSON file storage for tasks with atomic
// writes, backup, and an optional debounced autosave goroutine.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"taskmanager/internal/domain"
)

const autosaveDebounce = 300 * time.Millisecond

// Store handles reading and writing tasks to a JSON file with atomic writes
// and optional debounced autosave.
type Store struct {
	path    string
	mu      sync.RWMutex
	saveCh  chan *domain.TaskList
	done    chan struct{}
	wg      sync.WaitGroup
	closeOnce sync.Once
}

// New creates a Store that uses the given file path.
// Starts a background goroutine for debounced autosave; call Close() to stop it.
func New(path string) *Store {
	s := &Store{
		path:   path,
		saveCh: make(chan *domain.TaskList, 1),
		done:   make(chan struct{}),
	}
	s.wg.Add(1)
	go s.autosaveWorker()
	return s
}

// autosaveWorker reads save requests, debounces (300ms), then writes once.
func (s *Store) autosaveWorker() {
	defer s.wg.Done()
	var pending *domain.TaskList
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C//wait for the timer to expire
	}
	defer timer.Reset(0)

	for {//loop until the channel is closed
		select {
		case list := <-s.saveCh://new task list is received from the channel
			pending = list
			timer.Reset(autosaveDebounce)
		case <-timer.C://timer expires
			if pending != nil {
				_ = s.Save(pending)//save the task list to the file
				pending = nil
			}
		case <-s.done://channel is closed
			if pending != nil {
				_ = s.Save(pending)
			}
			return
		}
	}
}

// Close stops the autosave worker and waits for it to finish.
// Safe to call multiple times; only the first call does work.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
	return nil
}

// RequestSave schedules a save after the debounce period (autosave). Non-blocking.
func (s *Store) RequestSave(list *domain.TaskList) {
	select {
	case s.saveCh <- list:
	default:
		<-s.saveCh // drop oldest; prefer latest
		s.saveCh <- list
	}
}

// Load reads all tasks from the JSON file.
// Returns a new empty TaskList if the file does not exist or is empty.
func (s *Store) Load() (*domain.TaskList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.NewTaskList(), nil
		}
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	var list domain.TaskList
	if len(data) == 0 {
		return domain.NewTaskList(), nil
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}
	if list.Tasks == nil {
		list.Tasks = make([]domain.Task, 0)
	}
	// Normalize legacy or missing status
	for i := range list.Tasks {
		if list.Tasks[i].Status == "" {
			if list.Tasks[i].LegacyCompleted || list.Tasks[i].CompletedAt != nil {
				list.Tasks[i].Status = domain.StatusDone
			} else {
				list.Tasks[i].Status = domain.StatusTodo
			}
			list.Tasks[i].LegacyCompleted = false // don't persist old field
		}
		if list.Tasks[i].Priority == "" {
			list.Tasks[i].Priority = domain.PriorityMed
		}
	}
	return &list, nil
}

// Save writes the task list to the JSON file using an atomic write
// (write to temp file, then rename). Creates a backup of the previous file
// as tasks.json.bak before overwriting.
func (s *Store) Save(list *domain.TaskList) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}

	// Backup existing file before overwrite
	if _, err := os.Stat(s.path); err == nil {
		backupPath := s.path + ".bak"
		if err := copyFile(s.path, backupPath); err != nil {
			// log but don't fail the save
			_ = err
		}
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename store: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}