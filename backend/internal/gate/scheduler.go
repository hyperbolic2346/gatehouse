package gate

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

// schedulerInterval is how often scheduled holds are checked; holds start
// and end within this long of their set times.
const schedulerInterval = 15 * time.Second

// ScheduleStore is the slice of the database the scheduler needs.
type ScheduleStore interface {
	OperatorStore
	SchedulesToStart(now time.Time) ([]db.ScheduledHold, error)
	SchedulesToEnd(now time.Time) ([]db.ScheduledHold, error)
	SetScheduleStatus(id int64, status, detail string) error
}

// Scheduler starts and ends scheduled holds (e.g. a delivery window). It
// runs in the server, so holds happen whether or not anyone has the page open.
type Scheduler struct {
	Op    *Operator
	Store ScheduleStore
	// Ready reports whether commands can reach the gate controller. While it
	// is false the scheduler waits rather than logging a failure every tick.
	Ready func() bool
	// Changed is called after any schedule changes status.
	Changed func()
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Run checks scheduled holds every schedulerInterval until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()
	for {
		s.Tick()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick starts every hold whose start time has come and ends every hold whose
// end time has come.
func (s *Scheduler) Tick() {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if s.Ready != nil && !s.Ready() {
		return
	}

	changed := false

	// End first, so a hold ending at 3:00 releases before one starting at
	// 3:00 holds; the later hold then owns the gate.
	toEnd, err := s.Store.SchedulesToEnd(now)
	if err != nil {
		slog.Error("scheduler: list holds to end", "error", err)
	}
	for _, h := range toEnd {
		if s.end(h) {
			changed = true
		}
	}

	toStart, err := s.Store.SchedulesToStart(now)
	if err != nil {
		slog.Error("scheduler: list holds to start", "error", err)
	}
	for _, h := range toStart {
		if s.start(h, now) {
			changed = true
		}
	}

	if changed && s.Changed != nil {
		s.Changed()
	}
}

// start holds the gate for h. It returns false if h should be retried on the
// next tick.
func (s *Scheduler) start(h db.ScheduledHold, now time.Time) bool {
	if !h.End.After(now) {
		return s.finish(h, db.ScheduleMissed, "the server was not running during this window")
	}

	user, err := s.Store.GetUserByID(h.UserID)
	if err != nil {
		return s.finish(h, db.ScheduleSkipped, "the user who scheduled it no longer exists")
	}

	err = s.Op.Do(user, h.GateID, db.ActionHold, scheduledDetail(h))
	var outranked *OutrankedError
	switch {
	case err == nil:
		slog.Info("scheduled hold started", "schedule_id", h.ID, "gate_id", h.GateID, "user", user.Username)
		return s.finish(h, db.ScheduleActive, "")
	case errors.Is(err, ErrForbidden):
		return s.finish(h, db.ScheduleSkipped, user.Name()+" no longer has access to this gate")
	case errors.As(err, &outranked):
		return s.finish(h, db.ScheduleSkipped, "the gate was held by "+outranked.Holder.Name())
	default:
		slog.Error("scheduled hold could not start; will retry", "schedule_id", h.ID, "error", err)
		return false
	}
}

// end releases the gate for h, but only if h's owner still holds it: if
// someone else has taken the hold over since, their hold is left alone. It
// returns false if h should be retried on the next tick.
func (s *Scheduler) end(h db.ScheduledHold) bool {
	status, _ := s.Op.Gates.Status(h.GateID)
	if !status.Held {
		return s.finish(h, db.ScheduleDone, "the gate was already released")
	}
	if status.HeldByUserID == nil || *status.HeldByUserID != h.UserID {
		holder := status.HeldBy
		if holder == "" {
			holder = "someone outside Gatehouse"
		}
		return s.finish(h, db.ScheduleDone, "left held: "+holder+" holds it now")
	}

	// The owner holds it, so release it as them; a deleted owner is
	// released by the system.
	user, err := s.Store.GetUserByID(h.UserID)
	if err != nil {
		user = nil
	}
	if err := s.Op.Do(user, h.GateID, db.ActionRelease, scheduledDetail(h)+" ended"); err != nil {
		var cmdErr *CommandError
		if errors.As(err, &cmdErr) {
			slog.Error("scheduled hold could not end; will retry", "schedule_id", h.ID, "error", err)
			return false
		}
		return s.finish(h, db.ScheduleDone, "could not release: "+err.Error())
	}
	slog.Info("scheduled hold ended", "schedule_id", h.ID, "gate_id", h.GateID)
	return s.finish(h, db.ScheduleDone, "")
}

func (s *Scheduler) finish(h db.ScheduledHold, status, detail string) bool {
	if err := s.Store.SetScheduleStatus(h.ID, status, detail); err != nil {
		slog.Error("scheduler: update schedule", "schedule_id", h.ID, "error", err)
		return false
	}
	return true
}

func scheduledDetail(h db.ScheduledHold) string {
	if h.Note != "" {
		return "scheduled: " + h.Note
	}
	return "scheduled"
}
