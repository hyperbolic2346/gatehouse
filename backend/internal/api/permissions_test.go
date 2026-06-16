package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/frigate"
)

// withUser returns a request carrying the given user in its context, as the
// auth middleware would set it.
func withUser(req *http.Request, user *db.User) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), userContextKey, user))
}

func TestEventsListIntersectsRequestedWithPermitted(t *testing.T) {
	var gotCameras string
	called := false
	frig := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotCameras = r.URL.Query().Get("cameras")
		w.Write([]byte(`[]`))
	}))
	defer frig.Close()

	h := &EventsHandler{Frigate: frigate.New(frig.URL)}
	user := &db.User{ID: 1, Cameras: []string{"gate"}}

	// Requests gate (allowed) + driveway (not allowed) → only gate reaches Frigate.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/events?camera=gate,driveway", nil), user)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if !called {
		t.Fatal("expected Frigate to be queried")
	}
	if gotCameras != "gate" {
		t.Errorf("cameras sent to Frigate = %q, want %q", gotCameras, "gate")
	}
}

func TestEventsListDefaultsToAllPermitted(t *testing.T) {
	var gotCameras string
	frig := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCameras = r.URL.Query().Get("cameras")
		w.Write([]byte(`[]`))
	}))
	defer frig.Close()

	h := &EventsHandler{Frigate: frigate.New(frig.URL)}
	user := &db.User{ID: 1, Cameras: []string{"gate", "gate-rear"}}

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/events", nil), user)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	got := strings.Split(gotCameras, ",")
	sort.Strings(got)
	if want := []string{"gate", "gate-rear"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cameras sent to Frigate = %v, want %v", got, want)
	}
}

func TestEventsListNoPermittedCamerasReturnsEmptyWithoutQueryingFrigate(t *testing.T) {
	called := false
	frig := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`[]`))
	}))
	defer frig.Close()

	h := &EventsHandler{Frigate: frigate.New(frig.URL)}
	user := &db.User{ID: 1, Cameras: []string{"gate"}}

	// Requests only a camera the user is not permitted to view.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/events?camera=driveway", nil), user)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if called {
		t.Error("Frigate should not be queried when no requested camera is permitted")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var events []frigate.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %v, want empty", events)
	}
}

func TestWebRTCOfferRejectsUnpermittedCamera(t *testing.T) {
	h := &EventsHandler{Frigate: frigate.New("http://unused")}
	user := &db.User{ID: 1, Cameras: []string{"gate"}}

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/webrtc/offer?camera=driveway", strings.NewReader("v=0")), user)
	rec := httptest.NewRecorder()
	h.WebRTCOffer(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for unpermitted camera", rec.Code)
	}
}

func newAPITestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.New(filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestUpdateCameraVisibilityHidesPermittedCamera(t *testing.T) {
	d := newAPITestDB(t)
	u := &db.User{Username: "alice", PasswordHash: "x", Role: "user", Cameras: []string{"gate", "gate-rear"}}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	loaded, _ := d.GetUserByID(u.ID)

	h := &AuthHandler{DB: d}
	req := withUser(httptest.NewRequest(http.MethodPut, "/api/me/cameras", strings.NewReader(`{"hidden":["gate"]}`)), loaded)
	rec := httptest.NewRecorder()
	h.UpdateCameraVisibility(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	hidden, _ := d.GetUserHiddenCameras(u.ID)
	if len(hidden) != 1 || hidden[0] != "gate" {
		t.Errorf("hidden = %v, want [gate]", hidden)
	}
}

func TestUpdateCameraVisibilityRejectsUnpermittedCamera(t *testing.T) {
	d := newAPITestDB(t)
	u := &db.User{Username: "bob", PasswordHash: "x", Role: "user", Cameras: []string{"gate"}}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	loaded, _ := d.GetUserByID(u.ID)

	h := &AuthHandler{DB: d}
	req := withUser(httptest.NewRequest(http.MethodPut, "/api/me/cameras", strings.NewReader(`{"hidden":["driveway"]}`)), loaded)
	rec := httptest.NewRecorder()
	h.UpdateCameraVisibility(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for hiding unpermitted camera", rec.Code)
	}
	hidden, _ := d.GetUserHiddenCameras(u.ID)
	if len(hidden) != 0 {
		t.Errorf("hidden = %v, want empty (request rejected)", hidden)
	}
}
