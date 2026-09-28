package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

const testToken = "test-token-0123456789abcdef"

type fakeReports struct {
	rep agentproto.Report
	ok  bool
}

func (f *fakeReports) Latest() (agentproto.Report, bool) { return f.rep, f.ok }

// recordingHandler keeps slog records for assertions.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }
func (h *recordingHandler) count(level slog.Level, msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level && r.Message == msg {
			n++
		}
	}
	return n
}

// identities is a canned WhoIs directory keyed by IP.
var identities = map[string]*source.WhoIs{
	"100.64.0.10": {NodeID: "n10", NodeName: "laptop.example.ts.net", NodeIP: "100.64.0.10", LoginName: "alice@example.com"},
	"100.64.0.11": {NodeID: "n11", NodeName: "bobbox.example.ts.net", NodeIP: "100.64.0.11", LoginName: "bob@example.com"},
	"100.64.0.20": {NodeID: "n20", NodeName: "hub.example.ts.net", NodeIP: "100.64.0.20", LoginName: "tagged-device", IsTagged: true, Tags: []string{"tag:tailwatch"}},
	"100.64.0.21": {NodeID: "n21", NodeName: "srv.example.ts.net", NodeIP: "100.64.0.21", LoginName: "tagged-device", IsTagged: true, Tags: []string{"tag:server"}},
}

var errDaemonDown = errors.New("tailscaled unavailable")

func directoryWhoIs() *fakeWhoIs {
	return &fakeWhoIs{fn: func(remoteAddr string) (*source.WhoIs, error) {
		ip := remoteIP(remoteAddr)
		if ip == "100.64.0.99" {
			return nil, errDaemonDown
		}
		if w, ok := identities[ip]; ok {
			return w, nil
		}
		return nil, fmt.Errorf("whois %s: %w", remoteAddr, source.ErrNotFound)
	}}
}

type testServerOpts struct {
	mode    authMode
	token   string
	users   []string
	tags    []string
	nodes   []string
	owner   string
	whois   whoisSource
	reports reportSource
	log     slog.Handler
	clock   *fakeClock
	burst   int
}

func sampleReport() agentproto.Report {
	return agentproto.Report{
		ProtocolVersion: agentproto.ProtocolVersion,
		AgentVersion:    "v-test",
		SampledAt:       time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
		Host:            agentproto.Host{Hostname: "box", OS: "linux", Arch: "amd64"},
		CPU:             agentproto.CPU{Percent: 12.5, Count: 4, PerCore: []float64{1, 2, 3, 4}},
		Memory:          agentproto.Memory{Total: 100, Used: 50, Percent: 50},
		Disks:           []agentproto.Disk{{Mount: "/", Total: 10, Used: 1, Percent: 10, Primary: true}},
		Net:             agentproto.Net{Interfaces: []agentproto.Interface{{Name: "eth0", RxBytes: 1, Physical: true}}, TailscaleIf: "tailscale0"},
		Processes:       42,
		Tailscale:       &agentproto.TailscaleInfo{Version: "1.102.5", BackendState: "Running", IPs: []string{"100.64.0.1"}},
	}
}

