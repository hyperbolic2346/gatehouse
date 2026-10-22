package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

var schedNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func scheduleReq(h *SchedulesHandler, user *db.User, gateID int, start, end time.Duration) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"gate_id":%d,"start":%q,"end":%q,"note":"UPS"}`, gateID,
		schedNow.Add(start).Format(time.RFC3339), schedNow.Add(end).Format(time.RFC3339))
	rec := httptest.NewRecorder()
	h.Create(rec, withUser(httptest.NewRequest(http.MethodPost, "/api/schedules", strings.NewReader(body)), user))
	return rec
}

func TestCreateScheduleValidation(t *testing.T) {
	d := newAPITestDB(t)
	nana := mustCreate(t, d, &db.User{Username: "nana", WilsonGate: true})
	h := &SchedulesHandler{DB: d, Controller: &fakeGates{}, Now: func() time.Time { return schedNow }}

	cases := []struct {
		name       string
		gate       int
		start, end time.Duration
		want       int
	}{
		{"ok", 0, time.Hour, 3 * time.Hour, http.StatusCreated},
		{"overlaps the first", 0, 2 * time.Hour, 4 * time.Hour, http.StatusConflict},
		{"touching is fine", 0, 3 * time.Hour, 4 * time.Hour, http.StatusCreated},
		{"no access to brigman", 1, time.Hour, 2 * time.Hour, http.StatusForbidden},
		{"end before start", 0, 6 * time.Hour, 5 * time.Hour, http.StatusBadRequest},
		{"already over", 0, -2 * time.Hour, -time.Hour, http.StatusBadRequest},
		{"too long", 0, 10 * time.Hour, 35 * time.Hour, http.StatusBadRequest},
		{"too far ahead", 0, 90 * 24 * time.Hour, 91 * 24 * time.Hour, http.StatusBadRequest},
		{"started already but still running is fine", 0, -time.Hour, 30 * time.Minute, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := scheduleReq(h, nana, tc.gate, tc.start, tc.end); rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCancelSchedulePermissions(t *testing.T) {
	d := newAPITestDB(t)
	nana := mustCreate(t, d, &db.User{Username: "nana", WilsonGate: true})
	bob := mustCreate(t, d, &db.User{Username: "bob", WilsonGate: true})
	admin, _ := d.GetUserByUsername("knobby")
	h := &SchedulesHandler{DB: d, Controller: &fakeGates{}, Now: func() time.Time { return schedNow }}

	var created scheduleResponse
	json.Unmarshal(scheduleReq(h, nana, 0, time.Hour, 2*time.Hour).Body.Bytes(), &created)

	cancel := func(u *db.User) int {
		req := httptest.NewRequest(http.MethodDelete, "/api/schedules/x", nil)
		req.SetPathValue("id", fmt.Sprint(created.ID))
		rec := httptest.NewRecorder()
		h.Cancel(rec, withUser(req, u))
		return rec.Code
	}

	// Bob sees it but cannot cancel it.
	rec := httptest.NewRecorder()
	h.List(rec, withUser(httptest.NewRequest(http.MethodGet, "/api/schedules", nil), bob))
	var list []scheduleResponse
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].CanCancel || list[0].Owner != "nana" {
		t.Fatalf("bob's list = %+v, want nana's hold, not cancellable", list)
	}
	if code := cancel(bob); code != http.StatusForbidden {
		t.Errorf("bob cancel = %d, want 403", code)
	}
	if code := cancel(admin); code != http.StatusNoContent {
		t.Errorf("admin cancel = %d, want 204", code)
	}
	if code := cancel(nana); code != http.StatusConflict {
		t.Errorf("cancel twice = %d, want 409", code)
	}
}

func TestCancelActiveScheduleReleasesOwnersHold(t *testing.T) {
	d := newAPITestDB(t)
	nana := mustCreate(t, d, &db.User{Username: "nana", WilsonGate: true})
	ctrl := &fakeGates{holderID: nana.ID}
	h := &SchedulesHandler{DB: d, Controller: ctrl, Now: func() time.Time { return schedNow }}

	s := &db.ScheduledHold{GateID: 0, UserID: nana.ID, Owner: "nana", Start: schedNow, End: schedNow.Add(time.Hour)}
	d.CreateSchedule(s)
	d.SetScheduleStatus(s.ID, db.ScheduleActive, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/schedules/x", nil)
	req.SetPathValue("id", fmt.Sprint(s.ID))
	rec := httptest.NewRecorder()
	h.Cancel(rec, withUser(req, nana))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(ctrl.sent) != 1 || ctrl.sent[0] != "release" {
		t.Errorf("sent = %v, want release", ctrl.sent)
	}
}
