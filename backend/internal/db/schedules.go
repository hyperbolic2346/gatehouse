package db

import (
	"fmt"
	"time"
)

// Scheduled hold statuses. Pending and active holds are "open"; the rest are
// final.
const (
	SchedulePending   = "pending"   // waiting for its start time
	ScheduleActive    = "active"    // the gate was held at the start time
	ScheduleDone      = "done"      // ended at its end time
	ScheduleCancelled = "cancelled" // cancelled by its owner or an admin
	ScheduleSkipped   = "skipped"   // could not start (outranked, access revoked)
	ScheduleMissed    = "missed"    // the server was down for the whole window
)

// Activity actions for scheduled holds.
const (
	ActionScheduleCreated   = "schedule_created"
	ActionScheduleCancelled = "schedule_cancelled"
)

// ScheduledHold holds a gate open for a window, e.g. for a delivery.
type ScheduledHold struct {
	ID     int64 `json:"id"`
	GateID int   `json:"gate_id"`
	UserID int64 `json:"user_id"`
	// Owner is the scheduling user's display name when it was created.
	Owner     string    `json:"owner"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Note      string    `json:"note"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

const scheduleColumns = "id, gate_id, user_id, owner, start_at, end_at, note, status, detail, created_at"

// sortableTime is a fixed-width UTC layout, so stored times compare correctly
// as strings in SQL. (RFC3339Nano trims trailing zeros and does not.)
const sortableTime = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(t time.Time) string { return t.UTC().Format(sortableTime) }

func scanSchedule(row rowScanner) (*ScheduledHold, error) {
	var (
		h                     ScheduledHold
		start, end, createdAt string
	)
	if err := row.Scan(&h.ID, &h.GateID, &h.UserID, &h.Owner, &start, &end, &h.Note, &h.Status, &h.Detail, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if h.Start, err = time.Parse(time.RFC3339Nano, start); err != nil {
		return nil, fmt.Errorf("parse schedule start: %w", err)
	}
	if h.End, err = time.Parse(time.RFC3339Nano, end); err != nil {
		return nil, fmt.Errorf("parse schedule end: %w", err)
	}
	if h.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, fmt.Errorf("parse schedule created_at: %w", err)
	}
	return &h, nil
}

func (d *DB) querySchedules(query string, args ...any) ([]ScheduledHold, error) {
	rows, err := d.conn.Query("SELECT "+scheduleColumns+" FROM scheduled_holds "+query, args...)
	if err != nil {
		return nil, fmt.Errorf("query schedules: %w", err)
	}
	defer rows.Close()
	out := []ScheduledHold{}
	for rows.Next() {
		h, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query schedules: %w", err)
	}
	return out, nil
}

// CreateSchedule stores h as pending, filling in its ID and CreatedAt.
func (d *DB) CreateSchedule(h *ScheduledHold) error {
	h.Status = SchedulePending
	h.CreatedAt = time.Now()
	res, err := d.conn.Exec(
		"INSERT INTO scheduled_holds (gate_id, user_id, owner, start_at, end_at, note, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		h.GateID, h.UserID, h.Owner, formatTime(h.Start), formatTime(h.End), h.Note, h.Status, formatTime(h.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}
	if h.ID, err = res.LastInsertId(); err != nil {
		return fmt.Errorf("create schedule: last insert id: %w", err)
	}
	return nil
}

// GetSchedule returns one scheduled hold.
func (d *DB) GetSchedule(id int64) (*ScheduledHold, error) {
	h, err := scanSchedule(d.conn.QueryRow("SELECT "+scheduleColumns+" FROM scheduled_holds WHERE id = ?", id))
	if err != nil {
		return nil, fmt.Errorf("get schedule %d: %w", id, err)
	}
	return h, nil
}

// SetScheduleStatus moves a scheduled hold to status, recording why.
func (d *DB) SetScheduleStatus(id int64, status, detail string) error {
	if _, err := d.conn.Exec("UPDATE scheduled_holds SET status = ?, detail = ? WHERE id = ?", status, detail, id); err != nil {
		return fmt.Errorf("set schedule status: %w", err)
	}
	return nil
}

// OpenSchedules returns pending and active holds, soonest first.
func (d *DB) OpenSchedules() ([]ScheduledHold, error) {
	return d.querySchedules("WHERE status IN (?, ?) ORDER BY start_at, id", SchedulePending, ScheduleActive)
}

// SchedulesToStart returns pending holds whose start time has come.
func (d *DB) SchedulesToStart(now time.Time) ([]ScheduledHold, error) {
	return d.querySchedules("WHERE status = ? AND start_at <= ? ORDER BY start_at, id", SchedulePending, formatTime(now))
}

// SchedulesToEnd returns active holds whose end time has come.
func (d *DB) SchedulesToEnd(now time.Time) ([]ScheduledHold, error) {
	return d.querySchedules("WHERE status = ? AND end_at <= ? ORDER BY end_at, id", ScheduleActive, formatTime(now))
}

// OverlappingSchedule returns an open hold on gateID whose window overlaps
// [start, end), or nil. Windows that merely touch do not overlap.
func (d *DB) OverlappingSchedule(gateID int, start, end time.Time) (*ScheduledHold, error) {
	hs, err := d.querySchedules(
		"WHERE gate_id = ? AND status IN (?, ?) AND start_at < ? AND end_at > ? ORDER BY start_at LIMIT 1",
		gateID, SchedulePending, ScheduleActive, formatTime(end), formatTime(start),
	)
	if err != nil || len(hs) == 0 {
		return nil, err
	}
	return &hs[0], nil
}
