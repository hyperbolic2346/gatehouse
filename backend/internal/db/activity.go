package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Activity actions. Gate actions carry a gate id; account actions do not.
const (
	ActionLogin       = "login"
	ActionLoginFailed = "login_failed"
	ActionOpen        = "open"
	ActionHold        = "hold"
	ActionRelease     = "release"
	ActionDenied      = "denied"
	// ActionHeld and ActionCleared record hold-status transitions reported by
	// the gate controller itself, whoever caused them (Gatehouse, Home
	// Assistant, the hardware).
	ActionHeld    = "held"
	ActionCleared = "cleared"

	ActionUserCreated   = "user_created"
	ActionUserUpdated   = "user_updated"
	ActionUserDeleted   = "user_deleted"
	ActionUsersImported = "users_imported"

	// failedSuffix is appended to a gate action whose command never reached
	// the controller, so it no longer counts toward hold attribution.
	failedSuffix = "_failed"
)

// Activity is one row of the audit log.
type Activity struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	UserID *int64    `json:"user_id,omitempty"`
	// Actor is the user's display name at the time, kept so the log still
	// reads correctly after the user is renamed or deleted. Empty for
	// status changes reported by the gate controller.
	Actor  string `json:"actor"`
	GateID *int   `json:"gate_id,omitempty"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// NewActivity builds an activity row for user (which may be nil).
func NewActivity(user *User, gateID *int, action, detail string) *Activity {
	a := &Activity{GateID: gateID, Action: action, Detail: detail}
	if user != nil {
		id := user.ID
		a.UserID = &id
		a.Actor = user.Name()
	}
	return a
}

// GateID is a convenience for building the *int an Activity wants.
func GateID(id int) *int { return &id }

// LogActivity appends a to the audit log, filling in its ID and, if unset,
// its timestamp.
func (d *DB) LogActivity(a *Activity) error {
	if a.At.IsZero() {
		a.At = time.Now()
	}
	res, err := d.conn.Exec(
		"INSERT INTO activity_log (at, user_id, actor, gate_id, action, detail) VALUES (?, ?, ?, ?, ?, ?)",
		a.At.UTC().Format(time.RFC3339Nano), a.UserID, a.Actor, a.GateID, a.Action, a.Detail,
	)
	if err != nil {
		return fmt.Errorf("log activity: %w", err)
	}
	a.ID, err = res.LastInsertId()
	if err != nil {
		return fmt.Errorf("log activity: last insert id: %w", err)
	}
	return nil
}

// MarkActivityFailed records that the command logged as id never took effect.
func (d *DB) MarkActivityFailed(id int64, reason string) error {
	_, err := d.conn.Exec(
		"UPDATE activity_log SET action = action || ?, detail = ? WHERE id = ?",
		failedSuffix, reason, id,
	)
	if err != nil {
		return fmt.Errorf("mark activity failed: %w", err)
	}
	return nil
}

// ListActivity returns up to limit rows, newest first, with id < beforeID
// (0 means from the newest). gateID, when non-nil, restricts to that gate.
func (d *DB) ListActivity(beforeID int64, limit int, gateID *int) ([]Activity, error) {
	var where []string
	var args []any
	if beforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, beforeID)
	}
	if gateID != nil {
		where = append(where, "gate_id = ?")
		args = append(args, *gateID)
	}
	query := "SELECT id, at, user_id, actor, gate_id, action, detail FROM activity_log"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	out := []Activity{}
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	return out, nil
}

// LastHoldEvent returns the most recent row for gateID that bears on who
// holds it: a hold or release through Gatehouse, or a held/cleared status
// change from the controller. It returns nil, nil when there is none.
func (d *DB) LastHoldEvent(gateID int) (*Activity, error) {
	row := d.conn.QueryRow(
		"SELECT id, at, user_id, actor, gate_id, action, detail FROM activity_log "+
			"WHERE gate_id = ? AND action IN (?, ?, ?, ?) ORDER BY id DESC LIMIT 1",
		gateID, ActionHold, ActionRelease, ActionHeld, ActionCleared,
	)
	a, err := scanActivity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func scanActivity(row rowScanner) (*Activity, error) {
	var (
		a      Activity
		at     string
		userID sql.NullInt64
		gateID sql.NullInt64
	)
	if err := row.Scan(&a.ID, &at, &userID, &a.Actor, &gateID, &a.Action, &a.Detail); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan activity: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, fmt.Errorf("parse activity time %q: %w", at, err)
	}
	a.At = t
	if userID.Valid {
		a.UserID = &userID.Int64
	}
	if gateID.Valid {
		g := int(gateID.Int64)
		a.GateID = &g
	}
	return &a, nil
}
