package gate

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

func newTestController(t *testing.T) (*Controller, *db.DB) {
	t.Helper()
	d, err := db.New(filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(nil, nil, d), d
}

func TestIsHeld(t *testing.T) {
	for s, want := range map[string]bool{"Held By Us": true, "Not Held": false, "unknown": false, "": false, "HELD BY US": true} {
		if got := IsHeld(s); got != want {
			t.Errorf("IsHeld(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestHoldThroughGatehouseIsCreditedToUser(t *testing.T) {
	c, d := newTestController(t)
	alice := &db.User{ID: 3, Username: "alice", DisplayName: "Alice"}

	c.SetStatus(0, "Not Held")
	if err := d.LogActivity(db.NewActivity(alice, db.GateID(0), db.ActionHold, "")); err != nil {
		t.Fatal(err)
	}
	c.SetStatus(0, "Held By Us")

	s, _ := c.Status(0)
	if !s.Held || s.HeldBy != "Alice" || s.HeldByUserID == nil || *s.HeldByUserID != 3 || s.HeldSince == nil {
		t.Fatalf("status = %+v, want held by Alice", s)
	}

	c.SetStatus(0, "Not Held")
	s, _ = c.Status(0)
	if s.Held || s.HeldBy != "" || s.HeldSince != nil {
		t.Errorf("after release status = %+v, want not held, no holder", s)
	}
}

func TestOutsideHoldHasNoHolder(t *testing.T) {
	c, d := newTestController(t)
	alice := &db.User{ID: 3, Username: "alice"}

	// A hold command long ago that never took effect must not be credited
	// with a hold that Home Assistant places later.
	old := db.NewActivity(alice, db.GateID(1), db.ActionHold, "")
	old.At = time.Now().Add(-time.Hour)
	if err := d.LogActivity(old); err != nil {
		t.Fatal(err)
	}
	c.SetStatus(1, "Not Held")
	c.SetStatus(1, "Held By Us")

	s, _ := c.Status(1)
	if !s.Held || s.HeldBy != "" || s.HeldByUserID != nil {
		t.Fatalf("status = %+v, want held with no Gatehouse holder", s)
	}
	if s.HeldSince == nil {
		t.Error("outside hold should still report when it started")
	}
}

func TestRetainedStatusAfterRestartIsNotLoggedTwice(t *testing.T) {
	c, d := newTestController(t)
	c.SetStatus(0, "Held By Us")

	// A fresh controller sees the broker's retained message again.
	c2 := New(nil, nil, d)
	c2.SetStatus(0, "Held By Us")

	rows, err := d.ListActivity(0, 10, db.GateID(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != db.ActionHeld {
		t.Errorf("activity = %+v, want a single held row", rows)
	}
}

func TestGetStatusIsOrderedByID(t *testing.T) {
	c, _ := newTestController(t)
	for i := 0; i < 20; i++ {
		s := c.GetStatus()
		if len(s) != 2 || s[0].ID != 0 || s[1].ID != 1 {
			t.Fatalf("GetStatus = %+v, want gates 0 then 1", s)
		}
	}
}

func TestInitialNotHeldIsNotLogged(t *testing.T) {
	c, d := newTestController(t)
	c.SetStatus(0, "Not Held")
	rows, err := d.ListActivity(0, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("activity = %+v, want nothing for a gate that was never held", rows)
	}
}
