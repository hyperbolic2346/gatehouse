package api

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperbolic2346/gatehouse/internal/auth"
	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
)

// fakeGates is a GateController whose gate 0 is held by holderID (0 = not
// held) and which records the commands it is sent.
type fakeGates struct {
	holderID int64
	sent     []string
}

func (f *fakeGates) GetStatus() []gate.GateStatus {
	s := gate.GateStatus{Name: "Wilson", ID: 0, HoldStatus: "Not Held"}
	if f.holderID != 0 {
		id := f.holderID
		s.HoldStatus, s.Held, s.HeldByUserID = "Held By Us", true, &id
	}
	return []gate.GateStatus{s, {Name: "Brigman", ID: 1, HoldStatus: "Not Held"}}
}

func (f *fakeGates) Status(id int) (gate.GateStatus, bool) {
	for _, s := range f.GetStatus() {
		if s.ID == id {
			return s, true
		}
	}
	return gate.GateStatus{}, false
}

func (f *fakeGates) BroadcastStatus()  {}
func (f *fakeGates) Open(int) error    { f.sent = append(f.sent, "open"); return nil }
func (f *fakeGates) Hold(int) error    { f.sent = append(f.sent, "hold"); return nil }
func (f *fakeGates) Release(int) error { f.sent = append(f.sent, "release"); return nil }

func mustCreate(t *testing.T, d *db.DB, u *db.User) *db.User {
	t.Helper()
	if u.PasswordHash == "" {
		u.PasswordHash = "x"
	}
	if u.Role == "" {
		u.Role = "user"
	}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	loaded, err := d.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func gatePost(h *GatesHandler, action string, user *db.User) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/gates/0/"+action, nil)
	req.SetPathValue("id", "0")
	rec := httptest.NewRecorder()
	handler := map[string]http.HandlerFunc{"open": h.Open, "hold": h.Hold, "release": h.Release}[action]
	handler(rec, withUser(req, user))
	return rec
}

func TestHoldPriority(t *testing.T) {
	d := newAPITestDB(t)
	senior := mustCreate(t, d, &db.User{Username: "senior", DisplayName: "Mrs. Senior", WilsonGate: true, HoldPriority: 1})
	junior := mustCreate(t, d, &db.User{Username: "junior", WilsonGate: true})
	peer := mustCreate(t, d, &db.User{Username: "peer", WilsonGate: true})
	admin := mustCreate(t, d, &db.User{Username: "boss", Role: "admin"})

	cases := []struct {
		name     string
		holder   *db.User
		actor    *db.User
		action   string
		wantCode int
	}{
		{"junior cannot release senior's hold", senior, junior, "release", http.StatusConflict},
		{"junior cannot take over senior's hold", senior, junior, "hold", http.StatusConflict},
		{"junior can still open while senior holds", senior, junior, "open", http.StatusOK},
		{"senior can release own hold", senior, senior, "release", http.StatusOK},
		{"senior can release junior's hold", junior, senior, "release", http.StatusOK},
		{"equal priority can release", junior, peer, "release", http.StatusOK},
		{"admin can release anyone's hold", senior, admin, "release", http.StatusOK},
		{"senior cannot release admin's hold", admin, senior, "release", http.StatusConflict},
		{"anyone can release an outside hold", nil, junior, "release", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := &fakeGates{}
			if tc.holder != nil {
				ctrl.holderID = tc.holder.ID
			}
			h := &GatesHandler{Controller: ctrl, DB: d}
			rec := gatePost(h, tc.action, tc.actor)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode == http.StatusConflict {
				if len(ctrl.sent) != 0 {
					t.Errorf("denied action still sent %v", ctrl.sent)
				}
				if !strings.Contains(rec.Body.String(), tc.holder.Name()) {
					t.Errorf("error %q should name the holder %q", rec.Body.String(), tc.holder.Name())
				}
			} else if len(ctrl.sent) != 1 || ctrl.sent[0] != tc.action {
				t.Errorf("sent = %v, want [%s]", ctrl.sent, tc.action)
			}
		})
	}
}

func TestGateActionsAreLogged(t *testing.T) {
	d := newAPITestDB(t)
	senior := mustCreate(t, d, &db.User{Username: "senior", WilsonGate: true, HoldPriority: 1})
	junior := mustCreate(t, d, &db.User{Username: "junior", DisplayName: "Junior", WilsonGate: true})

	gatePost(&GatesHandler{Controller: &fakeGates{}, DB: d}, "open", junior)
	gatePost(&GatesHandler{Controller: &fakeGates{holderID: senior.ID}, DB: d}, "release", junior)

	rows, err := d.ListActivity(0, 10, db.GateID(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("activity = %+v, want 2 rows", rows)
	}
	if rows[1].Action != db.ActionOpen || rows[1].Actor != "Junior" {
		t.Errorf("first row = %+v, want open by Junior", rows[1])
	}
	if rows[0].Action != db.ActionDenied || !strings.HasPrefix(rows[0].Detail, "release") {
		t.Errorf("second row = %+v, want denied release", rows[0])
	}
}

func TestGateActionForbiddenWithoutPermission(t *testing.T) {
	d := newAPITestDB(t)
	brigmanOnly := mustCreate(t, d, &db.User{Username: "b", BrigmanGate: true})
	ctrl := &fakeGates{}
	rec := gatePost(&GatesHandler{Controller: ctrl, DB: d}, "open", brigmanOnly)
	if rec.Code != http.StatusForbidden || len(ctrl.sent) != 0 {
		t.Errorf("status = %d sent = %v, want 403 and nothing sent", rec.Code, ctrl.sent)
	}
}

func login(h *AuthHandler, username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body))))
	return rec
}