func newTestServer(t *testing.T, o testServerOpts) *server {
	t.Helper()
	if o.mode == "" {
		o.mode = authWhois
	}
	if o.whois == nil {
		o.whois = directoryWhoIs()
	}
	if o.reports == nil {
		o.reports = &fakeReports{rep: sampleReport(), ok: true}
	}
	if o.log == nil {
		o.log = slog.NewTextHandler(io.Discard, nil)
	}
	if o.clock == nil {
		o.clock = newFakeClock()
	}
	pol, err := newPolicy(o.mode, o.token, o.users, o.tags, o.nodes)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newServer(serverOptions{
		Version:   "v-test",
		Policy:    pol,
		WhoIs:     o.whois,
		Owner:     func(context.Context) string { return o.owner },
		Reports:   o.reports,
		Log:       slog.New(o.log),
		Now:       o.clock.now,
		RateBurst: o.burst,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(s *server, method, path, remote string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func decodeError(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rr.Body.String(), err)
	}
	return body["error"]
}

func TestAuthDecisions(t *testing.T) {
	withTok := map[string]string{agentproto.HeaderToken: testToken}
	badTok := map[string]string{agentproto.HeaderToken: "wrong"}
	tests := []struct {
		name     string
		opts     testServerOpts
		remote   string
		headers  map[string]string
		wantCode int
		wantErr  string
	}{
		{"whois: owner allowed", testServerOpts{owner: "alice@example.com"}, "100.64.0.10:4000", nil, 200, ""},
		{"whois: other user forbidden", testServerOpts{owner: "alice@example.com"}, "100.64.0.11:4000", nil, 403, "forbidden"},
		{"whois: tag:tailwatch allowed", testServerOpts{owner: "alice@example.com"}, "100.64.0.20:4000", nil, 200, ""},
		{"whois: other tag forbidden", testServerOpts{owner: "alice@example.com"}, "100.64.0.21:4000", nil, 403, "forbidden"},
		{"whois: tagged self without lists only accepts tag:tailwatch", testServerOpts{owner: ""}, "100.64.0.10:4000", nil, 403, "forbidden"},
		{"whois: tagged self accepts tag:tailwatch", testServerOpts{owner: ""}, "100.64.0.20:4000", nil, 200, ""},
		{"whois: allow-user", testServerOpts{owner: "alice@example.com", users: []string{"bob@example.com"}}, "100.64.0.11:4000", nil, 200, ""},
		{"whois: allow-user excludes owner", testServerOpts{owner: "alice@example.com", users: []string{"bob@example.com"}}, "100.64.0.10:4000", nil, 403, "forbidden"},
		{"whois: allow-tag", testServerOpts{tags: []string{"tag:server"}}, "100.64.0.21:4000", nil, 200, ""},
		{"whois: allow-node base", testServerOpts{nodes: []string{"srv"}}, "100.64.0.21:4000", nil, 200, ""},
		{"whois: allow-node fqdn", testServerOpts{nodes: []string{"laptop.example.ts.net"}}, "100.64.0.10:4000", nil, 200, ""},
		{"whois: allow-node other forbidden", testServerOpts{nodes: []string{"laptop"}}, "100.64.0.11:4000", nil, 403, "forbidden"},
		{"whois: unknown peer forbidden", testServerOpts{owner: "alice@example.com"}, "100.64.0.50:4000", nil, 403, "forbidden"},
		{"whois: non-tailnet source forbidden", testServerOpts{owner: "alice@example.com"}, "192.168.1.9:4000", nil, 403, "forbidden"},
		{"whois: unparsable remote forbidden", testServerOpts{owner: "alice@example.com"}, "garbage", nil, 403, "forbidden"},
		{"whois: daemon failure is 503", testServerOpts{owner: "alice@example.com"}, "100.64.0.99:4000", nil, 503, "identity lookup unavailable"},
		{"whois: token header ignored", testServerOpts{owner: "alice@example.com"}, "100.64.0.11:4000", withTok, 403, "forbidden"},
		{"token: missing", testServerOpts{mode: authToken, token: testToken}, "100.64.0.11:4000", nil, 403, "forbidden"},
		{"token: wrong", testServerOpts{mode: authToken, token: testToken}, "100.64.0.11:4000", badTok, 403, "forbidden"},
		{"token: right, any peer", testServerOpts{mode: authToken, token: testToken}, "100.64.0.50:4000", withTok, 200, ""},
		{"token: right, non-tailnet source", testServerOpts{mode: authToken, token: testToken}, "192.168.1.9:4000", withTok, 200, ""},
		{"both: token + owner", testServerOpts{mode: authBoth, token: testToken, owner: "alice@example.com"}, "100.64.0.10:4000", withTok, 200, ""},
		{"both: token only", testServerOpts{mode: authBoth, token: testToken, owner: "alice@example.com"}, "100.64.0.11:4000", withTok, 403, "forbidden"},
		{"both: whois only", testServerOpts{mode: authBoth, token: testToken, owner: "alice@example.com"}, "100.64.0.10:4000", nil, 403, "forbidden"},
		{"both: wrong token short-circuits", testServerOpts{mode: authBoth, token: testToken, owner: "alice@example.com"}, "100.64.0.10:4000", badTok, 403, "forbidden"},
		{"both: token + allow-tag", testServerOpts{mode: authBoth, token: testToken, tags: []string{"tag:server"}}, "100.64.0.21:4000", withTok, 200, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, tc.opts)
			for _, path := range []string{agentproto.PathMetrics, agentproto.PathHealth} {
				rr := do(s, http.MethodGet, path, tc.remote, tc.headers)
				if rr.Code != tc.wantCode {
					t.Fatalf("%s: code = %d, want %d (body %s)", path, rr.Code, tc.wantCode, rr.Body.String())
				}
				if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
					t.Fatalf("%s: Content-Type = %q", path, ct)
				}
				if tc.wantErr != "" {
					if got := decodeError(t, rr); got != tc.wantErr {
						t.Fatalf("%s: error = %q, want %q", path, got, tc.wantErr)
					}
				}
			}
		})
	}
}

