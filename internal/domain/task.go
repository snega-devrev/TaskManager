// Package domain defines the core task entity and business rules.
package domain

import "time"

// Priority levels for a task.
const (
	PriorityLow  = "low"
	PriorityMed  = "med"
	PriorityHigh = "high"
)

// Status values for a task.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in-progress"
	StatusDone       = "done"
)

// Task represents a single task in the task manager.
type Task struct {
	ID          int        `json:"id" bson:"id"`
	Title       string     `json:"title" bson:"title"`
	Description string     `json:"description,omitempty" bson:"description,omitempty"`
	Priority    string     `json:"priority,omitempty" bson:"priority,omitempty"` // low, med, high
	DueDate     *time.Time `json:"due_date,omitempty" bson:"due_date,omitempty"`
	Tags        []string   `json:"tags,omitempty" bson:"tags,omitempty"`
	Status      string     `json:"status" bson:"status"` // todo, in-progress, done
	CreatedAt   time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" bson:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" bson:"completed_at,omitempty"`

	// LegacyCompleted is only for reading old JSON ("completed": true); Status is authoritative.
	LegacyCompleted bool `json:"completed,omitempty" bson:"completed,omitempty"`
}

// Completed returns true if the task is in done status.
func (t *Task) Completed() bool {
	return t.Status == StatusDone
}

// TaskList holds a collection of tasks with the next ID for new tasks.
type TaskList struct {
	Tasks  []Task `json:"tasks" bson:"tasks"` // slice of Task objects
	NextID int    `json:"next_id" bson:"next_id"`
}

// NewTaskList returns an empty TaskList ready for use.
func NewTaskList() *TaskList {
	return &TaskList{
		Tasks:  make([]Task, 0),
		NextID: 1,
	}
}

// ValidPriority returns true if p is a valid priority.
func ValidPriority(p string) bool {
	return p == PriorityLow || p == PriorityMed || p == PriorityHigh
}

// ValidStatus returns true if s is a valid status.
func ValidStatus(s string) bool {
	return s == StatusTodo || s == StatusInProgress || s == StatusDone
}
