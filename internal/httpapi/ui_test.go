package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUIServing(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name        string
		method      string
		path        string
		status      int
		cache       string
		contentType string
		bodyHas     string
	}{
		{"root index", "GET", "/", 200, cacheNoCache, "text/html; charset=utf-8", "tailwatch ui"},
		{"explicit index", "GET", "/index.html", 200, cacheNoCache, "text/html; charset=utf-8", "tailwatch ui"},
		{"spa route", "GET", "/devices/laptop", 200, cacheNoCache, "text/html; charset=utf-8", "tailwatch ui"},
		{"spa route with dots", "GET", "/devices/laptop.tail.ts.net", 200, cacheNoCache, "text/html; charset=utf-8", "tailwatch ui"},
		{"spa deep route with query", "GET", "/alerts/rules?x=1", 200, cacheNoCache, "text/html; charset=utf-8", "tailwatch ui"},
		{"asset js", "GET", "/assets/app-abc.js", 200, cacheImmutable, "text/javascript; charset=utf-8", "console.log"},
		{"asset css", "GET", "/assets/app-abc.css", 200, cacheImmutable, "text/css; charset=utf-8", "body{}"},
		{"missing asset is 404", "GET", "/assets/missing.js", 404, "", "", ""},
		{"assets dir is not listed", "GET", "/assets/", 200, cacheNoCache, "text/html; charset=utf-8", "tailwatch ui"},
		{"favicon", "GET", "/favicon.svg", 200, cacheNoCache, "image/svg+xml", "<svg"},
		{"head index", "HEAD", "/", 200, cacheNoCache, "text/html; charset=utf-8", ""},
		{"post rejected", "POST", "/", 405, "", "", ""},
		{"traversal rejected", "GET", "/../etc/passwd", 400, "", "", ""},
		{"encoded traversal rejected", "GET", "/assets/%2e%2e/index.html", 400, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := h.do(tc.method, tc.path, nil, ipUnknown, noHdr(requestHeader))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d (%q)", w.Code, tc.status, w.Body.String())
			}
			if tc.cache != "" && w.Header().Get("Cache-Control") != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", w.Header().Get("Cache-Control"), tc.cache)
			}
			if tc.contentType != "" && w.Header().Get("Content-Type") != tc.contentType {
				t.Errorf("Content-Type = %q, want %q", w.Header().Get("Content-Type"), tc.contentType)
			}
			if tc.bodyHas != "" && !strings.Contains(w.Body.String(), tc.bodyHas) {
				t.Errorf("body %q lacks %q", w.Body.String(), tc.bodyHas)
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Error("HEAD returned a body")
			}
			if w.Header().Get("Content-Security-Policy") == "" {
				t.Error("CSP missing on UI response")
			}
		})
	}

	// Revalidation of no-cache files through ETag.
	first := h.do("GET", "/", nil, ipUnknown)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("index.html has no ETag")
	}
	again := h.do("GET", "/", nil, ipUnknown, hdr("If-None-Match", etag))
	if again.Code != http.StatusNotModified {
		t.Errorf("If-None-Match status = %d", again.Code)
	}
	if h.do("GET", "/assets/app-abc.js", nil, ipUnknown).Header().Get("Last-Modified") != "" {
		t.Error("embedded asset advertised a bogus Last-Modified")
	}
}

func TestUINotBuilt(t *testing.T) {
	for _, tc := range []struct {
		name string
		fsys fstest.MapFS
	}{
		{"nil fs", nil},
		{"fs without index", fstest.MapFS{".gitkeep": {Data: []byte{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var h *harness
			if tc.fsys == nil {
				h = newHarness(t, withUI(nil))
			} else {
				h = newHarness(t, withUI(tc.fsys))
			}
			for _, p := range []string{"/", "/devices/x", "/assets/app.js"} {
				w := h.do("GET", p, nil, ipUnknown)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "not built") || !strings.Contains(w.Body.String(), "make web") {
					t.Errorf("%s: %d %q", p, w.Code, w.Body.String())
				}
				if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
					t.Errorf("%s: Content-Type %q", p, ct)
				}
				if strings.Contains(w.Body.String(), "<script") || strings.Contains(w.Body.String(), "http://") && !strings.Contains(w.Body.String(), "www.w3.org") {
					t.Errorf("%s: placeholder page must be self-contained", p)
				}
			}
			// The API keeps working without a UI build.
			if w := h.do("GET", "/healthz", nil, ipUnknown); w.Code != 200 {
				t.Errorf("healthz = %d", w.Code)
			}
			if w := h.do("GET", "/api/v1/me", nil, ipViewer); w.Code != 200 {
				t.Errorf("me = %d", w.Code)
			}
		})
	}
}
