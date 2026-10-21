package api

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
)

// gateIDs lists every gate the controller knows about.
var gateIDs = []int{0, 1}

// GateController is the part of gate.Controller the handlers use.
type GateController interface {
	GetStatus() []gate.GateStatus
	Status(gateID int) (gate.GateStatus, bool)
	BroadcastStatus()
	Open(gateID int) error
	Hold(gateID int) error
	Release(gateID int) error
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
	h.act(w, r, db.ActionOpen, h.Controller.Open)
}

// Hold handles POST /api/gates/{id}/hold
// It publishes a hold command to keep the specified gate open via MQTT. A
// gate already held by someone who outranks the user cannot be taken over.
func (h *GatesHandler) Hold(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, db.ActionHold, h.Controller.Hold)
}

// Release handles POST /api/gates/{id}/release
// It publishes an unhold command to release the specified gate via MQTT. A
// hold placed by someone who outranks the user cannot be released.
func (h *GatesHandler) Release(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, db.ActionRelease, h.Controller.Release)
}

// act runs one gate command for the authenticated user: permission and hold
// priority checks, the audit log entry, then the command itself.
func (h *GatesHandler) act(w http.ResponseWriter, r *http.Request, action string, send func(int) error) {
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

	if action == db.ActionHold || action == db.ActionRelease {
		if holder := h.outrankingHolder(user, gateID); holder != nil {
			msg := fmt.Sprintf("%s is holding this gate; only they or an admin can change it", holder.Name())
			h.log(db.NewActivity(user, db.GateID(gateID), db.ActionDenied, action+": held by "+holder.Name()))
			slog.Info("gate action denied by hold priority", "gate_id", gateID, "action", action, "user", user.Username, "holder", holder.Username)
			writeJSONError(w, msg, http.StatusConflict)
			return
		}
	}

	// Log before sending so a status report that races back from the
	// controller is credited to this user.
	entry := db.NewActivity(user, db.GateID(gateID), action, "")
	h.log(entry)

	if err := send(gateID); err != nil {
		slog.Error("gate command failed", "gate_id", gateID, "action", action, "error", err)
		if entry.ID != 0 {
			if err := h.DB.MarkActivityFailed(entry.ID, err.Error()); err != nil {
				slog.Error("failed to mark gate action failed", "error", err)
			}
		}
		writeJSONError(w, "failed to "+action+" gate", http.StatusBadGateway)
		return
	}

	slog.Info("gate command sent", "gate_id", gateID, "action", action, "user", user.Username)
	if action == db.ActionHold {
		// A takeover changes the holder without any status change from
		// the controller, so other clients would not otherwise hear of it.
		h.Controller.BroadcastStatus()
	}
	writeJSON(w, h.permittedStatus(user), http.StatusOK)
}

// outrankingHolder returns the user holding gateID if they outrank user, so
// that user may neither take over nor release the hold. It returns nil when
// the gate is not held, is held from outside Gatehouse, is held by user
// themselves, or the holder no longer exists.
func (h *GatesHandler) outrankingHolder(user *db.User, gateID int) *db.User {
	status, ok := h.Controller.Status(gateID)
	if !ok || !status.Held || status.HeldByUserID == nil || *status.HeldByUserID == user.ID {
		return nil
	}
	holder, err := h.DB.GetUserByID(*status.HeldByUserID)
	if err != nil {
		return nil
	}
	if holdRank(holder) > holdRank(user) {
		return holder
	}
	return nil
}

// holdRank orders users for hold priority. Admins outrank everyone.
func holdRank(u *db.User) int {
	if u.Role == "admin" {
		return math.MaxInt
	}
	return u.HoldPriority
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

func (h *GatesHandler) log(a *db.Activity) {
	if h.DB == nil {
		return
	}
	if err := h.DB.LogActivity(a); err != nil {
		slog.Error("failed to log gate activity", "error", err)
	}
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
