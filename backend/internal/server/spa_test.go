package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	os.MkdirAll(filepath.Join(dir, "_app", "immutable"), 0o755)
	os.WriteFile(filepath.Join(dir, "_app", "immutable", "app.js"), []byte("js"), 0o644)
	os.WriteFile(filepath.Join(dir, "robots.txt"), []byte("robots"), 0o644)
	h := spaHandler(dir)

	cases := []struct {
		uri      string
		wantCode int
		wantLoc  string
		wantBody string
	}{
		{"/", 200, "", "app"},
		{"/login", 200, "", "app"},
		{"/admin", 200, "", "app"},
		{"/robots.txt", 200, "", "robots"},
		{"/_app/immutable/app.js", 200, "", "js"},
		// Old PHP site bookmarks and other unknown pages go home.
		{"/index.php", 302, "/", ""},
		{"/index.php?view_date=20261020", 302, "/", ""},
		{"/ajax_list.php?last_update=1", 302, "/", ""},
		{"/media/2026/10/20/clip.webm", 302, "/", ""},
		{"/templates/login.tpl", 302, "/", ""},
		{"/somewhere", 302, "/", ""},
		// A directory is not a page.
		{"/_app/", 404, "", ""},
		{"/_app/immutable/", 404, "", ""},
		// Stale build assets and unknown API paths are real 404s.
		{"/_app/immutable/old-chunk.js", 404, "", ""},
		{"/api/nope", 404, "", ""},
		// Traversal cannot escape dir.
		{"/../../etc/passwd", 302, "/", ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.uri, nil))
		if rec.Code != tc.wantCode {
			t.Errorf("%s: code %d, want %d", tc.uri, rec.Code, tc.wantCode)
			continue
		}
		if loc := rec.Header().Get("Location"); loc != tc.wantLoc {
			t.Errorf("%s: Location %q, want %q", tc.uri, loc, tc.wantLoc)
		}
		if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Errorf("%s: body %q, want it to contain %q", tc.uri, rec.Body.String(), tc.wantBody)
		}
	}
}
