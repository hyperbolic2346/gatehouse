package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
)

// GatesHandler provides HTTP handlers for gate operations.
type GatesHandler struct {
	Controller *gate.Controller
	DB         *db.DB
}

// Status handles GET /api/gates
// It returns the cached status of all gates that the authenticated user has
// permission to access.
func (h *GatesHandler) Status(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	statuses := h.Controller.GetStatus()

	// Filter gates to only include those the user has permission to see.
	var permitted []gate.GateStatus
	for _, s := range statuses {
		if checkGatePermission(user, s.ID) {
			permitted = append(permitted, s)
		}
	}

	if permitted == nil {
		permitted = []gate.GateStatus{}
	}

	writeJSON(w, permitted, http.StatusOK)
}

// Open handles POST /api/gates/{id}/open
// It publishes an open command to the specified gate via MQTT.
func (h *GatesHandler) Open(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	gateID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSONError(w, "invalid gate id", http.StatusBadRequest)
		return
	}

	if !checkGatePermission(user, gateID) {
		writeJSONError(w, "forbidden: no access to this gate", http.StatusForbidden)
		return
	}

	if err := h.Controller.Open(gateID); err != nil {
		slog.Error("failed to open gate", "gate_id", gateID, "error", err)
		writeJSONError(w, "failed to open gate", http.StatusBadGateway)
		return
	}

	slog.Info("gate opened", "gate_id", gateID, "user", user.Username)
	writeJSON(w, h.Controller.GetStatus(), http.StatusOK)
}

// Hold handles POST /api/gates/{id}/hold
// It publishes a hold command to keep the specified gate open via MQTT.
func (h *GatesHandler) Hold(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	gateID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSONError(w, "invalid gate id", http.StatusBadRequest)
		return
	}

	if !checkGatePermission(user, gateID) {
		writeJSONError(w, "forbidden: no access to this gate", http.StatusForbidden)
		return
	}

	if err := h.Controller.Hold(gateID); err != nil {
		slog.Error("failed to hold gate", "gate_id", gateID, "error", err)
		writeJSONError(w, "failed to hold gate", http.StatusBadGateway)
		return
	}

	slog.Info("gate held", "gate_id", gateID, "user", user.Username)
	writeJSON(w, h.Controller.GetStatus(), http.StatusOK)
}

// Release handles POST /api/gates/{id}/release
// It publishes an unhold command to release the specified gate via MQTT.
func (h *GatesHandler) Release(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	gateID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSONError(w, "invalid gate id", http.StatusBadRequest)
		return
	}

	if !checkGatePermission(user, gateID) {
		writeJSONError(w, "forbidden: no access to this gate", http.StatusForbidden)
		return
	}

	if err := h.Controller.Release(gateID); err != nil {
		slog.Error("failed to release gate", "gate_id", gateID, "error", err)
		writeJSONError(w, "failed to release gate", http.StatusBadGateway)
		return
	}

	slog.Info("gate released", "gate_id", gateID, "user", user.Username)
	writeJSON(w, h.Controller.GetStatus(), http.StatusOK)
}

// checkGatePermission returns true if the given user has permission to access
// the specified gate. Gate 0 is "Wilson" and gate 1 is "Brigman". Admin users
// have access to all gates.
func checkGatePermission(user *db.User, gateID int) bool {
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	switch gateID {
	case 0:
		return user.WilsonGate
	case 1:
		return user.BrigmanGate
	default:
		return false
	}
}
