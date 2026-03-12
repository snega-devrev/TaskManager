// Package store defines the persistence interface for the task manager.
package store

import "taskmanager/internal/domain"

// TaskStore is the interface for loading and saving the task list.
// Implementations may use a JSON file, MongoDB, or another backend.
type TaskStore interface {
	Load() (*domain.TaskList, error)
	Save(list *domain.TaskList) error
	RequestSave(list *domain.TaskList)
	// FlushSave blocks until any pending save (from RequestSave) has completed and returns its error, if any.
	FlushSave() error
	Close() error
}

// UserScopedStore wraps a MongoStore to scope all operations to one user (document tasklist_<userID>).
// Use for gRPC so each client's data is isolated. userID "" uses the default document "tasklist".
func NewUserScopedStore(m *MongoStore, userID string) TaskStore {
	return &userScopedStore{backend: m, userID: userID}
}

type userScopedStore struct {
	backend *MongoStore
	userID  string
}

func (u *userScopedStore) Load() (*domain.TaskList, error) {
	return u.backend.LoadForUser(u.userID)
}

func (u *userScopedStore) Save(list *domain.TaskList) error {
	return u.backend.SaveForUser(u.userID, list)
}

func (u *userScopedStore) RequestSave(list *domain.TaskList) {
	u.backend.RequestSaveForUser(u.userID, list)
}

func (u *userScopedStore) FlushSave() error {
	return u.backend.FlushSaveForUser(u.userID)
}

func (u *userScopedStore) Close() error {
	return nil
}
