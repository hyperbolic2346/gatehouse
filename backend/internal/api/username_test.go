package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

func TestLoginIgnoresUsernameCase(t *testing.T) {
	d := newAPITestDB(t)
	hash, _ := hashForTest("rosebud")
	mustCreate(t, d, &db.User{Username: "nana", PasswordHash: hash})
	h := &AuthHandler{DB: d, JWTSecret: "test"}

	for _, typed := range []string{"nana", "Nana", "NANA", "Nana "} {
		if rec := login(h, typed, "rosebud"); rec.Code != http.StatusOK {
			t.Errorf("login as %q: status %d, want 200", typed, rec.Code)
		}
	}
	// Passwords stay case-sensitive.
	if rec := login(h, "Nana", "Rosebud"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong-case password: status %d, want 401", rec.Code)
	}
}

func TestCreateAndRenameRejectCaseClash(t *testing.T) {
	d := newAPITestDB(t)
	nana := mustCreate(t, d, &db.User{Username: "nana"})
	bob := mustCreate(t, d, &db.User{Username: "bob"})
	admin, _ := d.GetUserByUsername("knobby")
	h := &UsersHandler{DB: d, Frigate: nil}

	create := httptest.NewRecorder()
	h.Create(create, withUser(httptest.NewRequest(http.MethodPost, "/api/users",
		strings.NewReader(`{"username":" Nana ","password":"pw","cameras":[]}`)), admin))
	if create.Code != http.StatusConflict || !strings.Contains(create.Body.String(), "nana") {
		t.Errorf("create Nana: %d %s, want 409 naming nana", create.Code, create.Body.String())
	}

	rename := func(u *db.User, name string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/users/x", strings.NewReader(`{"username":"`+name+`"}`))
		req.SetPathValue("id", itoa(u.ID))
		rec := httptest.NewRecorder()
		h.Update(rec, withUser(req, admin))
		return rec.Code
	}
	if code := rename(bob, "NANA"); code != http.StatusConflict {
		t.Errorf("rename bob to NANA: %d, want 409", code)
	}
	if code := rename(nana, "Nana"); code != http.StatusOK {
		t.Errorf("recase own name: %d, want 200", code)
	}
}
