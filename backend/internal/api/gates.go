package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
)

// gateIDs lists every gate the controller knows about.
var gateIDs = []int{0, 1}

// GateController is the part of gate.Controller the handlers use.
type GateController interface {
	gate.Commander
	GetStatus() []gate.GateStatus
}

// GatesHandler provides HTTP handlers for gate operations.
type GatesHandler struct {
	Controller GateController
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
	writeJSON(w, h.permittedStatus(user), http.StatusOK)
}

// Open handles POST /api/gates/{id}/open
// It publishes an open command to the specified gate via MQTT.
func (h *GatesHandler) Open(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, db.ActionOpen)
}

// Hold handles POST /api/gates/{id}/hold
// It publishes a hold command to keep the specified gate open via MQTT. A
// gate already held by someone who outranks the user cannot be taken over.
func (h *GatesHandler) Hold(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, db.ActionHold)
}

// Release handles POST /api/gates/{id}/release
// It publishes an unhold command to release the specified gate via MQTT. A
// hold placed by someone who outranks the user cannot be released.
func (h *GatesHandler) Release(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, db.ActionRelease)
}

// act runs one gate command for the authenticated user.
func (h *GatesHandler) act(w http.ResponseWriter, r *http.Request, action string) {
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

	op := &gate.Operator{Gates: h.Controller, Store: h.DB}
	if !writeGateError(w, op.Do(user, gateID, action, "")) {
		return
	}
	writeJSON(w, h.permittedStatus(user), http.StatusOK)
}

// writeGateError writes the HTTP response for an Operator error and reports
// whether there was none.
func writeGateError(w http.ResponseWriter, err error) bool {
	var outranked *gate.OutrankedError
	var cmdErr *gate.CommandError
	switch {
	case err == nil:
		return true
	case errors.Is(err, gate.ErrForbidden):
		writeJSONError(w, "forbidden: no access to this gate", http.StatusForbidden)
	case errors.As(err, &outranked):
		writeJSONError(w, outranked.Error(), http.StatusConflict)
	case errors.As(err, &cmdErr):
		writeJSONError(w, "failed to "+cmdErr.Action+" gate", http.StatusBadGateway)
	default:
		slog.Error("gate action failed", "error", err)
		writeJSONError(w, "gate action failed", http.StatusInternalServerError)
	}
	return false
}

func (h *GatesHandler) permittedStatus(user *db.User) []gate.GateStatus {
	permitted := []gate.GateStatus{}
	for _, s := range h.Controller.GetStatus() {
		if checkGatePermission(user, s.ID) {
			permitted = append(permitted, s)
		}
	}
	return permitted
}

// PermittedGates returns the ids of the gates user may operate.
func PermittedGates(user *db.User) []int {
	var ids []int
	for _, id := range gateIDs {
		if checkGatePermission(user, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// checkGatePermission returns true if the given user has permission to access
// the specified gate.
func checkGatePermission(user *db.User, gateID int) bool {
	return user != nil && user.CanOperateGate(gateID)
}
