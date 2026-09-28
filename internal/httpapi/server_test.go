package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

func TestNewServerValidatesDeps(t *testing.T) {
	h := newHarness(t)
	base := Deps{Cfg: h.cfg, Store: h.st, Collector: h.col, Alerts: h.eng, Local: h.local, Log: quietLogger()}
	tests := []struct {
		name   string
		mutate func(d *Deps)
		wantOK bool
	}{
		{"complete", func(*Deps) {}, true},
		{"missing cfg", func(d *Deps) { d.Cfg = nil }, false},
		{"missing store", func(d *Deps) { d.Store = nil }, false},
		{"missing collector", func(d *Deps) { d.Collector = nil }, false},
		{"missing alerts", func(d *Deps) { d.Alerts = nil }, false},
		{"missing local without auth", func(d *Deps) { d.Local = nil }, false},
		{"missing local with demo", func(d *Deps) {
			cfg := *h.cfg
			cfg.Demo = true
			d.Cfg = &cfg
			d.Local = nil
		}, true},
		{"missing local with injected auth", func(d *Deps) { d.Local = nil; d.Auth = &fakeAuth{} }, true},
		{"insecure-no-auth on tailscale ip", func(d *Deps) {
			cfg := *h.cfg
			cfg.InsecureNoAuth = true
			d.Cfg = &cfg
		}, false},
		{"insecure-no-auth on loopback", func(d *Deps) {
			cfg := *h.cfg
			cfg.InsecureNoAuth = true
			cfg.Listen = "127.0.0.1:8484"
			d.Cfg = &cfg
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := base
			tc.mutate(&d)
			_, err := NewServer(d)
			if (err == nil) != tc.wantOK {
				t.Fatalf("NewServer err = %v, wantOK %v", err, tc.wantOK)
			}
		})
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newHarness(t)
	want := map[string]string{
		"Content-Security-Policy":      contentSecurityPolicy,
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
	}
	paths := []struct {
		method, path, ip string
		cache            string
	}{
		{"GET", "/healthz", ipUnknown, "no-store"},
		{"GET", "/", ipUnknown, "no-cache"},
		{"GET", "/assets/app-abc.js", ipUnknown, cacheImmutable},
		{"GET", "/api/v1/me", ipViewer, "no-store"},
		{"GET", "/api/v1/me", ipUnknown, "no-store"}, // 401 still carries headers
		{"GET", "/api/v1/nope", ipViewer, "no-store"},
		{"POST", "/api/v1/refresh", ipViewer, "no-store"}, // 403
	}
	for _, p := range paths {
		w := h.do(p.method, p.path, nil, p.ip)
		for k, v := range want {
			if got := w.Header().Get(k); got != v {
				t.Errorf("%s %s: header %s = %q, want %q", p.method, p.path, k, got, v)
			}
		}
		if got := w.Header().Get("Cache-Control"); got != p.cache {
			t.Errorf("%s %s: Cache-Control = %q, want %q", p.method, p.path, got, p.cache)
		}
		if w.Header().Get("Strict-Transport-Security") != "" {
			t.Errorf("%s %s: HSTS set without TLS", p.method, p.path)
		}
		if w.Header().Get("Server") != "" || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s %s: unexpected Server/CORS header", p.method, p.path)
		}
	}

	// HSTS only when TLS is configured.
	h.srv.tls = true
	if got := h.do("GET", "/healthz", nil, ipUnknown).Header().Get("Strict-Transport-Security"); got != strictTransportSecurity {
		t.Errorf("HSTS = %q with TLS", got)
	}
}

