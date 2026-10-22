package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

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
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	Role         string   `json:"role"`
	WilsonGate   bool     `json:"wilson_gate"`
	BrigmanGate  bool     `json:"brigman_gate"`
	Cameras      []string `json:"cameras"`
	DisplayName  string   `json:"display_name"`
	HoldPriority int      `json:"hold_priority"`
}

// updateUserRequest is the expected JSON body for updating an existing user.
// All fields are optional; only provided fields are updated.
type updateUserRequest struct {
	Username     *string   `json:"username"`
	Password     *string   `json:"password"`
	Role         *string   `json:"role"`
	WilsonGate   *bool     `json:"wilson_gate"`
	BrigmanGate  *bool     `json:"brigman_gate"`
	Cameras      *[]string `json:"cameras"`
	DisplayName  *string   `json:"display_name"`
	HoldPriority *int      `json:"hold_priority"`
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

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeJSONError(w, "username is required", http.StatusBadRequest)
		return
	}
	if taken, err := h.DB.GetUserByUsername(req.Username); err == nil {
		writeJSONError(w, "username already taken by "+taken.Username, http.StatusConflict)
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
		DisplayName:  strings.TrimSpace(req.DisplayName),
		HoldPriority: req.HoldPriority,
	}

	if err := h.DB.CreateUser(user); err != nil {
		slog.Error("failed to create user", "error", err)
		writeJSONError(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	slog.Info("user created", "user_id", user.ID, "username", user.Username)
	h.logAdmin(r, db.ActionUserCreated, user.Username)
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
		name := strings.TrimSpace(*req.Username)
		if name == "" {
			writeJSONError(w, "username cannot be empty", http.StatusBadRequest)
			return
		}
		if taken, err := h.DB.GetUserByUsername(name); err == nil && taken.ID != existing.ID {
			writeJSONError(w, "username already taken by "+taken.Username, http.StatusConflict)
			return
		}
		existing.Username = name
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
	if req.DisplayName != nil {
		existing.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.HoldPriority != nil {
		existing.HoldPriority = *req.HoldPriority
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
	h.logAdmin(r, db.ActionUserUpdated, updated.Username+": "+strings.Join(req.changedFields(), ", "))
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
	target, err := h.DB.GetUserByID(id)
	if err != nil {
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
	h.logAdmin(r, db.ActionUserDeleted, target.Username)
	w.WriteHeader(http.StatusNoContent)
}

// changedFields names the fields present in an update request, for the audit
// log. Values are left out so passwords never reach the log.
func (req *updateUserRequest) changedFields() []string {
	var f []string
	if req.Username != nil {
		f = append(f, "username")
	}
	if req.Password != nil {
		f = append(f, "password")
	}
	if req.Role != nil {
		f = append(f, "role="+*req.Role)
	}
	if req.WilsonGate != nil {
		f = append(f, fmt.Sprintf("wilson=%t", *req.WilsonGate))
	}
	if req.BrigmanGate != nil {
		f = append(f, fmt.Sprintf("brigman=%t", *req.BrigmanGate))
	}
	if req.DisplayName != nil {
		f = append(f, "display name")
	}
	if req.HoldPriority != nil {
		f = append(f, fmt.Sprintf("hold priority=%d", *req.HoldPriority))
	}
	if req.Cameras != nil {
		f = append(f, "cameras="+strings.Join(*req.Cameras, "+"))
	}
	return f
}

func (h *UsersHandler) logAdmin(r *http.Request, action, detail string) {
	a := db.NewActivity(UserFromContext(r.Context()), nil, action, detail)
	if err := h.DB.LogActivity(a); err != nil {
		slog.Error("failed to log user change", "error", err)
	}
}

// importRequest is the body for POST /api/users/import. TSV is the output of
// the old gate site's users table, as printed by `mysql -B`:
//
//	SELECT username, password, display_name, access_level,
//	       operate_wilson_gate, operate_brigman_gate FROM users
type importRequest struct {
	TSV string `json:"tsv"`
}

type importSkip struct {
	Username string `json:"username"`
	Reason   string `json:"reason"`
}

type importResponse struct {
	Imported []userResponse `json:"imported"`
	Skipped  []importSkip   `json:"skipped"`
}

// legacyRow is one parsed user from the old gate site.
type legacyRow struct {
	username, md5, displayName string
	accessLevel                int
	wilson, brigman            bool
}

var md5Hex = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// Import handles POST /api/users/import. It creates users from the old gate
// site's users table, keeping their md5 password hashes so they can log in
// with their existing password (upgraded to bcrypt on first login).
// Usernames that already exist are skipped, never overwritten. Everyone is
// imported as a regular user; gate's access_level (lower = more senior)
// becomes hold priority (higher = more senior).
func (h *UsersHandler) Import(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	rows, skipped, err := parseLegacyUsers(req.TSV)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	maxLevel := 0
	for _, row := range rows {
		maxLevel = max(maxLevel, row.accessLevel)
	}

	resp := importResponse{Imported: []userResponse{}, Skipped: skipped}
	var names []string
	for _, row := range rows {
		if _, err := h.DB.GetUserByUsername(row.username); err == nil {
			resp.Skipped = append(resp.Skipped, importSkip{row.username, "already exists"})
			continue
		}
		user := &db.User{
			Username:     row.username,
			PasswordHash: db.LegacyMD5Prefix + strings.ToLower(row.md5),
			Role:         "user",
			WilsonGate:   row.wilson,
			BrigmanGate:  row.brigman,
			Cameras:      defaultNewUserCameras,
			DisplayName:  row.displayName,
			HoldPriority: maxLevel - row.accessLevel,
		}
		if err := h.DB.CreateUser(user); err != nil {
			slog.Error("failed to import user", "username", row.username, "error", err)
			resp.Skipped = append(resp.Skipped, importSkip{row.username, "could not be saved"})
			continue
		}
		created, err := h.DB.GetUserByID(user.ID)
		if err != nil {
			created = user
		}
		resp.Imported = append(resp.Imported, toUserResponse(created))
		names = append(names, row.username)
	}

	if len(names) > 0 {
		slog.Info("users imported", "count", len(names))
		h.logAdmin(r, db.ActionUsersImported, strings.Join(names, ", "))
	}
	writeJSON(w, resp, http.StatusOK)
}

// parseLegacyUsers reads `mysql -B` output: a header row naming the columns,
// then tab-separated rows. Rows that cannot be used are returned as skips.
func parseLegacyUsers(tsv string) ([]legacyRow, []importSkip, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(tsv), "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		return nil, nil, fmt.Errorf("expected a header row and at least one user")
	}

	col := map[string]int{}
	for i, name := range strings.Split(lines[0], "\t") {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, need := range []string{"username", "password"} {
		if _, ok := col[need]; !ok {
			return nil, nil, fmt.Errorf("missing %q column in header", need)
		}
	}
	field := func(cells []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(cells) {
			return ""
		}
		v := strings.TrimSpace(cells[i])
		if v == "NULL" {
			return ""
		}
		return v
	}
	flag := func(cells []string, name string) bool {
		v := field(cells, name)
		return v != "" && v != "0"
	}

	var rows []legacyRow
	var skipped []importSkip
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := strings.Split(line, "\t")
		row := legacyRow{
			username:    field(cells, "username"),
			md5:         field(cells, "password"),
			displayName: field(cells, "display_name"),
			accessLevel: 1, // gate's default for users it created
			wilson:      flag(cells, "operate_wilson_gate"),
			brigman:     flag(cells, "operate_brigman_gate"),
		}
		if row.username == "" {
			continue
		}
		if !md5Hex.MatchString(row.md5) {
			skipped = append(skipped, importSkip{row.username, "password is not an md5 hash"})
			continue
		}
		if v := field(cells, "access_level"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				skipped = append(skipped, importSkip{row.username, "bad access_level " + v})
				continue
			}
			row.accessLevel = n
		}
		rows = append(rows, row)
	}
	return rows, skipped, nil
}