func md5Of(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestLegacyPasswordLoginUpgradesToBcrypt(t *testing.T) {
	d := newAPITestDB(t)
	u := mustCreate(t, d, &db.User{Username: "nana", PasswordHash: db.LegacyMD5Prefix + md5Of("rosebud")})
	h := &AuthHandler{DB: d, JWTSecret: "test"}

	if rec := login(h, "nana", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status = %d, want 401", rec.Code)
	}
	if after, _ := d.GetUserByID(u.ID); !after.HasLegacyPassword() {
		t.Fatal("failed login must not touch the stored hash")
	}

	if rec := login(h, "nana", "rosebud"); rec.Code != http.StatusOK {
		t.Fatalf("legacy login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	after, _ := d.GetUserByID(u.ID)
	if after.HasLegacyPassword() || !strings.HasPrefix(after.PasswordHash, "$2") {
		t.Fatalf("hash after login = %q, want bcrypt", after.PasswordHash)
	}

	// The upgraded hash keeps working, and the old md5 itself is no longer
	// accepted as a password.
	if rec := login(h, "nana", "rosebud"); rec.Code != http.StatusOK {
		t.Errorf("second login: status = %d, want 200", rec.Code)
	}
	if rec := login(h, "nana", md5Of("rosebud")); rec.Code != http.StatusUnauthorized {
		t.Errorf("md5 as password: status = %d, want 401", rec.Code)
	}

	rows, _ := d.ListActivity(0, 10, nil)
	var logins, failures int
	for _, r := range rows {
		switch r.Action {
		case db.ActionLogin:
			logins++
		case db.ActionLoginFailed:
			failures++
		}
	}
	if logins != 2 || failures != 2 {
		t.Errorf("logged %d logins and %d failures, want 2 and 2", logins, failures)
	}
}

func TestLoginCookieSecureBehindHTTPSGateway(t *testing.T) {
	d := newAPITestDB(t)
	hash, _ := hashForTest("pw")
	mustCreate(t, d, &db.User{Username: "u", PasswordHash: hash})
	h := &AuthHandler{DB: d, JWTSecret: "test"}

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"u","password":"pw"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	if c := rec.Result().Cookies(); len(c) != 1 || !c[0].Secure {
		t.Errorf("cookies = %+v, want one Secure cookie", c)
	}

	// Plain-HTTP local dev keeps working without Secure.
	if c := login(h, "u", "pw").Result().Cookies(); len(c) != 1 || c[0].Secure {
		t.Errorf("cookies over http = %+v, want one non-Secure cookie", c)
	}
}

func TestImportLegacyUsers(t *testing.T) {
	d := newAPITestDB(t)

	tsv := strings.Join([]string{
		"username\tpassword\tdisplay_name\taccess_level\toperate_wilson_gate\toperate_brigman_gate",
		"knobby\t" + md5Of("x") + "\tMike\t0\t1\t1",
		"nana\t" + strings.ToUpper(md5Of("rosebud")) + "\tNana\t0\t1\t0",
		"neighbor\t" + md5Of("pw") + "\tNULL\t1\t0\t1",
		"broken\tnot-a-hash\tB\t1\t1\t1",
		"",
	}, "\n")
	body, _ := json.Marshal(map[string]string{"tsv": tsv})
	admin, _ := d.GetUserByUsername("knobby")
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/users/import", strings.NewReader(string(body))), admin)
	rec := httptest.NewRecorder()
	(&UsersHandler{DB: d}).Import(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Imported []userResponse `json:"imported"`
		Skipped  []importSkip   `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Imported) != 2 || len(resp.Skipped) != 2 {
		t.Fatalf("imported %d, skipped %+v; want 2 imported, knobby+broken skipped", len(resp.Imported), resp.Skipped)
	}

	nana, err := d.GetUserByUsername("nana")
	if err != nil {
		t.Fatal(err)
	}
	if nana.Role != "user" || nana.DisplayName != "Nana" || !nana.WilsonGate || nana.BrigmanGate || nana.HoldPriority != 1 {
		t.Errorf("nana = %+v, want user, Nana, wilson only, priority 1", nana)
	}
	neighbor, _ := d.GetUserByUsername("neighbor")
	if neighbor.DisplayName != "" || neighbor.WilsonGate || !neighbor.BrigmanGate || neighbor.HoldPriority != 0 {
		t.Errorf("neighbor = %+v, want no display name, brigman only, priority 0", neighbor)
	}
	if len(neighbor.Cameras) == 0 {
		t.Error("imported users should get the default cameras")
	}
	if admin2, _ := d.GetUserByUsername("knobby"); admin2.PasswordHash != admin.PasswordHash {
		t.Error("existing user must not be overwritten")
	}

	// And the imported password works.
	if rec := login(&AuthHandler{DB: d, JWTSecret: "t"}, "nana", "rosebud"); rec.Code != http.StatusOK {
		t.Errorf("imported user login: status = %d", rec.Code)
	}
}

func hashForTest(pw string) (string, error) { return auth.HashPassword(pw) }
