package gate

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/ws"
)

// MQTT topic patterns for the Arduino gate controller.
const (
	// statusTopicPattern is the MQTT topic the Arduino publishes hold status
	// updates to. The + wildcard matches the gate ID (0 or 1).
	statusTopicPattern = "home/gate/+/hold_status"

	// commandTopicFmt is the MQTT topic we publish commands to.
	// Format: home/gate/{id}/command
	commandTopicFmt = "home/gate/%d/command"
)

// gateNames maps hardware gate IDs to human-readable names.
var gateNames = map[int]string{
	0: "Wilson",
	1: "Brigman",
}

// attributionWindow is how long after a Gatehouse hold command a "held"
// report from the controller is credited to that user. A held report with no
// recent hold command came from somewhere else (Home Assistant, a remote).
const attributionWindow = 2 * time.Minute

// GateStatus represents the current state of a single gate.
type GateStatus struct {
	Name       string `json:"name"`
	ID         int    `json:"id"`
	HoldStatus string `json:"hold_status"`
	Held       bool   `json:"held"`
	// HeldBy is the display name of the Gatehouse user holding the gate.
	// Empty when the gate is not held or was held from outside Gatehouse.
	HeldBy string `json:"held_by,omitempty"`
	// HeldSince is when the current hold began, when known.
	HeldSince *time.Time `json:"held_since,omitempty"`
	// HeldByUserID identifies the holder for priority checks; not sent to
	// clients.
	HeldByUserID *int64 `json:"-"`
}

// IsHeld interprets the controller's hold_status payload ("Held By Us",
// "Not Held").
func IsHeld(holdStatus string) bool {
	s := strings.ToLower(strings.TrimSpace(holdStatus))
	return strings.Contains(s, "held") && !strings.HasPrefix(s, "not")
}

// ActivityStore is the slice of the database the controller needs to record
// status changes and work out who holds a gate.
type ActivityStore interface {
	LogActivity(a *db.Activity) error
	LastHoldEvent(gateID int) (*db.Activity, error)
}

// gateStatusMessage is broadcast over WebSocket when gate status changes.
type gateStatusMessage struct {
	Type string       `json:"type"`
	Data []GateStatus `json:"data"`
}

// Controller communicates with the Arduino gate controller over MQTT and
// tracks the last known hold status for each gate.
type Controller struct {
	client mqtt.Client
	hub    *ws.Hub
	store  ActivityStore
	mu     sync.RWMutex
	status map[int]string // gate ID → hold_status string
}

// New creates a new gate Controller. The client may be nil initially and
// set later via SetClient once the MQTT connection is established.
func New(client mqtt.Client, hub *ws.Hub, store ActivityStore) *Controller {
	return &Controller{
		client: client,
		hub:    hub,
		store:  store,
		status: make(map[int]string),
	}
}

// SetClient updates the MQTT client after a deferred connection.
func (c *Controller) SetClient(client mqtt.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = client
}

// Ready reports whether commands can currently reach the controller.
func (c *Controller) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client != nil && c.client.IsConnected()
}

// Subscribe registers the MQTT topic subscriptions for gate status updates.
// Call this after the MQTT client is connected (e.g. in the OnConnect handler).
func (c *Controller) Subscribe() {
	if c.client == nil {
		return
	}
	token := c.client.Subscribe(statusTopicPattern, 1, c.handleStatus)
	token.Wait()
	if err := token.Error(); err != nil {
		slog.Error("gate mqtt subscribe failed", "topic", statusTopicPattern, "error", err)
		return
	}
	slog.Info("gate mqtt subscribed", "topic", statusTopicPattern)
}

// handleStatus processes an MQTT message on home/gate/{id}/hold_status.
func (c *Controller) handleStatus(_ mqtt.Client, msg mqtt.Message) {
	// Parse gate ID from topic: home/gate/{id}/hold_status
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 4 {
		slog.Warn("gate mqtt unexpected topic format", "topic", msg.Topic())
		return
	}

	gateID, err := strconv.Atoi(parts[2])
	if err != nil {
		slog.Warn("gate mqtt invalid gate id", "topic", msg.Topic(), "error", err)
		return
	}

	c.SetStatus(gateID, string(msg.Payload()))
}

// SetStatus records a hold_status report for gateID. When the gate moves
// between held and not held it logs the transition and pushes the new status
// to WebSocket clients.
func (c *Controller) SetStatus(gateID int, holdStatus string) {
	c.mu.Lock()
	old, known := c.status[gateID]
	c.status[gateID] = holdStatus
	c.mu.Unlock()

	slog.Debug("gate status received", "gate_id", gateID, "hold_status", holdStatus)
	if old == holdStatus {
		return
	}
	slog.Info("gate status changed", "gate_id", gateID, "hold_status", holdStatus)

	// The first report after startup is the broker's retained message, not
	// a change; only log it if it disagrees with what the log last saw.
	if !known || IsHeld(old) != IsHeld(holdStatus) {
		c.logTransition(gateID, holdStatus)
	}
	c.BroadcastStatus()
}