func TestForbiddenShapeIsExact(t *testing.T) {
	s := newTestServer(t, testServerOpts{owner: "alice@example.com"})
	rr := do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.11:1", nil)
	if rr.Code != http.StatusForbidden || strings.TrimSpace(rr.Body.String()) != `{"error":"forbidden"}` {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}

func TestHealthShape(t *testing.T) {
	s := newTestServer(t, testServerOpts{owner: "alice@example.com"})
	rr := do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.10:1", nil)
	if rr.Code != 200 {
		t.Fatalf("code %d", rr.Code)
	}
	var h agentproto.HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if !h.OK || h.AgentVersion != "v-test" || h.ProtocolVersion != agentproto.ProtocolVersion {
		t.Fatalf("health = %+v", h)
	}
}

func TestMetricsBodyAndNotReady(t *testing.T) {
	reports := &fakeReports{rep: sampleReport(), ok: false}
	s := newTestServer(t, testServerOpts{owner: "alice@example.com", reports: reports})

	rr := do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.10:1", nil)
	if rr.Code != http.StatusServiceUnavailable || decodeError(t, rr) != "not ready" || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("not ready: %d %s", rr.Code, rr.Body.String())
	}

	reports.ok = true
	rr = do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.10:1", nil)
	if rr.Code != 200 {
		t.Fatalf("code %d: %s", rr.Code, rr.Body.String())
	}
	var got agentproto.Report
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := sampleReport()
	if got.ProtocolVersion != want.ProtocolVersion || got.AgentVersion != want.AgentVersion || !got.SampledAt.Equal(want.SampledAt) ||
		got.CPU.Percent != want.CPU.Percent || len(got.CPU.PerCore) != 4 || got.Memory != want.Memory || len(got.Disks) != 1 ||
		got.Net.TailscaleIf != "tailscale0" || got.Processes != 42 || got.Tailscale == nil || got.Tailscale.Version != "1.102.5" {
		t.Fatalf("decoded report differs: %+v", got)
	}
	if cl := rr.Header().Get("Content-Length"); cl == "" {
		t.Fatal("missing Content-Length")
	}
}

