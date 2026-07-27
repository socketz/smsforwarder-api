// Package auth handles password hashing and device-token authentication.
//
// Login flow: the user posts username+password once; the server issues a random
// long-lived device token, stores only its SHA-256 hash, and returns the plaintext
// token to the app a single time. The app sends it as `Authorization: Bearer <token>`
// on every request. Tokens are revocable per device without touching the password.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/shuttlelab/smsforwarder-api/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// NewToken returns a random opaque token (given to the client) and its hash
// (stored in the DB).
func NewToken() (token, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
)

// Middleware authenticates the Bearer token and injects the user + device id
// into the request context. Responds 401 on any failure.
func Middleware(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := bearer(r)
			if tok == "" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			u, deviceID, err := st.UserByDeviceToken(r.Context(), HashToken(tok))
			if err != nil {
				http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxUser, u)
			ctx = context.WithValue(ctx, ctxDevice, deviceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// UserFrom returns the authenticated user (only valid behind Middleware).
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxUser).(*store.User)
	return u
}

// DeviceFrom returns the authenticated device id (only valid behind Middleware).
func DeviceFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxDevice).(int64)
	return id
}
