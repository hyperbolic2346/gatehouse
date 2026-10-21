package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

const (
	defaultActivityLimit = 100
	maxActivityLimit     = 500
)

// ActivityHandler serves the audit log.
type ActivityHandler struct {
	DB *db.DB
}

// List handles GET /api/activity?before=ID&limit=N&gate=ID
// It returns audit log rows newest first. Page backwards by passing the
// smallest id seen as before.
func (h *ActivityHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var before int64
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSONError(w, "invalid before", http.StatusBadRequest)
			return
		}
		before = n
	}

	limit := defaultActivityLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSONError(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = min(n, maxActivityLimit)
	}

	var gateID *int
	if v := q.Get("gate"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeJSONError(w, "invalid gate", http.StatusBadRequest)
			return
		}
		gateID = &n
	}

	rows, err := h.DB.ListActivity(before, limit, gateID)
	if err != nil {
		slog.Error("failed to list activity", "error", err)
		writeJSONError(w, "failed to list activity", http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows, http.StatusOK)
}
