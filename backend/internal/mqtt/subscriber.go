package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/hyperbolic2346/gatehouse/internal/frigate"
	"github.com/hyperbolic2346/gatehouse/internal/ws"
)

const (
	// frigateEventsTopic is the MQTT topic that Frigate publishes detection
	// events to.
	frigateEventsTopic = "frigate/events"

	// reconnectDelay is how long to wait before attempting to reconnect to
	// the MQTT broker.
	reconnectDelay = 5 * time.Second
)

// frigatePayload represents the JSON structure that Frigate publishes to its
// MQTT events topic.
type frigatePayload struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
	Type   string          `json:"type"`
}

// eventMessage is the WebSocket message structure broadcast to connected
// clients when a Frigate event is received.
type eventMessage struct {
	Type string        `json:"type"`
	Data frigate.Event `json:"data"`
}

// Subscriber listens to MQTT messages from Frigate and broadcasts them to
// connected WebSocket clients.
type Subscriber struct {
	broker        string
	hub           *ws.Hub
	frigateClient *frigate.Client
}

// New creates a new MQTT subscriber. The broker should be a URL like
// "tcp://10.0.1.20:1883".
func New(broker string, hub *ws.Hub, frigateClient *frigate.Client) *Subscriber {
	return &Subscriber{
		broker:        broker,
		hub:           hub,
		frigateClient: frigateClient,
	}
}

// Start connects to the MQTT broker and subscribes to Frigate event topics.
// It blocks until the provided context is cancelled.
func (s *Subscriber) Start(ctx context.Context) error {
	opts := mqtt.NewClientOptions().
		AddBroker(s.broker).
		SetClientID("gatehouse").
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(reconnectDelay).
		SetOnConnectHandler(func(c mqtt.Client) {
			slog.Info("mqtt connected", "broker", s.broker)
			s.subscribe(c)
		}).
		SetConnectionLostHandler(func(c mqtt.Client, err error) {
			slog.Warn("mqtt connection lost", "error", err)
		})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}

	slog.Info("mqtt subscriber started", "broker", s.broker)

	// Block until the context is done, then disconnect gracefully.
	<-ctx.Done()
	slog.Info("mqtt subscriber shutting down")
	client.Disconnect(250)
	return nil
}

// subscribe sets up the topic subscriptions. It is called on initial connect
// and on every reconnect.
func (s *Subscriber) subscribe(client mqtt.Client) {
	token := client.Subscribe(frigateEventsTopic, 1, s.handleMessage)
	token.Wait()
	if err := token.Error(); err != nil {
		slog.Error("mqtt subscribe failed", "topic", frigateEventsTopic, "error", err)
		return
	}
	slog.Info("mqtt subscribed", "topic", frigateEventsTopic)
}

// handleMessage processes a single MQTT message from the frigate/events topic.
func (s *Subscriber) handleMessage(_ mqtt.Client, msg mqtt.Message) {
	slog.Debug("mqtt message received", "topic", msg.Topic(), "payload_size", len(msg.Payload()))

	var payload frigatePayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		slog.Error("mqtt failed to parse frigate payload", "error", err)
		return
	}

	// Use the "after" object which contains the latest state of the event.
	// Fall back to "before" if "after" is null.
	eventData := payload.After
	if len(eventData) == 0 || string(eventData) == "null" {
		eventData = payload.Before
	}
	if len(eventData) == 0 || string(eventData) == "null" {
		slog.Warn("mqtt frigate event has no before or after data")
		return
	}

	var event frigate.Event
	if err := json.Unmarshal(eventData, &event); err != nil {
		slog.Error("mqtt failed to parse frigate event", "error", err)
		return
	}

	// Map the Frigate event type to our WebSocket message type.
	var msgType string
	switch payload.Type {
	case "new":
		msgType = "new_event"
	case "update":
		msgType = "event_update"
	case "end":
		msgType = "event_end"
	default:
		msgType = "event_update"
	}

	wsMsg := eventMessage{
		Type: msgType,
		Data: event,
	}

	slog.Debug("broadcasting frigate event",
		"message_type", msgType,
		"event_id", event.ID,
		"camera", event.Camera,
		"label", event.Label,
	)

	s.hub.BroadcastJSON(wsMsg)
}
