// Package store provides user persistence for login/register.
package store

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const usersCollName = "users"

// UserStore stores and retrieves users (username + password hash) for login.
type UserStore interface {
	CreateUser(ctx context.Context, username, passwordHash string) error
	GetUser(ctx context.Context, username string) (passwordHash string, err error)
}

type mongoUserDoc struct {
	ID           string `bson:"_id"` // username
	PasswordHash string `bson:"password_hash"`
}

// MongoUserStore stores users in MongoDB collection "users".
type MongoUserStore struct {
	coll *mongo.Collection
}

// NewMongoUserStore creates a user store using the same database as the given MongoStore (collection "users").
func NewMongoUserStore(m *MongoStore) *MongoUserStore {
	coll := m.Coll.Database().Collection(usersCollName)
	return &MongoUserStore{coll: coll}
}

// CreateUser inserts a user. Returns error if username already exists.
func (s *MongoUserStore) CreateUser(ctx context.Context, username, passwordHash string) error {
	if username == "" {
		return fmt.Errorf("username required")
	}
	doc := mongoUserDoc{ID: username, PasswordHash: passwordHash}
	_, err := s.coll.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("username already exists")
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUser returns the password hash for the username, or error if not found.
func (s *MongoUserStore) GetUser(ctx context.Context, username string) (passwordHash string, err error) {
	var doc mongoUserDoc
	err = s.coll.FindOne(ctx, bson.M{"_id": username}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", fmt.Errorf("invalid username or password")
		}
		return "", fmt.Errorf("get user: %w", err)
	}
	return doc.PasswordHash, nil
}
