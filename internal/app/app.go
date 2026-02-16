// Package app provides business logic for the task manager (no I/O).
package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

// ListFilter is the filter for listing tasks.
type ListFilter string

const (
	FilterAll     ListFilter = "all"
	FilterDone    ListFilter = "done"
	FilterPending ListFilter = "pending"
)

// SortBy is the sort order for listing.
type SortBy string

const (
	SortByDueDate   SortBy = "due"
	SortByPriority  SortBy = "priority"
	SortByCreatedAt SortBy = "created"
)

// App holds the store and provides task operations.
type App struct {
	store *store.Store
}

// New creates an App that uses the given store.
func New(st *store.Store) *App {
	return &App{store: st}
}

func (a *App) load() (*domain.TaskList, error) {
	list, err := a.store.Load()
	if err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}
	return list, nil
}

// AddTask adds a new task and saves. Uses autosave (RequestSave).
func (a *App) AddTask(title, description, priority string, dueDate *time.Time, tags []string) (*domain.Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("task title cannot be empty")
	}
	if priority != "" && !domain.ValidPriority(priority) {
		return nil, fmt.Errorf("invalid priority: use low, med, or high")
	}
	list, err := a.load()
	if err != nil {
		return nil, err
	}
	titleLower := strings.ToLower(title)
	for _, t := range list.Tasks {
		if strings.ToLower(strings.TrimSpace(t.Title)) == titleLower {
			return nil, fmt.Errorf("task with title %q already exists (id %d)", t.Title, t.ID)
		}
	}
	now := time.Now().UTC()
	if priority == "" {
		priority = domain.PriorityMed
	}
	task := domain.Task{
		ID:        list.NextID,
		Title:     title,
		Description: description,
		Priority:  priority,
		DueDate:   dueDate,
		Tags:      tags,
		Status:    domain.StatusTodo,
		CreatedAt: now,
		UpdatedAt: now,
	}
	list.Tasks = append(list.Tasks, task)
	list.NextID++
	a.store.RequestSave(list)
	return &task, nil
}

// ListTasks returns tasks filtered and sorted. Does not mutate.
func (a *App) ListTasks(filter ListFilter, sortBy SortBy, dueToday, overdue bool) ([]domain.Task, error) {
	list, err := a.load()
	if err != nil {
		return nil, err
	}
	tasks := list.Tasks

	// Filter
	switch filter {
	case FilterDone:
		tasks = filterTasks(tasks, func(t domain.Task) bool { return t.Status == domain.StatusDone })
	case FilterPending:
		tasks = filterTasks(tasks, func(t domain.Task) bool { return t.Status != domain.StatusDone })
	}
	if dueToday {
		tasks = filterTasks(tasks, dueTodayFilter)
	}
	if overdue {
		tasks = filterTasks(tasks, overdueFilter)
	}

	// Sort
	switch sortBy {
	case SortByDueDate:
		sort.Slice(tasks, func(i, j int) bool {
			return taskDueBefore(tasks[i], tasks[j])
		})
	case SortByPriority:
		sort.Slice(tasks, func(i, j int) bool {
			return priorityOrder(tasks[i].Priority) > priorityOrder(tasks[j].Priority)
		})
	case SortByCreatedAt:
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		})
	}
	return tasks, nil
}

func filterTasks(tasks []domain.Task, keep func(domain.Task) bool) []domain.Task {
	out := make([]domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if keep(t) {
			out = append(out, t)
		}
	}
	return out
}

func dueTodayFilter(t domain.Task) bool {
	if t.DueDate == nil {
		return false
	}
	now := time.Now()
	y, m, d := now.Date()
	ty, tm, td := t.DueDate.Date()
	return y == ty && m == tm && d == td
}

func overdueFilter(t domain.Task) bool {
	if t.DueDate == nil || t.Status == domain.StatusDone {
		return false
	}
	return t.DueDate.Before(time.Now())
}