func TestBrowserHardening(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		method string
		path   string
		opts   []reqOpt
		status int
	}{
		{"cross-site GET", "GET", "/api/v1/me", []reqOpt{hdr("Sec-Fetch-Site", "cross-site")}, 403},
		{"cross-site POST", "POST", "/api/v1/refresh", []reqOpt{hdr("Sec-Fetch-Site", "Cross-Site")}, 403},
		{"same-origin fetch ok", "GET", "/api/v1/me", []reqOpt{hdr("Sec-Fetch-Site", "same-origin")}, 200},
		{"origin mismatch", "GET", "/api/v1/me", []reqOpt{hdr("Origin", "https://evil.example")}, 403},
		{"origin null", "POST", "/api/v1/refresh", []reqOpt{hdr("Origin", "null")}, 403},
		{"origin garbage", "GET", "/api/v1/me", []reqOpt{hdr("Origin", "::not a url")}, 403},
		{"origin matches host", "GET", "/api/v1/me", []reqOpt{hdr("Origin", "http://"+hostHeader)}, 200},
		{"origin matches host case-insensitively", "GET", "/api/v1/me", []reqOpt{hdr("Origin", "http://HUB.tail.ts.net:8484")}, 200},
		{"origin default port normalized", "GET", "/api/v1/me", []reqOpt{hdr("Origin", "http://hub.tail.ts.net"), func(r *http.Request) { r.Host = "hub.tail.ts.net:80" }}, 200},
		{"origin different port", "GET", "/api/v1/me", []reqOpt{hdr("Origin", "http://hub.tail.ts.net:9999")}, 403},
		{"POST without X-Requested-With", "POST", "/api/v1/refresh", []reqOpt{noHdr(requestHeader)}, 403},
		{"POST with wrong X-Requested-With", "POST", "/api/v1/refresh", []reqOpt{hdr(requestHeader, "XMLHttpRequest")}, 403},
		{"OPTIONS preflight never answered", "OPTIONS", "/api/v1/refresh", []reqOpt{noHdr(requestHeader), hdr("Origin", "http://"+hostHeader)}, 403},
		{"PUT without header", "PUT", "/api/v1/alerts/rules/high_cpu", []reqOpt{noHdr(requestHeader)}, 403},
		{"DELETE without header", "DELETE", "/api/v1/devices/n-laptop", []reqOpt{noHdr(requestHeader)}, 403},
		{"HEAD needs no header", "HEAD", "/api/v1/me", nil, 200},
		{"POST with header proceeds", "POST", "/api/v1/refresh", nil, 202},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := h.do(tc.method, tc.path, nil, ipAdmin, tc.opts...)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %q", w.Code, tc.status, w.Body.String())
			}
			if tc.status == 403 {
				if errCode(t, w) != codeForbidden {
					t.Errorf("code = %q", errCode(t, w))
				}
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Error("CORS header emitted")
			}
		})
	}
}

func TestAuthenticationResponses(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		ip     string
		status int
		code   string
	}{
		{"unknown identity", ipUnknown, 401, codeUnauthorized},
		{"identity without role", ipNoRole, 403, codeForbidden},
		{"viewer", ipViewer, 200, ""},
		{"admin", ipAdmin, 200, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := h.do("GET", "/api/v1/me", nil, tc.ip)
			expect(t, w, tc.status, tc.code)
			if tc.status == 200 {
				var id model.Identity
				decode(t, w, &id)
				if id.NodeIP != tc.ip || id.AuthMode != "tailscale" {
					t.Errorf("identity = %+v", id)
				}
			}
		})
	}
	// Unparsable remote address.
	r := h.do("GET", "/api/v1/me", nil, "garbage")
	expect(t, r, 401, codeUnauthorized)

	// /healthz never authenticates.
	w := h.do("GET", "/healthz", nil, ipUnknown)
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("healthz = %d %q", w.Code, w.Body.String())
	}
	if w := h.do("POST", "/healthz", nil, ipUnknown); w.Code != 405 {
		t.Errorf("POST /healthz = %d", w.Code)
	}
}

