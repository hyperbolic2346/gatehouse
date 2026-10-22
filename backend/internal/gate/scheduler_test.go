package gate

import (
	"testing"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

// fakeHardware stands in for the MQTT link: commands immediately update the
// controller's status, as the Arduino does.
type fakeHardware struct {
	*Controller
	fail bool
	sent []string
}

func (f *fakeHardware) Hold(id int) error {
	if f.fail {
		return errTest
	}
	f.sent = append(f.sent, "hold")
	f.SetStatus(id, "Held By Us")
	return nil
}

func (f *fakeHardware) Release(id int) error {
	if f.fail {
		return errTest
	}
	f.sent = append(f.sent, "release")
	f.SetStatus(id, "Not Held")
	return nil
}

type testErr string

func (e testErr) Error() string { return string(e) }

const errTest = testErr("mqtt down")

type schedFixture struct {
	t     *testing.T
	d     *db.DB
	hw    *fakeHardware
	s     *Scheduler
	now   time.Time
	ready bool
}

func newSchedFixture(t *testing.T) *schedFixture {
	c, d := newTestController(t)
	c.SetStatus(0, "Not Held")
	f := &schedFixture{t: t, d: d, hw: &fakeHardware{Controller: c}, now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), ready: true}
	f.s = &Scheduler{
		Op:    &Operator{Gates: f.hw, Store: d},
		Store: d,
		Ready: func() bool { return f.ready },
		Now:   func() time.Time { return f.now },
	}
	return f
}

func (f *schedFixture) user(name string, priority int) *db.User {
	u := &db.User{Username: name, PasswordHash: "x", Role: "user", WilsonGate: true, HoldPriority: priority}
	if err := f.d.CreateUser(u); err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *schedFixture) schedule(u *db.User, startIn, endIn time.Duration) *db.ScheduledHold {
	h := &db.ScheduledHold{GateID: 0, UserID: u.ID, Owner: u.Name(), Start: f.now.Add(startIn), End: f.now.Add(endIn), Note: "UPS"}
	if err := f.d.CreateSchedule(h); err != nil {
		f.t.Fatal(err)
	}
	return h
}

func (f *schedFixture) status(id int64) *db.ScheduledHold {
	h, err := f.d.GetSchedule(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

func TestScheduledHoldStartsAndEnds(t *testing.T) {
	f := newSchedFixture(t)
	nana := f.user("nana", 0)
	h := f.schedule(nana, time.Hour, 2*time.Hour)

	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.SchedulePending || len(f.hw.sent) != 0 {
		t.Fatalf("before start: status %s sent %v, want pending and nothing sent", got, f.hw.sent)
	}

	f.now = f.now.Add(time.Hour)
	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.ScheduleActive {
		t.Fatalf("at start: status %s, want active", got)
	}
	if s, _ := f.hw.Status(0); !s.Held || s.HeldByUserID == nil || *s.HeldByUserID != nana.ID {
		t.Fatalf("gate = %+v, want held by nana", s)
	}

	f.now = f.now.Add(time.Hour)
	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.ScheduleDone {
		t.Fatalf("at end: status %s, want done", got)
	}
	if s, _ := f.hw.Status(0); s.Held {
		t.Error("gate should be released at the end")
	}
	if len(f.hw.sent) != 2 {
		t.Errorf("sent = %v, want hold then release", f.hw.sent)
	}
}

func TestScheduledHoldLeavesTakenOverHoldAlone(t *testing.T) {
	f := newSchedFixture(t)
	nana := f.user("nana", 0)
	bob := f.user("bob", 0)
	h := f.schedule(nana, 0, time.Hour)
	f.s.Tick()

	// Bob takes over the hold mid-window.
	if err := f.s.Op.Do(bob, 0, db.ActionHold, ""); err != nil {
		t.Fatal(err)
	}

	f.now = f.now.Add(time.Hour)
	f.s.Tick()
	if s, _ := f.hw.Status(0); !s.Held {
		t.Error("bob's hold must survive nana's schedule ending")
	}
	if got := f.status(h.ID); got.Status != db.ScheduleDone || got.Detail == "" {
		t.Errorf("schedule = %+v, want done with a note about bob", got)
	}
}

func TestScheduledHoldSkippedWhenOutranked(t *testing.T) {
	f := newSchedFixture(t)
	senior := f.user("senior", 5)
	junior := f.user("junior", 0)
	if err := f.s.Op.Do(senior, 0, db.ActionHold, ""); err != nil {
		t.Fatal(err)
	}
	h := f.schedule(junior, 0, time.Hour)

	f.s.Tick()
	if got := f.status(h.ID); got.Status != db.ScheduleSkipped {
		t.Errorf("schedule = %+v, want skipped", got)
	}
}

func TestScheduledHoldWaitsWhileControllerUnreachable(t *testing.T) {
	f := newSchedFixture(t)
	nana := f.user("nana", 0)
	h := f.schedule(nana, 0, time.Hour)

	f.ready = false
	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.SchedulePending {
		t.Fatalf("while unreachable: status %s, want pending", got)
	}

	// Reachable but the command fails: still pending, retried next tick.
	f.ready, f.hw.fail = true, true
	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.SchedulePending {
		t.Fatalf("after failed command: status %s, want pending", got)
	}

	f.hw.fail = false
	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.ScheduleActive {
		t.Errorf("after recovery: status %s, want active", got)
	}
}

func TestScheduledHoldMissedEntirely(t *testing.T) {
	f := newSchedFixture(t)
	nana := f.user("nana", 0)
	h := f.schedule(nana, -2*time.Hour, -time.Hour)

	f.s.Tick()
	if got := f.status(h.ID).Status; got != db.ScheduleMissed || len(f.hw.sent) != 0 {
		t.Errorf("status %s sent %v, want missed and nothing sent", got, f.hw.sent)
	}
}

func TestBackToBackScheduledHoldsHandOver(t *testing.T) {
	f := newSchedFixture(t)
	nana := f.user("nana", 0)
	bob := f.user("bob", 0)
	first := f.schedule(nana, 0, time.Hour)
	second := f.schedule(bob, time.Hour, 2*time.Hour)
	f.s.Tick()

	f.now = f.now.Add(time.Hour)
	f.s.Tick()
	if f.status(first.ID).Status != db.ScheduleDone || f.status(second.ID).Status != db.ScheduleActive {
		t.Fatalf("first %s second %s, want done and active", f.status(first.ID).Status, f.status(second.ID).Status)
	}
	if s, _ := f.hw.Status(0); !s.Held || *s.HeldByUserID != bob.ID {
		t.Errorf("gate = %+v, want held by bob", s)
	}
}