func taskDueBefore(a, b domain.Task) bool {
	if a.DueDate == nil && b.DueDate == nil {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	if a.DueDate == nil {
		return false
	}
	if b.DueDate == nil {
		return true
	}
	return a.DueDate.Before(*b.DueDate)
}

func priorityOrder(p string) int {
	switch p {
	case domain.PriorityHigh:
		return 3
	case domain.PriorityMed:
		return 2
	case domain.PriorityLow:
		return 1
	}
	return 0
}

// Done marks a task as done and saves.
func (a *App) Done(id int) error {
	list, err := a.load()
	if err != nil {
		return err
	}
	var found bool
	for i := range list.Tasks {
		if list.Tasks[i].ID == id {
			if list.Tasks[i].Status == domain.StatusDone {
				return fmt.Errorf("task #%d is already done", id)
			}
			now := time.Now().UTC()
			list.Tasks[i].Status = domain.StatusDone
			list.Tasks[i].UpdatedAt = now
			list.Tasks[i].CompletedAt = &now
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task #%d not found", id)
	}
	a.store.RequestSave(list)
	return nil
}

// Delete removes a task by ID and saves.
func (a *App) Delete(id int) error {
	list, err := a.load()
	if err != nil {
		return err
	}
	var found bool
	filtered := list.Tasks[:0]
	for _, t := range list.Tasks {
		if t.ID != id {
			filtered = append(filtered, t)
		} else {
			found = true
		}
	}
	list.Tasks = filtered
	if !found {
		return fmt.Errorf("task #%d not found", id)
	}
	a.store.RequestSave(list)
	return nil
}

// EditOpts holds optional fields for editing a task. Nil pointer = don't change.
type EditOpts struct {
	Title       *string
	Description *string
	Priority    *string
	DueDate     *time.Time
	Status      *string
}

// Edit updates a task by ID. Only fields set in opts are changed.
func (a *App) Edit(id int, opts EditOpts) error {
	list, err := a.load()
	if err != nil {
		return err
	}
	if opts.Priority != nil && !domain.ValidPriority(*opts.Priority) {
		return fmt.Errorf("invalid priority: use low, med, or high")
	}
	if opts.Status != nil && !domain.ValidStatus(*opts.Status) {
		return fmt.Errorf("invalid status: use todo, in-progress, or done")
	}
	var found bool
	now := time.Now().UTC()
	for i := range list.Tasks {
		if list.Tasks[i].ID == id {
			if opts.Title != nil {
				list.Tasks[i].Title = strings.TrimSpace(*opts.Title)
			}
			if opts.Description != nil {
				list.Tasks[i].Description = *opts.Description
			}
			if opts.Priority != nil {
				list.Tasks[i].Priority = *opts.Priority
			}
			if opts.DueDate != nil {
				list.Tasks[i].DueDate = opts.DueDate
			}
			if opts.Status != nil {
				list.Tasks[i].Status = *opts.Status
				if *opts.Status == domain.StatusDone {
					if list.Tasks[i].CompletedAt == nil {
						list.Tasks[i].CompletedAt = &now
					}
				} else {
					// Revert from done: clear CompletedAt when moving to todo or in-progress
					list.Tasks[i].CompletedAt = nil
				}
			}
			list.Tasks[i].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task #%d not found", id)
	}
	a.store.RequestSave(list)
	return nil
}

// Search returns tasks whose title or description contains the keyword (case-insensitive).
func (a *App) Search(keyword string) ([]domain.Task, error) {
	keyword = strings.TrimSpace(strings.ToLower(keyword))
	if keyword == "" {
		return nil, fmt.Errorf("search keyword cannot be empty")
	}
	list, err := a.load()
	if err != nil {
		return nil, err
	}
	var out []domain.Task
	for _, t := range list.Tasks {
		if strings.Contains(strings.ToLower(t.Title), keyword) ||
			strings.Contains(strings.ToLower(t.Description), keyword) {
			out = append(out, t)
		}
	}
	return out, nil
}

// TagAdd adds a tag to a task if not already present.
func (a *App) TagAdd(id int, tag string) error {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}
	list, err := a.load()
	if err != nil {
		return err
	}
	var found bool
	for i := range list.Tasks {
		if list.Tasks[i].ID == id {
			for _, t := range list.Tasks[i].Tags {
				if t == tag {
					return nil // already present
				}
			}
			list.Tasks[i].Tags = append(list.Tasks[i].Tags, tag)
			list.Tasks[i].UpdatedAt = time.Now().UTC()
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task #%d not found", id)
	}
	a.store.RequestSave(list)
	return nil
}

// ClearDone removes all tasks with status done and returns the count.
func (a *App) ClearDone() (int, error) {
	list, err := a.load()
	if err != nil {
		return 0, err
	}
	var count int
	filtered := list.Tasks[:0]
	for _, t := range list.Tasks {
		if t.Status != domain.StatusDone {
			filtered = append(filtered, t)
		} else {
			count++
		}
	}
	list.Tasks = filtered
	if count > 0 {
		a.store.RequestSave(list)
	}
	return count, nil
}

// GetTask returns a single task by ID, or nil if not found.
func (a *App) GetTask(id int) (*domain.Task, error) {
	list, err := a.load()
	if err != nil {
		return nil, err
	}
	for i := range list.Tasks {
		if list.Tasks[i].ID == id {
			return &list.Tasks[i], nil
		}
	}
	return nil, nil
}

// Reset clears all tasks and resets the list (next ID will be 1). Saves immediately.
func (a *App) Reset() error {
	return a.store.Save(domain.NewTaskList())
}

// FlushSave blocks until the autosave worker has written any pending data.
// Call before exit to ensure data is persisted.
func (a *App) FlushSave() error {
	return a.store.Close()
}
