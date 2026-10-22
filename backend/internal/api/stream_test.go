package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperbolic2346/gatehouse/internal/db"
)

func hlsGet(h *StreamHandler, path, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/stream/hls/"+path+"?"+query, nil)
	req.SetPathValue("path", path)
	rec := httptest.NewRecorder()
	h.ProxyHLS(rec, withUser(req, &db.User{ID: 1, Cameras: []string{"gate"}}))
	return rec
}

func TestProxyHLS(t *testing.T) {
	var gotPath, gotQuery string
	go2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write([]byte("#EXTM3U\nhls/playlist.m3u8?id=abc\n"))
	}))
	defer go2rtc.Close()
	h := &StreamHandler{Go2rtcURL: go2rtc.URL}

	rec := hlsGet(h, "stream.m3u8", "src=gate")
	if rec.Code != http.StatusOK || gotPath != "/api/stream.m3u8" || gotQuery != "src=gate" {
		t.Fatalf("entry playlist: code %d, go2rtc saw %s?%s", rec.Code, gotPath, gotQuery)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("content type = %q", ct)
	}

	if rec := hlsGet(h, "hls/segment.ts", "id=abc&n=3"); rec.Code != http.StatusOK || gotPath != "/api/hls/segment.ts" {
		t.Errorf("segment: code %d, go2rtc saw %s", rec.Code, gotPath)
	}

	gotPath = ""
	for name, tc := range map[string]struct {
		path, query string
		want        int
	}{
		"camera not permitted":  {"stream.m3u8", "src=driveway", http.StatusForbidden},
		"no camera":             {"stream.m3u8", "", http.StatusForbidden},
		"segment without id":    {"hls/segment.ts", "n=1", http.StatusBadRequest},
		"other go2rtc endpoint": {"streams", "", http.StatusNotFound},
		"path traversal":        {"../api/streams", "", http.StatusNotFound},
	} {
		if rec := hlsGet(h, tc.path, tc.query); rec.Code != tc.want {
			t.Errorf("%s: code %d, want %d", name, rec.Code, tc.want)
		}
	}
	if gotPath != "" {
		t.Errorf("rejected requests reached go2rtc: %s", gotPath)
	}
}
