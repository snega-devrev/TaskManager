// Package grpc implements the gRPC TaskService server using the app layer.
package grpc

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"taskmanager/api/proto"
	"taskmanager/internal/app"
	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

// Metadata key for user isolation. Client must set this (e.g. "user-id: alice") so each user's tasks are stored separately.
const UserIDMetadataKey = "user-id"

// Server implements proto.TaskServiceServer and proto.AuthServiceServer.
type Server struct {
	proto.UnimplementedTaskServiceServer
	proto.UnimplementedAuthServiceServer
	mongo     *store.MongoStore
	userStore *store.MongoUserStore
	jwtSecret string
}

// NewServer returns a gRPC server. If jwtSecret is set, use Register/Login to get a token; then use that token for TaskService calls.
func NewServer(mongo *store.MongoStore, jwtSecret string) *Server {
	return &Server{
		mongo:     mongo,
		userStore: store.NewMongoUserStore(mongo),
		jwtSecret: jwtSecret,
	}
}

// appForUser returns an App scoped to the user ID from ctx (JWT when jwtSecret set, else metadata), or an error if missing.
func (s *Server) appForUser(ctx context.Context) (*app.App, error) {
	userID := s.userIDForRequest(ctx)
	if userID == "" {
		if s.jwtSecret != "" {
			return nil, status.Error(codes.Unauthenticated, "login required: use the Login RPC to get a token, then set Authorization: Bearer <token>")
		}
		return nil, status.Error(codes.Unauthenticated, "missing metadata: set \"user-id\" to isolate your tasks")
	}
	scoped := store.NewUserScopedStore(s.mongo, userID)
	return app.New(scoped), nil
}

func userIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get(UserIDMetadataKey)
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}

// userIDForRequest returns the user ID for this request (JWT sub when jwtSecret set, else metadata).
func (s *Server) userIDForRequest(ctx context.Context) string {
	userID := UserIDFromContext(ctx)
	if userID == "" && s.jwtSecret == "" {
		userID = userIDFromMetadata(ctx)
	}
	return userID
}

func taskToProto(t *domain.Task) *proto.Task {
	out := &proto.Task{
		Id:          int32(t.ID),
		Title:       t.Title,
		Description: t.Description,
		Priority:    t.Priority,
		Tags:        t.Tags,
		Status:      t.Status,
		CreatedAt:   t.CreatedAt.Unix(),
		UpdatedAt:   t.UpdatedAt.Unix(),
	}
	if t.DueDate != nil {
		out.DueDate = t.DueDate.Unix()
	}
	if t.CompletedAt != nil {
		out.CompletedAt = t.CompletedAt.Unix()
	}
	return out
}

func (s *Server) AddTask(ctx context.Context, req *proto.AddTaskRequest) (*proto.AddTaskResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	var dueDate *time.Time
	if req.GetDueDate() != 0 {
		t := time.Unix(req.GetDueDate(), 0).UTC()
		dueDate = &t
	}
	task, err := a.AddTask(req.GetTitle(), req.GetDescription(), req.GetPriority(), dueDate, req.GetTags())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := a.FlushSave(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.AddTaskResponse{Task: taskToProto(task)}, nil
}

func (s *Server) GetTask(ctx context.Context, req *proto.GetTaskRequest) (*proto.GetTaskResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	task, err := a.GetTask(int(req.GetId()))
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if task == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	return &proto.GetTaskResponse{Task: taskToProto(task)}, nil
}

func (s *Server) ListTasks(ctx context.Context, req *proto.ListTasksRequest) (*proto.ListTasksResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	filter := app.FilterAll
	switch req.GetFilter() {
	case proto.ListFilter_LIST_FILTER_DONE:
		filter = app.FilterDone
	case proto.ListFilter_LIST_FILTER_PENDING:
		filter = app.FilterPending
	}
	sortBy := app.SortByCreatedAt
	switch req.GetSortBy() {
	case proto.SortBy_SORT_BY_DUE:
		sortBy = app.SortByDueDate
	case proto.SortBy_SORT_BY_PRIORITY:
		sortBy = app.SortByPriority
	}
	tasks, err := a.ListTasks(filter, sortBy, req.GetDueToday(), req.GetOverdue())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*proto.Task, len(tasks))
	for i := range tasks {
		out[i] = taskToProto(&tasks[i])
	}
	return &proto.ListTasksResponse{Tasks: out, UserId: s.userIDForRequest(ctx)}, nil
}

