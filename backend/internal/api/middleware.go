package api

import (
	"context"
	"log"
	"net/http"

	"github.com/hyperbolic2346/gatehouse/internal/auth"
	"github.com/hyperbolic2346/gatehouse/internal/db"
)

type contextKey string

const userContextKey contextKey = "user"

// AuthMiddleware returns middleware that validates the JWT from the "token"
// cookie, looks up the user in the database, and stores it in the request
// context. If the token is missing or invalid the request is rejected with 401.
func AuthMiddleware(jwtSecret string, database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("token")
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			claims, err := auth.ValidateToken(cookie.Value, jwtSecret)
			if err != nil {
				log.Printf("invalid token: %v", err)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			user, err := database.GetUserByID(claims.UserID)
			if err != nil {
				log.Printf("user lookup failed for id %d: %v", claims.UserID, err)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext retrieves the authenticated user stored by AuthMiddleware.
// Returns nil if no user is present in the context.
func UserFromContext(ctx context.Context) *db.User {
	u, _ := ctx.Value(userContextKey).(*db.User)
	return u
}

// AdminOnly is middleware that rejects requests from non-admin users with 403.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || user.Role != "admin" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
