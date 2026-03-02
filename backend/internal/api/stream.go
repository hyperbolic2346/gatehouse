package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// StreamHandler proxies WebSocket connections to go2rtc for MSE (Media Source
// Extensions) live streaming. This avoids the need for direct WebRTC UDP
// connectivity between the browser and go2rtc.
type StreamHandler struct {
	Go2rtcURL string // e.g. "http://frigate.home.svc.cluster.local:1984"
}

var streamUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ProxyMSE handles GET /api/stream/mse?camera=gate
// It upgrades the client connection to WebSocket, then opens a second
// WebSocket to go2rtc's /api/ws?src={camera} and relays all messages
// bidirectionally. This lets the browser receive MSE video over the
// existing HTTPS connection without needing exposed UDP ports.
func (h *StreamHandler) ProxyMSE(w http.ResponseWriter, r *http.Request) {
	camera := r.URL.Query().Get("camera")
	if camera == "" {
		http.Error(w, "missing camera parameter", http.StatusBadRequest)
		return
	}

	// Upgrade client connection to WebSocket.
	clientConn, err := streamUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("stream: client websocket upgrade failed", "error", err)
		return
	}
	defer clientConn.Close()

	// Build go2rtc WebSocket URL.
	go2rtcURL, err := buildGo2rtcWSURL(h.Go2rtcURL, camera)
	if err != nil {
		slog.Error("stream: invalid frigate url", "error", err)
		clientConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "invalid backend url"))
		return
	}

	slog.Info("stream: connecting to go2rtc", "url", go2rtcURL, "camera", camera)

	// Connect to go2rtc.
	backendConn, resp, err := websocket.DefaultDialer.Dial(go2rtcURL, nil)
	if err != nil {
		body := ""
		if resp != nil && resp.Body != nil {
			b, _ := io.ReadAll(resp.Body)
			body = string(b)
			resp.Body.Close()
		}
		slog.Error("stream: go2rtc websocket dial failed", "error", err, "body", body)
		clientConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "backend connection failed"))
		return
	}
	defer backendConn.Close()

	slog.Info("stream: proxy established", "camera", camera)

	done := make(chan struct{})

	// Relay: go2rtc → client
	go func() {
		defer close(done)
		for {
			msgType, msg, err := backendConn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					slog.Debug("stream: backend read error", "error", err)
				}
				return
			}
			if err := clientConn.WriteMessage(msgType, msg); err != nil {
				slog.Debug("stream: client write error", "error", err)
				return
			}
		}
	}()

	// Relay: client → go2rtc
	go func() {
		for {
			msgType, msg, err := clientConn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					slog.Debug("stream: client read error", "error", err)
				}
				backendConn.Close()
				return
			}
			if err := backendConn.WriteMessage(msgType, msg); err != nil {
				slog.Debug("stream: backend write error", "error", err)
				return
			}
		}
	}()

	<-done
}

// buildGo2rtcWSURL converts a go2rtc HTTP base URL to a WebSocket URL.
// e.g. "http://frigate:1984" → "ws://frigate:1984/api/ws?src=gate"
func buildGo2rtcWSURL(baseURL, camera string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}

	params := url.Values{}
	params.Set("src", camera)

	u.Path = strings.TrimRight(u.Path, "/") + "/api/ws"
	u.RawQuery = params.Encode()

	return u.String(), nil
}
