// Package grpc provides JWT-based automatic user ID extraction for gRPC.
package grpc

import (
	"context"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MintToken creates an HS256 JWT with sub=userID. Used by Login/Register.
func MintToken(secret, userID string, expHours time.Duration) (string, error) {
	if secret == "" || userID == "" {
		return "", status.Error(codes.Internal, "missing secret or user id")
	}
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(expHours).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

type contextKey string

const userIDContextKey contextKey = "user_id"

// setUserIDInContext returns a context with the user ID stored. Used by the JWT interceptor.
func setUserIDInContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// UserIDFromContext returns the user ID from context (set by JWT interceptor or for tests).
func UserIDFromContext(ctx context.Context) string {
	if v := ctx.Value(userIDContextKey); v != nil {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

// Auth methods that do not require a JWT (used to obtain the token).
var unauthenticatedMethods = map[string]bool{
	"/taskmanager.AuthService/Register": true,
	"/taskmanager.AuthService/Login":    true,
}

// JWTUnaryInterceptor returns a gRPC unary interceptor that validates a Bearer JWT and sets the user ID
// in context from the "sub" claim. Register and Login are allowed without a token.
func JWTUnaryInterceptor(secret string) grpc.UnaryServerInterceptor {
	if secret == "" {
		return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return handler(ctx, req)
		}
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if unauthenticatedMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		userID, err := extractUserIDFromJWT(ctx, secret)
		if err != nil {
			return nil, err
		}
		if userID != "" {
			ctx = setUserIDInContext(ctx, userID)
		}
		return handler(ctx, req)
	}
}

func extractUserIDFromJWT(ctx context.Context, secret string) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", nil
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", nil
	}
	tokenStr := strings.TrimSpace(vals[0])
	const prefix = "Bearer "
	if !strings.HasPrefix(tokenStr, prefix) {
		return "", nil
	}
	tokenStr = strings.TrimSpace(tokenStr[len(prefix):])
	if tokenStr == "" {
		return "", nil
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid token signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return "", status.Error(codes.Unauthenticated, "invalid or expired token: "+err.Error())
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", status.Error(codes.Unauthenticated, "invalid token")
	}
	sub, _ := claims["sub"].(string)
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return "", status.Error(codes.Unauthenticated, "token missing \"sub\" claim")
	}
	return sub, nil
}
