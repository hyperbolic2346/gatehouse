package api

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/frigate"
)

// EventsHandler provides HTTP handlers for Frigate event operations.
type EventsHandler struct {
	Frigate *frigate.Client
}

// List handles GET /api/events?date=20260301&camera=gate
// It retrieves detection events from Frigate for the specified date and camera.
// If no date is provided, today is used.
func (h *EventsHandler) List(w http.ResponseWriter, r *http.Request) {
	camera := r.URL.Query().Get("camera")
	dateStr := r.URL.Query().Get("date")

	var after, before int64

	if dateStr != "" {
		t, err := time.Parse("20060102", dateStr)
		if err != nil {
			writeJSONError(w, "invalid date format, use YYYYMMDD", http.StatusBadRequest)
			return
		}
		after = t.Unix()
		before = t.Add(24 * time.Hour).Unix()
	} else {
		// Default to today in local time.
		now := time.Now()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		after = startOfDay.Unix()
		before = startOfDay.Add(24 * time.Hour).Unix()
	}

	events, err := h.Frigate.GetEvents(camera, before, after)
	if err != nil {
		slog.Error("failed to fetch events from frigate", "error", err)
		writeJSONError(w, "failed to fetch events", http.StatusBadGateway)
		return
	}

	// Return an empty array instead of null when there are no events.
	if events == nil {
		events = []frigate.Event{}
	}

	writeJSON(w, events, http.StatusOK)
}

// Delete handles DELETE /api/events/{id}
// It removes an event from Frigate. Only admin users may delete events.
func (h *EventsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	eventID := r.PathValue("id")
	if eventID == "" {
		writeJSONError(w, "missing event id", http.StatusBadRequest)
		return
	}

	if err := h.Frigate.DeleteEvent(eventID); err != nil {
		slog.Error("failed to delete event", "event_id", eventID, "error", err)
		writeJSONError(w, "failed to delete event", http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Thumbnail handles GET /api/events/{id}/thumbnail.jpg
// It proxies the thumbnail image from Frigate back to the client.
func (h *EventsHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	if eventID == "" {
		writeJSONError(w, "missing event id", http.StatusBadRequest)
		return
	}

	data, contentType, err := h.Frigate.GetThumbnail(eventID)
	if err != nil {
		slog.Error("failed to fetch thumbnail", "event_id", eventID, "error", err)
		writeJSONError(w, "failed to fetch thumbnail", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		slog.Error("failed to write thumbnail response", "error", err)
	}
}

// Clip handles GET /api/events/{id}/clip.mp4
// It streams the video clip from Frigate back to the client.
func (h *EventsHandler) Clip(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	if eventID == "" {
		writeJSONError(w, "missing event id", http.StatusBadRequest)
		return
	}

	body, contentType, err := h.Frigate.GetClip(eventID)
	if err != nil {
		slog.Error("failed to fetch clip", "event_id", eventID, "error", err)
		writeJSONError(w, "failed to fetch clip", http.StatusBadGateway)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, body); err != nil {
		slog.Error("failed to stream clip response", "error", err)
	}
}

// WebRTCOffer handles POST /api/webrtc/offer?camera=gate
// It proxies an SDP offer to the go2rtc instance and returns the SDP answer.
func (h *EventsHandler) WebRTCOffer(w http.ResponseWriter, r *http.Request) {
	camera := r.URL.Query().Get("camera")
	if camera == "" {
		writeJSONError(w, "missing camera parameter", http.StatusBadRequest)
		return
	}

	sdpOffer, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webrtc offer body", "error", err)
		writeJSONError(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(sdpOffer) == 0 {
		writeJSONError(w, "empty sdp offer", http.StatusBadRequest)
		return
	}

	answer, err := h.Frigate.ProxyWebRTCOffer(camera, sdpOffer)
	if err != nil {
		slog.Error("webrtc offer proxy failed", "camera", camera, "error", err)
		writeJSONError(w, "failed to proxy webrtc offer", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(answer); err != nil {
		slog.Error("failed to write webrtc answer", "error", err)
	}
}
