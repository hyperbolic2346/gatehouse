package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/frigate"
)

func TestEventsListUsesLocalDayAndPagesBackwards(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	saved := time.Local
	time.Local = ny
	defer func() { time.Local = saved }()

	var got url.Values
	frig := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`[]`))
	}))
	defer frig.Close()
	h := &EventsHandler{Frigate: frigate.New(frig.URL)}
	user := &db.User{ID: 1, Cameras: []string{"gate"}}

	list := func(query string) int {
		rec := httptest.NewRecorder()
		h.List(rec, withUser(httptest.NewRequest(http.MethodGet, "/api/events?"+query, nil), user))
		return rec.Code
	}

	dayStart := time.Date(2026, 10, 22, 0, 0, 0, 0, ny)
	dayEnd := time.Date(2026, 10, 23, 0, 0, 0, 0, ny)

	if code := list("date=20261022"); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Get("after") != itoa(dayStart.Unix()) || got.Get("before") != itoa(dayEnd.Unix()) {
		t.Errorf("day window = %s..%s, want local midnight %d..%d", got.Get("after"), got.Get("before"), dayStart.Unix(), dayEnd.Unix())
	}

	// Next page: before the oldest event the client has.
	oldest := dayStart.Add(15 * time.Hour)
	list("date=20261022&before=" + itoa(oldest.Unix()) + ".75")
	if got.Get("after") != itoa(dayStart.Unix()) || got.Get("before") != itoa(oldest.Unix()) {
		t.Errorf("page window = %s..%s, want %d..%d", got.Get("after"), got.Get("before"), dayStart.Unix(), oldest.Unix())
	}

	// A before past the end of the day cannot widen the window.
	list("date=20261022&before=" + itoa(dayEnd.Add(48*time.Hour).Unix()))
	if got.Get("before") != itoa(dayEnd.Unix()) {
		t.Errorf("before = %s, want clamped to %d", got.Get("before"), dayEnd.Unix())
	}

	if code := list("date=20261022&before=soon"); code != http.StatusBadRequest {
		t.Errorf("bad before: status %d, want 400", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