func TestRoutesAndMethods(t *testing.T) {
	s := newTestServer(t, testServerOpts{owner: "alice@example.com"})
	tests := []struct {
		method, path string
		wantCode     int
		wantAllow    bool
	}{
		{http.MethodGet, "/", 404, false},
		{http.MethodGet, "/nope", 404, false},
		{http.MethodGet, "/v1", 404, false},
		{http.MethodGet, "/v1/metrics/", 404, false},
		{http.MethodGet, "/v1/metrics/../health", 404, false},
		{http.MethodGet, "/V1/metrics", 404, false},
		{http.MethodGet, "/v1/metrics?x=1", 200, false},
		{http.MethodPost, "/v1/metrics", 405, true},
		{http.MethodPut, "/v1/health", 405, true},
		{http.MethodDelete, "/v1/health", 405, true},
		{http.MethodHead, "/v1/metrics", 405, true},
		{http.MethodOptions, "/v1/metrics", 405, true},
		{http.MethodPost, "/nope", 404, false},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := do(s, tc.method, tc.path, "100.64.0.10:1", nil)
			if rr.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d (%s)", rr.Code, tc.wantCode, rr.Body.String())
			}
			if tc.wantAllow && rr.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("Allow = %q", rr.Header().Get("Allow"))
			}
			if rr.Code != 200 {
				if e := decodeError(t, rr); e == "" {
					t.Fatal("error body missing")
				}
			}
		})
	}
	// Unauthenticated callers never learn routes: everything is 403.
	for _, path := range []string{"/nope", "/v1/metrics", "/v1/health"} {
		rr := do(s, http.MethodPost, path, "100.64.0.11:1", nil)
		if rr.Code != 403 {
			t.Fatalf("unauthenticated %s: %d", path, rr.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestServer(t, testServerOpts{owner: "alice@example.com"})
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Type":            "application/json",
	}
	for _, remote := range []string{"100.64.0.10:1", "100.64.0.11:1", "100.64.0.99:1"} {
		rr := do(s, http.MethodGet, agentproto.PathMetrics, remote, nil)
		for k, v := range want {
			if got := rr.Header().Get(k); got != v {
				t.Errorf("%s (%d): %s = %q, want %q", remote, rr.Code, k, got, v)
			}
		}
		if rr.Header().Get("Access-Control-Allow-Origin") != "" || rr.Header().Get("Server") != "" {
			t.Errorf("unexpected CORS/Server header")
		}
	}
}

func TestRateLimitPerIP(t *testing.T) {
	clock := newFakeClock()
	s := newTestServer(t, testServerOpts{owner: "alice@example.com", clock: clock, burst: 3})
	for i := 0; i < 3; i++ {
		if rr := do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.10:1", nil); rr.Code != 200 {
			t.Fatalf("request %d: %d", i, rr.Code)
		}
	}
	rr := do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.10:1", nil)
	if rr.Code != http.StatusTooManyRequests || decodeError(t, rr) != "rate limited" || rr.Header().Get("Retry-After") != "1" {
		t.Fatalf("4th request: %d %s", rr.Code, rr.Body.String())
	}
	// Other IPs (and even unauthenticated ones) have their own budget.
	if rr := do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.20:1", nil); rr.Code != 200 {
		t.Fatalf("other ip: %d", rr.Code)
	}
	if rr := do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.11:1", nil); rr.Code != 403 {
		t.Fatalf("unauthenticated other ip: %d", rr.Code)
	}
	clock.advance(time.Second) // 10 r/s refill
	if rr := do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.10:1", nil); rr.Code != 200 {
		t.Fatalf("after refill: %d", rr.Code)
	}
}

func TestWhoisCachedPerIP(t *testing.T) {
	clock := newFakeClock()
	wi := directoryWhoIs()
	s := newTestServer(t, testServerOpts{owner: "alice@example.com", whois: wi, clock: clock})
	for i := 0; i < 5; i++ {
		do(s, http.MethodGet, agentproto.PathHealth, fmt.Sprintf("100.64.0.10:%d", 1000+i), nil)
	}
	if wi.calls != 1 {
		t.Fatalf("whois calls = %d, want 1", wi.calls)
	}
	clock.advance(whoisCacheTTL + time.Second)
	do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.10:1", nil)
	if wi.calls != 2 {
		t.Fatalf("whois calls after ttl = %d, want 2", wi.calls)
	}
	// Failures are retried, not cached.
	do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.99:1", nil)
	do(s, http.MethodGet, agentproto.PathHealth, "100.64.0.99:1", nil)
	if wi.calls != 4 {
		t.Fatalf("whois calls after failures = %d, want 4", wi.calls)
	}
}

