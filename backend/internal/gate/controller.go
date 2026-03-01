package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/ws"
)

// gateNames maps hardware gate IDs to human-readable names.
var gateNames = map[int]string{
	0: "Wilson",
	1: "Brigman",
}

// rawGateStatus is the JSON structure returned by the Arduino HTTP API.
type rawGateStatus struct {
	Gate      int    `json:"gate"`
	State     string `json:"state"`
	HoldState string `json:"hold_state"`
}

// GateStatus represents the state of a single gate with its human-readable
// name and current position and hold information.
type GateStatus struct {
	Name      string `json:"name"`
	ID        int    `json:"id"`
	State     string `json:"state"`
	HoldState string `json:"hold_state"`
	Holder    string `json:"holder"`
}

// gateStatusMessage is broadcast over WebSocket when gate status changes.
type gateStatusMessage struct {
	Type string       `json:"type"`
	Data []GateStatus `json:"data"`
}

// Controller communicates with the Arduino gate controller over HTTP and
// tracks the last known status for change detection.
type Controller struct {
	addr       string
	httpClient *http.Client
	mu         sync.Mutex
	lastStatus []GateStatus
}

// New creates a new gate Controller. The addr should be the base URL of the
// Arduino HTTP server (e.g. "http://10.0.1.25").
func New(addr string) *Controller {
	return &Controller{
		addr: addr,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetStatus queries the Arduino for the current state of all gates.
func (c *Controller) GetStatus() ([]GateStatus, error) {
	reqURL := fmt.Sprintf("%s/gate", c.addr)

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("gate get status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gate get status: status %d: %s", resp.StatusCode, string(body))
	}

	var raw []rawGateStatus
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("gate decode status: %w", err)
	}

	return toGateStatuses(raw), nil
}

// Open sends a command to open the specified gate.
func (c *Controller) Open(gateID int) ([]GateStatus, error) {
	return c.sendCommand(gateID, "open")
}

// Hold sends a command to hold the specified gate in the open position.
func (c *Controller) Hold(gateID int) ([]GateStatus, error) {
	return c.sendCommand(gateID, "hold")
}

// Release sends a command to release the hold on the specified gate.
func (c *Controller) Release(gateID int) ([]GateStatus, error) {
	return c.sendCommand(gateID, "release")
}

// sendCommand sends a PUT command to the Arduino for the given gate and action.
func (c *Controller) sendCommand(gateID int, action string) ([]GateStatus, error) {
	reqURL := fmt.Sprintf("%s/gate/%d/%s", c.addr, gateID, action)

	req, err := http.NewRequest(http.MethodPut, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("gate %s: build request: %w", action, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gate %s: %w", action, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gate %s: status %d: %s", action, resp.StatusCode, string(body))
	}

	// The Arduino returns the updated status array after a command.
	var raw []rawGateStatus
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("gate %s decode response: %w", action, err)
	}

	slog.Info("gate command sent", "gate_id", gateID, "action", action)
	return toGateStatuses(raw), nil
}

// toGateStatuses converts raw Arduino responses to GateStatus values with
// human-readable names.
func toGateStatuses(raw []rawGateStatus) []GateStatus {
	statuses := make([]GateStatus, len(raw))
	for i, r := range raw {
		name, ok := gateNames[r.Gate]
		if !ok {
			name = fmt.Sprintf("Gate %d", r.Gate)
		}
		statuses[i] = GateStatus{
			Name:      name,
			ID:        r.Gate,
			State:     r.State,
			HoldState: r.HoldState,
		}
	}
	return statuses
}

// StartPolling begins polling the Arduino for gate status at the given
// interval. When a change is detected, it broadcasts the new status to all
// connected WebSocket clients. This method blocks until the context is
// cancelled.
func (c *Controller) StartPolling(ctx context.Context, hub *ws.Hub, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("gate polling started", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("gate polling stopped")
			return
		case <-ticker.C:
			statuses, err := c.GetStatus()
			if err != nil {
				slog.Error("gate poll failed", "error", err)
				continue
			}

			c.mu.Lock()
			changed := !reflect.DeepEqual(c.lastStatus, statuses)
			if changed {
				c.lastStatus = statuses
			}
			c.mu.Unlock()

			if changed {
				slog.Info("gate status changed", "statuses", statuses)
				hub.BroadcastJSON(gateStatusMessage{
					Type: "gate_status",
					Data: statuses,
				})
			}
		}
	}
}
