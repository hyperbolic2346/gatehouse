package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hyperbolic2346/gatehouse/internal/auth"
	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/frigate"
)

// defaultNewUserCameras is the camera set granted to a new user when the
// create request does not specify one.
var defaultNewUserCameras = []string{"gate"}

// UsersHandler provides HTTP handlers for user management operations.
// All endpoints require admin privileges, which should be enforced by
// middleware wrapping these handlers.
type UsersHandler struct {
	DB      *db.DB
	Frigate *frigate.Client
}

// createUserRequest is the expected JSON body for creating a new user.
// Cameras is the set of cameras the user may view; when omitted it defaults to
// defaultNewUserCameras.
type createUserRequest struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Role        string   `json:"role"`
	WilsonGate  bool     `json:"wilson_gate"`
	BrigmanGate bool     `json:"brigman_gate"`
	Cameras     []string `json:"cameras"`
}

// updateUserRequest is the expected JSON body for updating an existing user.
// All fields are optional; only provided fields are updated.
type updateUserRequest struct {
	Username    *string   `json:"username"`
	Password    *string   `json:"password"`
	Role        *string   `json:"role"`
	WilsonGate  *bool     `json:"wilson_gate"`
	BrigmanGate *bool     `json:"brigman_gate"`
	Cameras     *[]string `json:"cameras"`
}

// ListCameras handles GET /api/cameras. It returns the catalog of cameras
// discovered from Frigate, which admins assign to users.
func (h *UsersHandler) ListCameras(w http.ResponseWriter, r *http.Request) {
	cameras, err := h.Frigate.GetCameras()
	if err != nil {
		slog.Error("failed to list cameras from frigate", "error", err)
		writeJSONError(w, "failed to list cameras", http.StatusBadGateway)
		return
	}
	if cameras == nil {
		cameras = []string{}
	}
	writeJSON(w, cameras, http.StatusOK)
}

// validateCameras returns an error response message if any requested camera is
// not in the Frigate catalog. If the catalog cannot be fetched, validation is
// skipped (and a warning logged) so user management does not depend on Frigate
// being reachable.
func (h *UsersHandler) validateCameras(cameras []string) (badCamera string, ok bool) {
	catalog, err := h.Frigate.GetCameras()
	if err != nil {
		slog.Warn("skipping camera validation; frigate catalog unavailable", "error", err)
		return "", true
	}
	valid := make(map[string]bool, len(catalog))
	for _, c := range catalog {
		valid[c] = true
	}
	for _, c := range cameras {
		if !valid[c] {
			return c, false
		}
	}
	return "", true
}

// List handles GET /api/users
// It returns all users in the system with password hashes excluded.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.DB.ListUsers()
	if err != nil {
		slog.Error("failed to list users", "error", err)
		writeJSONError(w, "failed to list users", http.StatusInternalServerError)
		return
	}

	resp := make([]userResponse, len(users))
	for i := range users {
		resp[i] = toUserResponse(&users[i])
	}

	writeJSON(w, resp, http.StatusOK)
}

// Create handles POST /api/users
// It creates a new user with the provided username, password, role, and gate
// permissions.
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		writeJSONError(w, "username is required", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		writeJSONError(w, "password is required", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role != "admin" && req.Role != "user" {
		writeJSONError(w, "role must be 'admin' or 'user'", http.StatusBadRequest)
		return
	}

	cameras := req.Cameras
	if cameras == nil {
		cameras = defaultNewUserCameras
	}
	if bad, ok := h.validateCameras(cameras); !ok {
		writeJSONError(w, "unknown camera: "+bad, http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		writeJSONError(w, "internal error", http.StatusInternalServerError)
		return
	}

	user := &db.User{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
		WilsonGate:   req.WilsonGate,
		BrigmanGate:  req.BrigmanGate,
		Cameras:      cameras,
	}

	if err := h.DB.CreateUser(user); err != nil {
		slog.Error("failed to create user", "error", err)
		writeJSONError(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	slog.Info("user created", "user_id", user.ID, "username", user.Username)
	writeJSON(w, toUserResponse(user), http.StatusCreated)
}

// Update handles PUT /api/users/{id}
// It updates an existing user. Only the fields provided in the request body
// are changed. If a new password is provided it is hashed before storage.
func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeJSONError(w, "missing user id", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSONError(w, "invalid user id", http.StatusBadRequest)
		return
	}

	existing, err := h.DB.GetUserByID(id)
	if err != nil {
		slog.Error("failed to get user for update", "user_id", id, "error", err)
		writeJSONError(w, "user not found", http.StatusNotFound)
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username != nil {
		if *req.Username == "" {
			writeJSONError(w, "username cannot be empty", http.StatusBadRequest)
			return
		}
		existing.Username = *req.Username
	}
	if req.Password != nil {
		if *req.Password == "" {
			writeJSONError(w, "password cannot be empty", http.StatusBadRequest)
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			slog.Error("failed to hash password", "error", err)
			writeJSONError(w, "internal error", http.StatusInternalServerError)
			return
		}
		existing.PasswordHash = hash
	}
	if req.Role != nil {
		if *req.Role != "admin" && *req.Role != "user" {
			writeJSONError(w, "role must be 'admin' or 'user'", http.StatusBadRequest)
			return
		}
		existing.Role = *req.Role
	}
	if req.WilsonGate != nil {
		existing.WilsonGate = *req.WilsonGate
	}
	if req.BrigmanGate != nil {
		existing.BrigmanGate = *req.BrigmanGate
	}
	if req.Cameras != nil {
		if bad, ok := h.validateCameras(*req.Cameras); !ok {
			writeJSONError(w, "unknown camera: "+bad, http.StatusBadRequest)
			return
		}
	}

	if err := h.DB.UpdateUser(existing); err != nil {
		slog.Error("failed to update user", "user_id", id, "error", err)
		writeJSONError(w, "failed to update user", http.StatusInternalServerError)
		return
	}

	if req.Cameras != nil {
		if err := h.DB.SetUserCameras(id, *req.Cameras); err != nil {
			slog.Error("failed to update user cameras", "user_id", id, "error", err)
			writeJSONError(w, "failed to update user", http.StatusInternalServerError)
			return
		}
	}

	// Reload so the response reflects stored camera grants and any hidden
	// cameras pruned because they are no longer permitted.
	updated, err := h.DB.GetUserByID(id)
	if err != nil {
		slog.Error("failed to reload user after update", "user_id", id, "error", err)
		writeJSONError(w, "failed to update user", http.StatusInternalServerError)
		return
	}

	slog.Info("user updated", "user_id", updated.ID, "username", updated.Username)
	writeJSON(w, toUserResponse(updated), http.StatusOK)
}

// Delete handles DELETE /api/users/{id}
// It removes a user from the system. A user cannot delete themselves.
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeJSONError(w, "missing user id", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSONError(w, "invalid user id", http.StatusBadRequest)
		return
	}

	// Prevent self-deletion.
	currentUser := UserFromContext(r.Context())
	if currentUser != nil && currentUser.ID == id {
		writeJSONError(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}

	// Verify the user exists before deleting.
	if _, err := h.DB.GetUserByID(id); err != nil {
		slog.Error("failed to get user for deletion", "user_id", id, "error", err)
		writeJSONError(w, "user not found", http.StatusNotFound)
		return
	}

	if err := h.DB.DeleteUser(id); err != nil {
		slog.Error("failed to delete user", "user_id", id, "error", err)
		writeJSONError(w, "failed to delete user", http.StatusInternalServerError)
		return
	}

	slog.Info("user deleted", "user_id", id)
	w.WriteHeader(http.StatusNoContent)
}