func TestRejectedRequestsLoggedAtWarnRateLimited(t *testing.T) {
	clock := newFakeClock()
	h := &recordingHandler{}
	s := newTestServer(t, testServerOpts{owner: "alice@example.com", clock: clock, log: h})
	for i := 0; i < 5; i++ {
		do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.11:1", nil)
	}
	if got := h.count(slog.LevelWarn, "request rejected"); got != 1 {
		t.Fatalf("warn records = %d, want 1", got)
	}
	do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.21:1", nil) // other IP logs
	if got := h.count(slog.LevelWarn, "request rejected"); got != 2 {
		t.Fatalf("warn records = %d, want 2", got)
	}
	clock.advance(warnLogInterval + time.Second)
	do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.11:1", nil)
	if got := h.count(slog.LevelWarn, "request rejected"); got != 3 {
		t.Fatalf("warn records = %d, want 3", got)
	}
	// The warning carries the identity but never a token.
	h.mu.Lock()
	defer h.mu.Unlock()
	found := false
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "identity" && strings.Contains(a.Value.String(), "bob@example.com") {
				found = true
			}
			if strings.Contains(a.Value.String(), testToken) {
				t.Fatal("token leaked into log")
			}
			return true
		})
	}
	if !found {
		t.Fatal("rejected-request warning lacks caller identity")
	}
}

func TestTokenNeverLogged(t *testing.T) {
	h := &recordingHandler{}
	s := newTestServer(t, testServerOpts{mode: authToken, token: testToken, log: h})
	do(s, http.MethodGet, agentproto.PathMetrics, "100.64.0.11:1", map[string]string{agentproto.HeaderToken: "leaky-wrong-token"})
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		r.Attrs(func(a slog.Attr) bool {
			if s := a.Value.String(); strings.Contains(s, "leaky-wrong-token") || strings.Contains(s, testToken) {
				t.Fatalf("token material in log attr %s=%s", a.Key, s)
			}
			return true
		})
	}
}

func TestNewServerValidation(t *testing.T) {
	pol, _ := newPolicy(authWhois, "", nil, nil, nil)
	reports := &fakeReports{}
	if _, err := newServer(serverOptions{Policy: nil, Reports: reports, WhoIs: directoryWhoIs()}); err == nil {
		t.Error("nil policy accepted")
	}
	if _, err := newServer(serverOptions{Policy: pol, Reports: nil, WhoIs: directoryWhoIs()}); err == nil {
		t.Error("nil reports accepted")
	}
	if _, err := newServer(serverOptions{Policy: pol, Reports: reports, WhoIs: nil}); err == nil {
		t.Error("whois mode without whois source accepted")
	}
	tokPol, _ := newPolicy(authToken, testToken, nil, nil, nil)
	s, err := newServer(serverOptions{Policy: tokPol, Reports: reports})
	if err != nil || s == nil {
		t.Fatalf("token mode should not need whois: %v", err)
	}
	hs := s.httpServer(context.Background(), "127.0.0.1:0")
	if hs.ReadHeaderTimeout != readHeaderTimeout || hs.ReadTimeout != readTimeout || hs.WriteTimeout != writeTimeout ||
		hs.IdleTimeout != idleTimeout || hs.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("http.Server timeouts not applied: %+v", hs)
	}
}

func TestRemoteIP(t *testing.T) {
	for in, want := range map[string]string{
		"100.64.0.1:1234":              "100.64.0.1",
		"[fd7a:115c:a1e0::1]:1":        "fd7a:115c:a1e0::1",
		"[::ffff:100.64.0.1]:1":        "100.64.0.1",
		"100.64.0.1":                   "100.64.0.1",
		"garbage":                      "",
		"":                             "",
		"[fd7a:115c:a1e0::1]:notaport": "fd7a:115c:a1e0::1", // port is not validated here
	} {
		if got := remoteIP(in); got != want {
			t.Errorf("remoteIP(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestServeOverTCP runs the real http.Server end to end on loopback.
func TestServeOverTCP(t *testing.T) {
	s := newTestServer(t, testServerOpts{mode: authToken, token: testToken})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+agentproto.PathMetrics, nil)
	req.Header.Set(agentproto.HeaderToken, testToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"protocolVersion":"1.0"`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}
