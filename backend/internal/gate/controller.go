package gate

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"

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

// GateStatus represents the current state of a single gate.
type GateStatus struct {
	Name       string `json:"name"`
	ID         int    `json:"id"`
	HoldStatus string `json:"hold_status"`
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
	mu     sync.RWMutex
	status map[int]string // gate ID → hold_status string
}

// New creates a new gate Controller. The client may be nil initially and
// set later via SetClient once the MQTT connection is established.
func New(client mqtt.Client, hub *ws.Hub) *Controller {
	return &Controller{
		client: client,
		hub:    hub,
		status: make(map[int]string),
	}
}

// SetClient updates the MQTT client after a deferred connection.
func (c *Controller) SetClient(client mqtt.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = client
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

	holdStatus := string(msg.Payload())

	c.mu.Lock()
	old := c.status[gateID]
	changed := old != holdStatus
	c.status[gateID] = holdStatus
	c.mu.Unlock()

	slog.Debug("gate status received", "gate_id", gateID, "hold_status", holdStatus, "changed", changed)

	if changed {
		statuses := c.GetStatus()
		slog.Info("gate status changed", "gate_id", gateID, "hold_status", holdStatus)
		c.hub.BroadcastJSON(gateStatusMessage{
			Type: "gate_status",
			Data: statuses,
		})
	}
}

// GetStatus returns the cached status of all known gates.
func (c *Controller) GetStatus() []GateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var statuses []GateStatus
	for id, name := range gateNames {
		hs := c.status[id]
		if hs == "" {
			hs = "unknown"
		}
		statuses = append(statuses, GateStatus{
			Name:       name,
			ID:         id,
			HoldStatus: hs,
		})
	}
	return statuses
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
