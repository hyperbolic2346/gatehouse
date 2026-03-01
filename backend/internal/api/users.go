package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hyperbolic2346/gatehouse/internal/auth"
	"github.com/hyperbolic2346/gatehouse/internal/db"
)

// UsersHandler provides HTTP handlers for user management operations.
// All endpoints require admin privileges, which should be enforced by
// middleware wrapping these handlers.
type UsersHandler struct {
	DB *db.DB
}

// createUserRequest is the expected JSON body for creating a new user.
type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	WilsonGate  bool   `json:"wilson_gate"`
	BrigmanGate bool   `json:"brigman_gate"`
}

// updateUserRequest is the expected JSON body for updating an existing user.
// All fields are optional; only provided fields are updated.
type updateUserRequest struct {
	Username    *string `json:"username"`
	Password    *string `json:"password"`
	Role        *string `json:"role"`
	WilsonGate  *bool   `json:"wilson_gate"`
	BrigmanGate *bool   `json:"brigman_gate"`
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

	if err := h.DB.UpdateUser(existing); err != nil {
		slog.Error("failed to update user", "user_id", id, "error", err)
		writeJSONError(w, "failed to update user", http.StatusInternalServerError)
		return
	}

	slog.Info("user updated", "user_id", existing.ID, "username", existing.Username)
	writeJSON(w, toUserResponse(existing), http.StatusOK)
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