// logTransition records a held/cleared report, crediting a held report to
// the Gatehouse user whose hold command just preceded it.
func (c *Controller) logTransition(gateID int, holdStatus string) {
	if c.store == nil {
		return
	}
	last, err := c.store.LastHoldEvent(gateID)
	if err != nil {
		slog.Error("gate hold lookup failed", "gate_id", gateID, "error", err)
	}

	action := db.ActionCleared
	if IsHeld(holdStatus) {
		action = db.ActionHeld
	}
	if last != nil && last.Action == action {
		return // already recorded (e.g. retained message after a restart)
	}
	if last == nil && action == db.ActionCleared {
		return // nothing was ever held; "not held" is not news
	}

	a := &db.Activity{GateID: db.GateID(gateID), Action: action, Detail: holdStatus}
	if action == db.ActionHeld && last != nil && last.Action == db.ActionHold && time.Since(last.At) < attributionWindow {
		a.UserID = last.UserID
		a.Actor = last.Actor
	}
	if err := c.store.LogActivity(a); err != nil {
		slog.Error("gate status log failed", "gate_id", gateID, "error", err)
	}
}

// BroadcastStatus pushes the current status to every WebSocket client,
// limited to the gates each client may operate.
func (c *Controller) BroadcastStatus() {
	if c.hub == nil {
		return
	}
	statuses := c.GetStatus()
	c.hub.BroadcastGates(func(allowed func(gateID int) bool) []byte {
		visible := []GateStatus{}
		for _, s := range statuses {
			if allowed(s.ID) {
				visible = append(visible, s)
			}
		}
		data, err := json.Marshal(gateStatusMessage{Type: "gate_status", Data: visible})
		if err != nil {
			slog.Error("failed to marshal gate status", "error", err)
			return nil
		}
		return data
	})
}

// GetStatus returns the cached status of all known gates, ordered by id,
// with the current holder filled in for held gates.
func (c *Controller) GetStatus() []GateStatus {
	c.mu.RLock()
	raw := make(map[int]string, len(c.status))
	for id, hs := range c.status {
		raw[id] = hs
	}
	c.mu.RUnlock()

	statuses := make([]GateStatus, 0, len(gateNames))
	for id, name := range gateNames {
		hs := raw[id]
		if hs == "" {
			hs = "unknown"
		}
		s := GateStatus{Name: name, ID: id, HoldStatus: hs, Held: IsHeld(hs)}
		if s.Held {
			c.fillHolder(&s)
		}
		statuses = append(statuses, s)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

// Status returns the status of a single gate.
func (c *Controller) Status(gateID int) (GateStatus, bool) {
	for _, s := range c.GetStatus() {
		if s.ID == gateID {
			return s, true
		}
	}
	return GateStatus{}, false
}

// fillHolder works out who holds a held gate from the activity log.
func (c *Controller) fillHolder(s *GateStatus) {
	if c.store == nil {
		return
	}
	last, err := c.store.LastHoldEvent(s.ID)
	if err != nil {
		slog.Error("gate holder lookup failed", "gate_id", s.ID, "error", err)
		return
	}
	if last == nil {
		return
	}
	switch last.Action {
	case db.ActionHold, db.ActionHeld:
		// A hold through Gatehouse, or a held report: credited to a user
		// when one caused it, otherwise an outside hold since that time.
		at := last.At
		s.HeldSince = &at
		s.HeldBy = last.Actor
		s.HeldByUserID = last.UserID
	}
	// A release or cleared report followed by a held status means the
	// controller was re-held from outside Gatehouse without us seeing the
	// transition; the holder and start time are unknown.
}

// Open publishes an "open" command for the specified gate.
func (c *Controller) Open(gateID int) error {
	return c.publish(gateID, "open")
}

// Hold publishes a "hold" command for the specified gate.
func (c *Controller) Hold(gateID int) error {
	return c.publish(gateID, "hold")
}

// Release publishes an "unhold" command for the specified gate.
func (c *Controller) Release(gateID int) error {
	return c.publish(gateID, "unhold")
}

// publish sends an MQTT command to the specified gate.
func (c *Controller) publish(gateID int, command string) error {
	if c.client == nil {
		return fmt.Errorf("gate mqtt not connected")
	}
	topic := fmt.Sprintf(commandTopicFmt, gateID)
	token := c.client.Publish(topic, 1, false, command)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("gate mqtt publish %s to %s: %w", command, topic, err)
	}
	slog.Info("gate command sent", "gate_id", gateID, "command", command, "topic", topic)
	return nil
}
