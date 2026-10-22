package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
)

const (
	// maxScheduleLength caps a scheduled hold so a typo cannot leave the
	// gate open for days.
	maxScheduleLength = 24 * time.Hour
	// maxScheduleAhead limits how far ahead a hold can be booked.
	maxScheduleAhead = 60 * 24 * time.Hour
	maxNoteLength    = 200
)

// SchedulesHandler serves scheduled holds.
type SchedulesHandler struct {
	DB         *db.DB
	Controller GateController
	// Changed is called after a schedule is created or cancelled.
	Changed func()
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

type scheduleResponse struct {
	db.ScheduledHold
	// CanCancel is true for the hold's owner and for admins.
	CanCancel bool `json:"can_cancel"`
}

type createScheduleRequest struct {
	GateID int       `json:"gate_id"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Note   string    `json:"note"`
}

func (h *SchedulesHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func canCancel(user *db.User, s *db.ScheduledHold) bool {
	return user.Role == "admin" || user.ID == s.UserID
}

// List handles GET /api/schedules. It returns upcoming and in-progress holds
// on the gates the user can operate, soonest first.
func (h *SchedulesHandler) List(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	all, err := h.DB.OpenSchedules()
	if err != nil {
		slog.Error("failed to list schedules", "error", err)
		writeJSONError(w, "failed to list scheduled holds", http.StatusInternalServerError)
		return
	}
	out := []scheduleResponse{}
	for i := range all {
		if user.CanOperateGate(all[i].GateID) {
			out = append(out, scheduleResponse{all[i], canCancel(user, &all[i])})
		}
	}
	writeJSON(w, out, http.StatusOK)
}

// Create handles POST /api/schedules.
func (h *SchedulesHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())

	var req createScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	now := h.now()

	switch {
	case !user.CanOperateGate(req.GateID):
		writeJSONError(w, "forbidden: no access to this gate", http.StatusForbidden)
		return
	case req.Start.IsZero() || req.End.IsZero():
		writeJSONError(w, "start and end times are required", http.StatusBadRequest)
		return
	case !req.End.After(req.Start):
		writeJSONError(w, "the end time must be after the start time", http.StatusBadRequest)
		return
	case !req.End.After(now):
		writeJSONError(w, "that time has already passed", http.StatusBadRequest)
		return
	case req.End.Sub(req.Start) > maxScheduleLength:
		writeJSONError(w, "a scheduled hold can be at most 24 hours", http.StatusBadRequest)
		return
	case req.Start.Sub(now) > maxScheduleAhead:
		writeJSONError(w, "holds can be scheduled at most 60 days ahead", http.StatusBadRequest)
		return
	case len(req.Note) > maxNoteLength:
		writeJSONError(w, "the note is too long", http.StatusBadRequest)
		return
	}

	if clash, err := h.DB.OverlappingSchedule(req.GateID, req.Start, req.End); err != nil {
		slog.Error("failed to check schedule overlap", "error", err)
		writeJSONError(w, "failed to schedule hold", http.StatusInternalServerError)
		return
	} else if clash != nil {
		writeJSONError(w, clash.Owner+" already has this gate scheduled during that time", http.StatusConflict)
		return
	}

	s := &db.ScheduledHold{
		GateID: req.GateID,
		UserID: user.ID,
		Owner:  user.Name(),
		Start:  req.Start,
		End:    req.End,
		Note:   req.Note,
	}
	if err := h.DB.CreateSchedule(s); err != nil {
		slog.Error("failed to create schedule", "error", err)
		writeJSONError(w, "failed to schedule hold", http.StatusInternalServerError)
		return
	}

	h.log(user, s, db.ActionScheduleCreated)
	slog.Info("hold scheduled", "schedule_id", s.ID, "gate_id", s.GateID, "user", user.Username, "start", s.Start, "end", s.End)
	h.changed()
	writeJSON(w, scheduleResponse{*s, true}, http.StatusCreated)
}

// Cancel handles DELETE /api/schedules/{id}. Owners and admins may cancel. If
// the hold is in progress and its owner still holds the gate, the gate is
// released.
func (h *SchedulesHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, "invalid schedule id", http.StatusBadRequest)
		return
	}
	s, err := h.DB.GetSchedule(id)
	if err != nil || !user.CanOperateGate(s.GateID) {
		writeJSONError(w, "scheduled hold not found", http.StatusNotFound)
		return
	}
	if !canCancel(user, s) {
		writeJSONError(w, "only "+s.Owner+" or an admin can cancel this", http.StatusForbidden)
		return
	}
	if s.Status != db.SchedulePending && s.Status != db.ScheduleActive {
		writeJSONError(w, "this hold has already finished", http.StatusConflict)
		return
	}

	if s.Status == db.ScheduleActive {
		status, _ := h.Controller.Status(s.GateID)
		if status.Held && status.HeldByUserID != nil && *status.HeldByUserID == s.UserID {
			op := &gate.Operator{Gates: h.Controller, Store: h.DB}
			if !writeGateError(w, op.Do(user, s.GateID, db.ActionRelease, "cancelled scheduled hold")) {
				return
			}
		}
	}

	detail := "cancelled by " + user.Name()
	if err := h.DB.SetScheduleStatus(s.ID, db.ScheduleCancelled, detail); err != nil {
		slog.Error("failed to cancel schedule", "error", err)
		writeJSONError(w, "failed to cancel", http.StatusInternalServerError)
		return
	}
	h.log(user, s, db.ActionScheduleCancelled)
	h.changed()
	w.WriteHeader(http.StatusNoContent)
}

func (h *SchedulesHandler) log(user *db.User, s *db.ScheduledHold, action string) {
	detail := s.Start.Local().Format("Mon Jan 2 3:04 PM") + " – " + s.End.Local().Format("3:04 PM")
	if s.Note != "" {
		detail += " · " + s.Note
	}
	if err := h.DB.LogActivity(db.NewActivity(user, db.GateID(s.GateID), action, detail)); err != nil {
		slog.Error("failed to log schedule change", "error", err)
	}
}

func (h *SchedulesHandler) changed() {
	if h.Changed != nil {
		h.Changed()
	}
}
