// mint-token generates a JWT with "sub" set to the given user, for use with the task manager gRPC server.
// Usage:
//
//	go run ./cmd/mint-token -user alice -secret "your-jwt-secret"
//
// Then call gRPC with: Authorization: Bearer <printed-token>
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	user := flag.String("user", "", "user id (sets JWT 'sub' claim); e.g. alice, bob")
	secret := flag.String("secret", os.Getenv("JWT_SECRET"), "HMAC secret (same as server -jwt-secret)")
	expHours := flag.Float64("exp", 24, "token expiry in hours")
	flag.Parse()

	if *user == "" {
		fmt.Fprintln(os.Stderr, "usage: mint-token -user <user-id> -secret <jwt-secret>")
		fmt.Fprintln(os.Stderr, "  -user is required (e.g. alice, bob); each user gets isolated tasks")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if *secret == "" {
		fmt.Fprintln(os.Stderr, "error: -secret or JWT_SECRET required")
		os.Exit(1)
	}

	claims := jwt.MapClaims{
		"sub": *user,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Duration(*expHours * float64(time.Hour))).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(*secret))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(signed)
}
