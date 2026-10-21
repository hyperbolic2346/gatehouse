package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateAddsColumnsToExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// The users table as the first release created it.
	if _, err := conn.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user',
		wilson_gate BOOLEAN NOT NULL DEFAULT 0, brigman_gate BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO users (username, password_hash, role) VALUES ('knobby', 'x', 'admin');`); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	d, err := New(path, nil)
	if err != nil {
		t.Fatalf("New on old schema: %v", err)
	}
	defer d.Close()
	u, err := d.GetUserByUsername("knobby")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.DisplayName != "" || u.HoldPriority != 0 || u.Name() != "knobby" {
		t.Errorf("migrated user = %+v, want empty display name, priority 0", u)
	}

	// Reopening must not try to add the columns again.
	d.Close()
	d2, err := New(path, nil)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	d2.Close()
}

func TestLastHoldEventIgnoresUnrelatedAndFailedRows(t *testing.T) {
	d := newTestDB(t, nil)
	u := &User{ID: 7, Username: "alice", DisplayName: "Alice"}

	if got, err := d.LastHoldEvent(0); err != nil || got != nil {
		t.Fatalf("empty log: got %v, %v; want nil, nil", got, err)
	}

	hold := NewActivity(u, GateID(0), ActionHold, "")
	mustLog(t, d, hold)
	mustLog(t, d, NewActivity(u, GateID(0), ActionOpen, ""))
	mustLog(t, d, NewActivity(u, GateID(1), ActionRelease, ""))
	mustLog(t, d, NewActivity(u, nil, ActionLogin, ""))

	got, err := d.LastHoldEvent(0)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != hold.ID || got.Actor != "Alice" || *got.UserID != 7 {
		t.Fatalf("LastHoldEvent = %+v, want the hold row by Alice", got)
	}

	release := NewActivity(u, GateID(0), ActionRelease, "")
	mustLog(t, d, release)
	if err := d.MarkActivityFailed(release.ID, "mqtt down"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.LastHoldEvent(0)
	if got.ID != hold.ID {
		t.Errorf("failed release should not count; LastHoldEvent = %+v", got)
	}

	all, err := d.ListActivity(0, 10, GateID(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Action != "release_failed" || all[0].Detail != "mqtt down" {
		t.Errorf("gate 0 activity = %+v, want 3 rows newest first with release_failed", all)
	}
	page, _ := d.ListActivity(all[0].ID, 10, nil)
	if len(page) != 4 {
		t.Errorf("rows before newest = %d, want 4", len(page))
	}
}

func mustLog(t *testing.T, d *DB, a *Activity) {
	t.Helper()
	if err := d.LogActivity(a); err != nil {
		t.Fatalf("LogActivity: %v", err)
	}
}
