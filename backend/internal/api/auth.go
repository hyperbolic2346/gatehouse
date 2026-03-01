package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/auth"
	"github.com/hyperbolic2346/gatehouse/internal/db"
)

// AuthHandler implements the authentication HTTP endpoints.
type AuthHandler struct {
	DB        *db.DB
	JWTSecret string
}

// loginRequest is the expected JSON body for POST /api/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// userResponse is the JSON representation of a user returned to clients.
type userResponse struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	WilsonGate  bool   `json:"wilson_gate"`
	BrigmanGate bool   `json:"brigman_gate"`
	CreatedAt   string `json:"created_at"`
}

func toUserResponse(u *db.User) userResponse {
	return userResponse{
		ID:          u.ID,
		Username:    u.Username,
		Role:        u.Role,
		WilsonGate:  u.WilsonGate,
		BrigmanGate: u.BrigmanGate,
		CreatedAt:   u.CreatedAt,
	}
}

// Login handles POST /api/login. It validates the username and password,
// issues a JWT stored in an httpOnly cookie named "token", and responds
// with the authenticated user's info.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"username and password are required"}`, http.StatusBadRequest)
		return
	}

	user, err := h.DB.GetUserByUsername(req.Username)
	if err != nil {
		log.Printf("login: user lookup failed for %q: %v", req.Username, err)
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	token, err := auth.GenerateToken(user, h.JWTSecret)
	if err != nil {
		log.Printf("login: token generation failed: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toUserResponse(user))
}

// Me handles GET /api/me. It returns the current authenticated user's info
// from the request context.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toUserResponse(user))
}

// Logout handles POST /api/logout. It clears the token cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
