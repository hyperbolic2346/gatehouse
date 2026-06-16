package api

import (
	"encoding/json"
	"log"
	"log/slog"
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
	ID            int64    `json:"id"`
	Username      string   `json:"username"`
	Role          string   `json:"role"`
	WilsonGate    bool     `json:"wilson_gate"`
	BrigmanGate   bool     `json:"brigman_gate"`
	CreatedAt     string   `json:"created_at"`
	Cameras       []string `json:"cameras"`
	HiddenCameras []string `json:"hidden_cameras"`
}

func toUserResponse(u *db.User) userResponse {
	cameras := u.Cameras
	if cameras == nil {
		cameras = []string{}
	}
	hidden := u.HiddenCameras
	if hidden == nil {
		hidden = []string{}
	}
	return userResponse{
		ID:            u.ID,
		Username:      u.Username,
		Role:          u.Role,
		WilsonGate:    u.WilsonGate,
		BrigmanGate:   u.BrigmanGate,
		CreatedAt:     u.CreatedAt,
		Cameras:       cameras,
		HiddenCameras: hidden,
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

// updateVisibilityRequest is the body for PUT /api/me/cameras. Hidden lists the
// cameras the user wants to hide from their own view.
type updateVisibilityRequest struct {
	Hidden []string `json:"hidden"`
}

// UpdateCameraVisibility handles PUT /api/me/cameras. It lets the current user
// hide or show cameras within the set they are permitted to view. Cameras in
// the request that the user is not permitted to view are rejected.
func (h *AuthHandler) UpdateCameraVisibility(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req updateVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	allowed := make(map[string]bool, len(user.Cameras))
	for _, c := range user.Cameras {
		allowed[c] = true
	}
	for _, c := range req.Hidden {
		if !allowed[c] {
			writeJSONError(w, "cannot hide a camera you are not permitted to view: "+c, http.StatusBadRequest)
			return
		}
	}

	if err := h.DB.SetUserHiddenCameras(user.ID, req.Hidden); err != nil {
		slog.Error("failed to update camera visibility", "user_id", user.ID, "error", err)
		writeJSONError(w, "failed to update camera visibility", http.StatusInternalServerError)
		return
	}

	updated, err := h.DB.GetUserByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user after visibility update", "user_id", user.ID, "error", err)
		writeJSONError(w, "failed to update camera visibility", http.StatusInternalServerError)
		return
	}
	writeJSON(w, toUserResponse(updated), http.StatusOK)
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
