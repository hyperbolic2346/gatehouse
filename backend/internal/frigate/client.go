package frigate

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event represents a single Frigate detection event.
type Event struct {
	ID          string   `json:"id"`
	Camera      string   `json:"camera"`
	Label       string   `json:"label"`
	TopScore    float64  `json:"top_score"`
	StartTime   float64  `json:"start_time"`
	EndTime     *float64 `json:"end_time"`
	Thumbnail   string   `json:"thumbnail"`
	HasClip     bool     `json:"has_clip"`
	HasSnapshot bool     `json:"has_snapshot"`
}

// cameraCacheTTL is how long a discovered camera list is reused before
// re-querying Frigate's config endpoint.
const cameraCacheTTL = 60 * time.Second

// Client communicates with the Frigate NVR REST API. WebRTC signaling is
// proxied through Frigate's built-in go2rtc proxy at /api/go2rtc/webrtc.
type Client struct {
	baseURL    string
	httpClient *http.Client

	camMu       sync.Mutex
	camCache    []string
	camCachedAt time.Time
	now         func() time.Time
}

// New creates a Frigate API client. The baseURL should be the root URL of the
// Frigate instance (e.g. "http://frigate.home.svc.cluster.local:5000").
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		now: time.Now,
	}
}

// GetCameras returns the sorted list of camera names configured in Frigate,
// discovered from its /api/config endpoint. Results are cached briefly to
// avoid hammering Frigate when the admin UI or permission checks query it.
func (c *Client) GetCameras() ([]string, error) {
	c.camMu.Lock()
	defer c.camMu.Unlock()

	if c.camCache != nil && c.now().Sub(c.camCachedAt) < cameraCacheTTL {
		return c.camCache, nil
	}

	reqURL := fmt.Sprintf("%s/api/config", c.baseURL)
	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("frigate get config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("frigate get config: status %d: %s", resp.StatusCode, string(body))
	}

	var cfg struct {
		Cameras map[string]json.RawMessage `json:"cameras"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("frigate decode config: %w", err)
	}

	cameras := make([]string, 0, len(cfg.Cameras))
	for name := range cfg.Cameras {
		cameras = append(cameras, name)
	}
	sort.Strings(cameras)

	c.camCache = cameras
	c.camCachedAt = c.now()
	slog.Info("discovered frigate cameras", "count", len(cameras))
	return cameras, nil
}

// GetEvents retrieves detection events from Frigate, optionally filtered by
// camera and time range. Pass 0 for before or after to omit those filters.
func (c *Client) GetEvents(camera string, before, after int64) ([]Event, error) {
	params := url.Values{}
	if camera != "" {
		params.Set("cameras", camera)
	}
	if before > 0 {
		params.Set("before", fmt.Sprintf("%d", before))
	}
	if after > 0 {
		params.Set("after", fmt.Sprintf("%d", after))
	}
	params.Set("limit", "50")
	params.Set("include_thumbnails", "0")

	reqURL := fmt.Sprintf("%s/api/events?%s", c.baseURL, params.Encode())
	slog.Info("fetching frigate events", "url", reqURL)

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("frigate get events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("frigate get events: status %d: %s", resp.StatusCode, string(body))
	}

	var events []Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, fmt.Errorf("frigate decode events: %w", err)
	}

	slog.Info("fetched frigate events", "count", len(events))
	return events, nil
}

// GetThumbnail retrieves the JPEG thumbnail for the given event. It returns
// the raw image bytes and the Content-Type header from Frigate.
func (c *Client) GetThumbnail(eventID string) ([]byte, string, error) {
	reqURL := fmt.Sprintf("%s/api/events/%s/thumbnail.jpg", c.baseURL, url.PathEscape(eventID))

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, "", fmt.Errorf("frigate get thumbnail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("frigate get thumbnail: status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("frigate read thumbnail: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return data, contentType, nil
}

// GetClip retrieves the MP4 clip for the given event. The caller is
// responsible for closing the returned io.ReadCloser. It also returns the
// Content-Type header from Frigate.
func (c *Client) GetClip(eventID string) (io.ReadCloser, string, error) {
	reqURL := fmt.Sprintf("%s/api/events/%s/clip.mp4", c.baseURL, url.PathEscape(eventID))

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, "", fmt.Errorf("frigate get clip: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, "", fmt.Errorf("frigate get clip: status %d: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "video/mp4"
	}

	return resp.Body, contentType, nil
}

// DeleteEvent removes an event from Frigate by its ID.
func (c *Client) DeleteEvent(eventID string) error {
	reqURL := fmt.Sprintf("%s/api/events/%s", c.baseURL, url.PathEscape(eventID))

	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("frigate delete event: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("frigate delete event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("frigate delete event: status %d: %s", resp.StatusCode, string(body))
	}

	slog.Info("deleted frigate event", "event_id", eventID)
	return nil
}

// ProxyWebRTCOffer sends an SDP offer through Frigate's go2rtc proxy and
// returns the SDP answer. This is used for establishing WebRTC streams to
// view live camera feeds.
func (c *Client) ProxyWebRTCOffer(camera string, sdpOffer []byte) ([]byte, error) {
	params := url.Values{}
	params.Set("src", camera)
	reqURL := fmt.Sprintf("%s/api/go2rtc/webrtc?%s", c.baseURL, params.Encode())

	slog.Info("proxying webrtc offer", "url", reqURL, "camera", camera, "offer_len", len(sdpOffer))

	resp, err := c.httpClient.Post(reqURL, "application/sdp", strings.NewReader(string(sdpOffer)))
	if err != nil {
		return nil, fmt.Errorf("go2rtc webrtc offer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("go2rtc webrtc offer: status %d: %s", resp.StatusCode, string(body))
	}

	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("go2rtc read answer: %w", err)
	}

	slog.Info("webrtc answer received", "camera", camera, "status", resp.StatusCode, "answer_len", len(answer))
	return answer, nil
}