func (s *Server) UpdateStatus(ctx context.Context, req *proto.UpdateStatusRequest) (*proto.UpdateStatusResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	id := int(req.GetId())
	st := req.GetStatus()
	switch st {
	case domain.StatusDone:
		if err := a.Done(id); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	case domain.StatusTodo, domain.StatusInProgress:
		opts := app.EditOpts{Status: &st}
		if err := a.Edit(id, opts); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid status: use todo, in-progress, or done")
	}
	if err := a.FlushSave(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.UpdateStatusResponse{}, nil
}

func (s *Server) DeleteTask(ctx context.Context, req *proto.DeleteTaskRequest) (*proto.DeleteTaskResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Delete(int(req.GetId())); err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err := a.FlushSave(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.DeleteTaskResponse{}, nil
}

func (s *Server) EditTask(ctx context.Context, req *proto.EditTaskRequest) (*proto.EditTaskResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	opts := app.EditOpts{}
	if req.Title != nil {
		opts.Title = req.Title
	}
	if req.Description != nil {
		opts.Description = req.Description
	}
	if req.Priority != nil {
		opts.Priority = req.Priority
	}
	if req.DueDate != nil {
		if *req.DueDate == 0 {
			opts.ClearDueDate = true
		} else {
			t := time.Unix(*req.DueDate, 0).UTC()
			opts.DueDate = &t
		}
	}
	if req.Status != nil {
		opts.Status = req.Status
	}
	if err := a.Edit(int(req.GetId()), opts); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := a.FlushSave(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.EditTaskResponse{}, nil
}

func (s *Server) SearchTasks(ctx context.Context, req *proto.SearchTasksRequest) (*proto.SearchTasksResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := a.Search(req.GetKeyword())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*proto.Task, len(tasks))
	for i := range tasks {
		out[i] = taskToProto(&tasks[i])
	}
	return &proto.SearchTasksResponse{Tasks: out}, nil
}

func (s *Server) TagAdd(ctx context.Context, req *proto.TagAddRequest) (*proto.TagAddResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	added, err := a.TagAdd(int(req.GetId()), req.GetTag())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := a.FlushSave(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.TagAddResponse{Added: added}, nil
}

func (s *Server) Reset(ctx context.Context, _ *proto.ResetRequest) (*proto.ResetResponse, error) {
	a, err := s.appForUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Reset(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := a.FlushSave(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.ResetResponse{}, nil
}

const tokenExpHours = 24 * 7 * time.Hour // 7 days

func (s *Server) Register(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	username := strings.TrimSpace(req.GetUsername())
	password := req.GetPassword()
	if username == "" {
		return nil, status.Error(codes.InvalidArgument, "username required")
	}
	if len(password) < 6 {
		return nil, status.Error(codes.InvalidArgument, "password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := s.userStore.CreateUser(ctx, username, string(hash)); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil, status.Error(codes.AlreadyExists, "username already exists")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	if s.jwtSecret == "" {
		return nil, status.Error(codes.Internal, "server not configured with JWT secret")
	}
	token, err := MintToken(s.jwtSecret, username, tokenExpHours)
	if err != nil {
		return nil, err
	}
	return &proto.RegisterResponse{Token: token}, nil
}

func (s *Server) Login(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	username := strings.TrimSpace(req.GetUsername())
	password := req.GetPassword()
	if username == "" || password == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password required")
	}
	hash, err := s.userStore.GetUser(ctx, username)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}
	if s.jwtSecret == "" {
		return nil, status.Error(codes.Internal, "server not configured with JWT secret")
	}
	token, err := MintToken(s.jwtSecret, username, tokenExpHours)
	if err != nil {
		return nil, err
	}
	return &proto.LoginResponse{Token: token}, nil
}
