// Package store provides MongoDB persistence for the task list.
package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"taskmanager/internal/domain"
)

const (
	mongoDocID             = "tasklist"
	autosaveDebounceMongo  = 300 * time.Millisecond
)

func docIDForUser(userID string) string {
	if userID == "" {
		return mongoDocID
	}
	return mongoDocID + "_" + userID
}

// mongoDoc is the document stored in MongoDB (tasklist + _id).
type mongoDoc struct {
	ID     string         `bson:"_id"`
	Tasks  []domain.Task   `bson:"tasks"`
	NextID int            `bson:"next_id"`
}

// Ensure MongoStore implements TaskStore.
var _ TaskStore = (*MongoStore)(nil)

type saveReq struct {
	userID string
	list   *domain.TaskList
}
type flushReq struct {
	userID string
	done   chan error
}

// MongoStore handles reading and writing the task list to MongoDB with
// debounced autosave. Use LoadForUser/SaveForUser/RequestSaveForUser/FlushSaveForUser
// for per-user isolation; document _id is "tasklist" for userID "" else "tasklist_<userID>".
// Coll is exposed so UserStore can use the same database (coll.Database().Collection("users")).
type MongoStore struct {
	client     *mongo.Client
	Coll       *mongo.Collection
	mu         sync.RWMutex
	saveCh     chan saveReq
	flushCh    chan flushReq
	done       chan struct{}
	wg         sync.WaitGroup
	closeOnce  sync.Once
}

// NewMongo creates a MongoStore that uses the given URI, database name, and collection name.
// It connects immediately. Call Close() when done.
func NewMongo(uri, dbName, collName string) (*MongoStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	coll := client.Database(dbName).Collection(collName)
	s := &MongoStore{
		client:  client,
		Coll:    coll,
		saveCh:  make(chan saveReq, 8),
		flushCh: make(chan flushReq, 8),
		done:    make(chan struct{}),
	}
	s.wg.Add(1)
	go s.autosaveWorker()
	return s, nil
}

func (s *MongoStore) autosaveWorker() {
	defer s.wg.Done()
	pending := make(map[string]*domain.TaskList)
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Reset(0)

	for {
		select {
		case req := <-s.saveCh:
			pending[req.userID] = req.list
			timer.Reset(autosaveDebounceMongo)
		case <-timer.C:
			for uid, list := range pending {
				if err := s.SaveForUser(uid, list); err != nil {
					slog.Error("MongoDB save failed", "user", uid, "err", err)
				}
			}
			pending = make(map[string]*domain.TaskList)
		case req := <-s.flushCh:
			var saveErr error
			if list := pending[req.userID]; list != nil {
				saveErr = s.SaveForUser(req.userID, list)
				if saveErr != nil {
					slog.Error("MongoDB save failed", "user", req.userID, "err", saveErr)
				}
				delete(pending, req.userID)
			}
			req.done <- saveErr
		case <-s.done:
			for uid, list := range pending {
				if err := s.SaveForUser(uid, list); err != nil {
					slog.Error("MongoDB save failed", "user", uid, "err", err)
				}
			}
			return
		}
	}
}

// Close disconnects the client and stops the autosave worker.
func (s *MongoStore) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
		_ = s.client.Disconnect(context.Background())
	})
	return nil
}

// LoadForUser reads the task list for the given user. userID "" uses document "tasklist" (default).
func (s *MongoStore) LoadForUser(userID string) (*domain.TaskList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	docID := docIDForUser(userID)
	var doc mongoDoc
	err := s.Coll.FindOne(ctx, bson.M{"_id": docID}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.NewTaskList(), nil
		}
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	list := &domain.TaskList{
		Tasks:  doc.Tasks,
		NextID: doc.NextID,
	}
	if list.Tasks == nil {
		list.Tasks = make([]domain.Task, 0)
	}
	if list.NextID < 1 {
		list.NextID = 1
	}

	for i := range list.Tasks {
		if list.Tasks[i].Status == "" {
			if list.Tasks[i].LegacyCompleted || list.Tasks[i].CompletedAt != nil {
				list.Tasks[i].Status = domain.StatusDone
			} else {
				list.Tasks[i].Status = domain.StatusTodo
			}
			list.Tasks[i].LegacyCompleted = false
		}
		if list.Tasks[i].Priority == "" {
			list.Tasks[i].Priority = domain.PriorityMed
		}
	}
	return list, nil
}

// SaveForUser writes the task list for the given user.
func (s *MongoStore) SaveForUser(userID string, list *domain.TaskList) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	docID := docIDForUser(userID)
	doc := mongoDoc{
		ID:     docID,
		Tasks:  list.Tasks,
		NextID: list.NextID,
	}
	if doc.Tasks == nil {
		doc.Tasks = make([]domain.Task, 0)
	}

	opts := options.Replace().SetUpsert(true)
	_, err := s.Coll.ReplaceOne(ctx, bson.M{"_id": docID}, doc, opts)
	if err != nil {
		return fmt.Errorf("save tasks: %w", err)
	}
	return nil
}

// RequestSaveForUser schedules a save for the user after the debounce period.
func (s *MongoStore) RequestSaveForUser(userID string, list *domain.TaskList) {
	select {
	case s.saveCh <- saveReq{userID: userID, list: list}:
	default:
		<-s.saveCh
		s.saveCh <- saveReq{userID: userID, list: list}
	}
}

// FlushSaveForUser blocks until any pending save for the user has completed.
func (s *MongoStore) FlushSaveForUser(userID string) error {
	ch := make(chan error, 1)
	select {
	case s.flushCh <- flushReq{userID: userID, done: ch}:
		return <-ch
	case <-s.done:
		return nil
	}
}

// Load reads the task list for the default user (document "tasklist"). Implements TaskStore.
func (s *MongoStore) Load() (*domain.TaskList, error) {
	return s.LoadForUser("")
}

// Save writes the task list for the default user. Implements TaskStore.
func (s *MongoStore) Save(list *domain.TaskList) error {
	return s.SaveForUser("", list)
}

// RequestSave schedules a save for the default user. Implements TaskStore.
func (s *MongoStore) RequestSave(list *domain.TaskList) {
	s.RequestSaveForUser("", list)
}

// FlushSave blocks until the default user's pending save has completed. Implements TaskStore.
func (s *MongoStore) FlushSave() error {
	return s.FlushSaveForUser("")
}
