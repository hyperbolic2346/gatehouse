package gate

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

// Commander is the part of the Controller that operating a gate needs.
type Commander interface {
	Status(gateID int) (GateStatus, bool)
	BroadcastStatus()
	Open(gateID int) error
	Hold(gateID int) error
	Release(gateID int) error
}

// OperatorStore is the slice of the database operating a gate needs.
type OperatorStore interface {
	GetUserByID(id int64) (*db.User, error)
	LogActivity(a *db.Activity) error
	MarkActivityFailed(id int64, reason string) error
}

// ErrForbidden means the user may not operate the gate at all.
var ErrForbidden = errors.New("no access to this gate")

// OutrankedError means the gate is held by someone the user may not
// override.
type OutrankedError struct {
	Holder *db.User
}

func (e *OutrankedError) Error() string {
	return fmt.Sprintf("%s is holding this gate; only they or an admin can change it", e.Holder.Name())
}

// CommandError means the command could not be delivered to the controller.
type CommandError struct {
	Action string
	Err    error
}

func (e *CommandError) Error() string { return fmt.Sprintf("%s gate: %v", e.Action, e.Err) }
func (e *CommandError) Unwrap() error { return e.Err }

// Operator runs gate commands on behalf of a user, enforcing gate permissions
// and hold priority and recording every attempt in the activity log. Both the
// HTTP handlers and the hold scheduler go through it.
type Operator struct {
	Gates Commander
	Store OperatorStore
}

// Do sends action (db.ActionOpen, ActionHold or ActionRelease) to gateID for
// user. detail is recorded in the activity log. A nil user acts as the system
// and skips the permission and priority checks.
func (o *Operator) Do(user *db.User, gateID int, action, detail string) error {
	if user != nil && !user.CanOperateGate(gateID) {
		return ErrForbidden
	}

	if user != nil && (action == db.ActionHold || action == db.ActionRelease) {
		if holder := o.outrankingHolder(user, gateID); holder != nil {
			o.log(db.NewActivity(user, db.GateID(gateID), db.ActionDenied, action+": held by "+holder.Name()))
			slog.Info("gate action denied by hold priority", "gate_id", gateID, "action", action, "user", user.Username, "holder", holder.Username)
			return &OutrankedError{Holder: holder}
		}
	}

	var send func(int) error
	switch action {
	case db.ActionOpen:
		send = o.Gates.Open
	case db.ActionHold:
		send = o.Gates.Hold
	case db.ActionRelease:
		send = o.Gates.Release
	default:
		return fmt.Errorf("unknown gate action %q", action)
	}

	// Log before sending so a status report that races back from the
	// controller is credited to this user.
	entry := db.NewActivity(user, db.GateID(gateID), action, detail)
	if user == nil {
		entry.Actor = "Scheduler"
	}
	o.log(entry)

	if err := send(gateID); err != nil {
		slog.Error("gate command failed", "gate_id", gateID, "action", action, "error", err)
		if entry.ID != 0 {
			if err := o.Store.MarkActivityFailed(entry.ID, err.Error()); err != nil {
				slog.Error("failed to mark gate action failed", "error", err)
			}
		}
		return &CommandError{Action: action, Err: err}
	}

	slog.Info("gate command sent", "gate_id", gateID, "action", action, "user", entry.Actor)
	if action == db.ActionHold {
		// A takeover changes the holder without any status change from
		// the controller, so other clients would not otherwise hear of it.
		o.Gates.BroadcastStatus()
	}
	return nil
}

// outrankingHolder returns the user holding gateID if they outrank user. It
// returns nil when the gate is not held, is held from outside Gatehouse, is
// held by user themselves, or the holder no longer exists.
func (o *Operator) outrankingHolder(user *db.User, gateID int) *db.User {
	status, ok := o.Gates.Status(gateID)
	if !ok || !status.Held || status.HeldByUserID == nil || *status.HeldByUserID == user.ID {
		return nil
	}
	holder, err := o.Store.GetUserByID(*status.HeldByUserID)
	if err != nil {
		return nil
	}
	if holder.HoldRank() > user.HoldRank() {
		return holder
	}
	return nil
}

func (o *Operator) log(a *db.Activity) {
	if err := o.Store.LogActivity(a); err != nil {
		slog.Error("failed to log gate activity", "error", err)
	}
}