func TestViewerForbiddenOnAdminRoutes(t *testing.T) {
	h := newHarness(t, withAdminActions())
	routes := []struct{ method, path string }{
		{"POST", "/api/v1/devices/n-laptop/authorize"},
		{"POST", "/api/v1/devices/n-laptop/tags"},
		{"POST", "/api/v1/devices/n-laptop/key-expiry"},
		{"POST", "/api/v1/devices/n-laptop/routes"},
		{"POST", "/api/v1/devices/n-laptop/name"},
		{"DELETE", "/api/v1/devices/n-laptop"},
		{"POST", "/api/v1/alerts/1/ack"},
		{"PUT", "/api/v1/alerts/rules/high_cpu"},
		{"POST", "/api/v1/alerts/test"},
		{"GET", "/api/v1/audit"},
		{"POST", "/api/v1/refresh"},
	}
	for _, rt := range routes {
		w := h.do(rt.method, rt.path, map[string]any{"authorized": true}, ipViewer)
		if w.Code != 403 || errCode(t, w) != codeForbidden {
			t.Errorf("%s %s as viewer = %d %q", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
	if len(h.auditEntries()) != 0 || len(h.api.recorded()) != 0 {
		t.Error("viewer attempts must not reach the API or the audit log")
	}
	// Viewer routes still work for viewers.
	for _, p := range []string{"/api/v1/me", "/api/v1/overview", "/api/v1/settings", "/api/v1/devices", "/api/v1/devices/n-laptop",
		"/api/v1/devices/laptop/series", "/api/v1/devices/laptop/uptime", "/api/v1/devices/laptop/events", "/api/v1/events",
		"/api/v1/alerts", "/api/v1/alerts/rules", "/api/v1/network/topology"} {
		if w := h.do("GET", p, nil, ipViewer); w.Code != 200 {
			t.Errorf("GET %s as viewer = %d %q", p, w.Code, w.Body.String())
		}
	}
}

func TestRateLimiting(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < rateLimitBurst; i++ {
		if w := h.do("GET", "/api/v1/me", nil, ipViewer); w.Code != 200 {
			t.Fatalf("request %d = %d", i, w.Code)
		}
	}
	w := h.do("GET", "/api/v1/me", nil, ipViewer)
	expect(t, w, 429, codeRateLimited)
	if w.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	// Other identities are unaffected.
	if w := h.do("GET", "/api/v1/me", nil, ipAdmin); w.Code != 200 {
		t.Errorf("admin = %d", w.Code)
	}
	// Tokens refill at 20/s.
	h.advance(time.Second)
	for i := 0; i < 20; i++ {
		if w := h.do("GET", "/api/v1/me", nil, ipViewer); w.Code != 200 {
			t.Fatalf("after refill request %d = %d", i, w.Code)
		}
	}
	expect(t, h.do("GET", "/api/v1/me", nil, ipViewer), 429, codeRateLimited)
}

func TestUnknownAPIPathsAreJSON(t *testing.T) {
	h := newHarness(t)
	w := h.do("GET", "/api/v1/nope", nil, ipViewer)
	expect(t, w, 404, codeNotFound)
	w = h.do("GET", "/api/", nil, ipViewer)
	expect(t, w, 404, codeNotFound)
	w = h.do("GET", "/api/v1/refresh", nil, ipAdmin) // wrong method
	expect(t, w, 405, codeNotFound)
	if !strings.Contains(w.Header().Get("Allow"), "POST") {
		t.Errorf("Allow = %q", w.Header().Get("Allow"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestBodyLimit(t *testing.T) {
	h := newHarness(t, withAdminActions())
	big := `{"tags":["tag:a"],"pad":"` + strings.Repeat("x", maxBodyBytes+10) + `"}`
	w := h.do("POST", "/api/v1/devices/n-laptop/tags", big, ipAdmin)
	expect(t, w, http.StatusRequestEntityTooLarge, codeBadRequest)
	if len(h.api.recorded()) != 0 {
		t.Error("oversize body reached the API")
	}
	// Just under the limit still parses.
	ok := `{"tags":["tag:a"],"pad":"` + strings.Repeat("x", maxBodyBytes-100) + `"}`
	w = h.do("POST", "/api/v1/devices/n-laptop/tags", ok, ipAdmin)
	expect(t, w, 200, "")
}

func TestRecoverMiddleware(t *testing.T) {
	h := newHarness(t)
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	chain := h.srv.recoverer(h.srv.logging(h.srv.securityHeaders(panicking)))
	r := newRequest("GET", "/api/v1/me")
	rec := newRecorder()
	chain.ServeHTTP(rec, r)
	expect(t, rec, 500, codeInternal)
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing on recovered response")
	}
	r = newRequest("GET", "/some/page")
	rec = newRecorder()
	chain.ServeHTTP(rec, r)
	if rec.Code != 500 {
		t.Errorf("UI panic status = %d", rec.Code)
	}
}

func TestRequestLogging(t *testing.T) {
	h := newHarness(t)
	var buf bytes.Buffer
	h.srv.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h.srv.handler = h.srv.buildHandler()
	h.do("GET", "/api/v1/devices?secret=doNotLog", nil, ipViewer)
	out := buf.String()
	if !strings.Contains(out, "path=/api/v1/devices") || !strings.Contains(out, "login=bob@example.com") || !strings.Contains(out, "status=200") {
		t.Errorf("log line missing fields: %s", out)
	}
	if strings.Contains(out, "doNotLog") {
		t.Errorf("query string logged: %s", out)
	}
}

func TestResolveListen(t *testing.T) {
	h := newHarness(t)
	s := h.srv
	s.autoRetry = time.Millisecond
	s.autoTimeout = 5 * time.Millisecond
	ctx := context.Background()
	set := func(listen string, insecure, demo bool) {
		cfg := *h.cfg
		cfg.Listen = listen
		cfg.InsecureListenAny = insecure
		cfg.Demo = demo
		s.cfg = &cfg
	}

	tests := []struct {
		name     string
		listen   string
		insecure bool
		demo     bool
		want     string
		wantErr  string
	}{
		{"auto demo", "auto", false, true, "127.0.0.1:8484", ""},
		{"auto tailscale", "auto", false, false, "100.64.0.1:8484", ""},
		{"tailscale ip", "100.64.0.5:9000", false, false, "100.64.0.5:9000", ""},
		{"tailscale ipv6", "[fd7a:115c:a1e0::1]:9000", false, false, "[fd7a:115c:a1e0::1]:9000", ""},
		{"loopback", "127.0.0.1:8484", false, false, "127.0.0.1:8484", ""},
		{"localhost", "localhost:8484", false, false, "localhost:8484", ""},
		{"unspecified refused", "0.0.0.0:8484", false, false, "", "refusing"},
		{"ipv6 unspecified refused", "[::]:8484", false, false, "", "refusing"},
		{"lan refused", "192.168.1.5:8484", false, false, "", "refusing"},
		{"unspecified allowed", "0.0.0.0:8484", true, false, "0.0.0.0:8484", ""},
		{"lan allowed", "192.168.1.5:8484", true, false, "192.168.1.5:8484", ""},
		{"bad port", "127.0.0.1:99999", false, false, "", "port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set(tc.listen, tc.insecure, tc.demo)
			got, err := s.resolveListen(ctx)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("resolveListen = %q, %v; want %q", got, err, tc.want)
			}
		})
	}

	// auto without a Tailscale IP fails with a hint after the retry budget.
	set("auto", false, false)
	h.local.mu.Lock()
	h.local.status = &source_LocalStatusNoIP
	h.local.mu.Unlock()
	_, err := s.resolveListen(ctx)
	if err == nil || !strings.Contains(err.Error(), "tailscaled") {
		t.Fatalf("auto without IP err = %v", err)
	}
	h.local.mu.Lock()
	h.local.statusErr = context.DeadlineExceeded
	h.local.mu.Unlock()
	if _, err := s.resolveListen(ctx); err == nil {
		t.Fatal("auto with status error succeeded")
	}
	// A cancelled context stops the retry loop immediately.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.resolveListen(cctx); err == nil {
		t.Fatal("cancelled context did not fail")
	}
	h.local.mu.Lock()
	h.local.statusErr = nil
	h.local.mu.Unlock()
	s.d.Local = nil
	if _, err := s.resolveListen(ctx); err == nil {
		t.Fatal("auto without local source succeeded")
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	h := newHarness(t)
	h.srv.heartbeat = 10 * time.Millisecond
	// The client's source address is 127.0.0.1, which the harness
	// authenticator rejects; map every address to a viewer instead.
	h.srv.auth = alwaysViewer{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.srv.Serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}
	// Regular request.
	req, _ := http.NewRequest("GET", base+"/healthz", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
	// Open an SSE stream.
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	sreq, _ := http.NewRequestWithContext(sctx, "GET", base+"/api/v1/stream", nil)
	sresp, err := http.DefaultClient.Do(sreq)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer sresp.Body.Close()
	buf := make([]byte, 64)
	if _, err := sresp.Body.Read(buf); err != nil { // hello frame
		t.Fatalf("read hello: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	// The stream ended with the server.
	readDone := make(chan struct{})
	go func() {
		for {
			if _, err := sresp.Body.Read(buf); err != nil {
				close(readDone)
				return
			}
		}
	}()
	select {
	case <-readDone:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE stream still open after shutdown")
	}
}

// alwaysViewer maps every address to a viewer identity.
type alwaysViewer struct{}

func (alwaysViewer) Authenticate(_ context.Context, remoteAddr string) (*model.Identity, error) {
	ip, _ := remoteIP(remoteAddr)
	return &model.Identity{Login: "bob@example.com", NodeName: "bob-pc", NodeID: "n-bob", NodeIP: ip.String(), Role: model.RoleViewer, AuthMode: "tailscale"}, nil
}

var source_LocalStatusNoIP = sourceLocalStatusNoIP()

func TestClassifyListenIP(t *testing.T) {
	tests := []struct {
		ip   string
		want listenKind
	}{
		{"100.64.0.1", listenTailscale},
		{"100.127.255.255", listenTailscale},
		{"100.128.0.1", listenOther},
		{"fd7a:115c:a1e0::1", listenTailscale},
		{"127.0.0.1", listenLoopback},
		{"::1", listenLoopback},
		{"::ffff:127.0.0.1", listenLoopback},
		{"0.0.0.0", listenAny},
		{"::", listenAny},
		{"10.0.0.1", listenOther},
	}
	for _, tc := range tests {
		if got := classifyListenIP(mustAddr(tc.ip)); got != tc.want {
			t.Errorf("classifyListenIP(%s) = %s, want %s", tc.ip, got, tc.want)
		}
	}
	if _, ok := firstIPv4([]string{"fd7a::1", "bad", "100.64.0.9"}); !ok {
		t.Error("firstIPv4 missed the IPv4")
	}
	if _, ok := firstIPv4([]string{"fd7a::1"}); ok {
		t.Error("firstIPv4 found an IPv4 in an IPv6-only list")
	}
	_ = config.DefaultPort
}

func TestServeTLS(t *testing.T) {
	certFile, keyFile := selfSignedCert(t)
	h := newHarness(t)
	cfg := *h.cfg
	cfg.TLSCert, cfg.TLSKey = certFile, keyFile
	srv, err := NewServer(Deps{Cfg: &cfg, Store: h.st, Collector: h.col, Alerts: h.eng, Local: h.local, Log: quietLogger(), Auth: alwaysViewer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !srv.tls {
		t.Fatal("TLS not detected from config")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	base := "https://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}}
	resp, err := client.Do(mustReq("GET", base+"/api/v1/me"))
	if err != nil {
		t.Fatalf("https request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Strict-Transport-Security") != strictTransportSecurity {
		t.Errorf("status %d, HSTS %q", resp.StatusCode, resp.Header.Get("Strict-Transport-Security"))
	}
	if resp.TLS == nil || resp.TLS.Version < tls.VersionTLS12 {
		t.Errorf("negotiated TLS %+v", resp.TLS)
	}
	// TLS 1.1 clients are refused.
	old := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS11, MaxVersion: tls.VersionTLS11}}}
	if r, err := old.Do(mustReq("GET", base+"/healthz")); err == nil {
		r.Body.Close()
		t.Error("TLS 1.1 handshake succeeded")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}

	// Bad certificate files fail before serving.
	cfg.TLSKey = certFile
	srv2, _ := NewServer(Deps{Cfg: &cfg, Store: h.st, Collector: h.col, Alerts: h.eng, Local: h.local, Log: quietLogger(), Auth: alwaysViewer{}})
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	if err := srv2.Serve(context.Background(), ln2); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Errorf("Serve with bad key = %v", err)
	}
}

func TestListenAndServeLoopback(t *testing.T) {
	// Pick a free port, then let ListenAndServe bind it.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()

	h := newHarness(t)
	cfg := *h.cfg
	cfg.Listen = addr
	h.srv.cfg = &cfg
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.srv.ListenAndServe(ctx) }()

	client := &http.Client{Timeout: 2 * time.Second}
	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Do(mustReq("GET", "http://"+addr+"/healthz"))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("healthz never answered: %v", err)
	}
	resp.Body.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}

	// A refused address fails without binding.
	cfg.Listen = "192.168.7.7:1"
	if err := h.srv.ListenAndServe(context.Background()); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("ListenAndServe on LAN address = %v", err)
	}
}
