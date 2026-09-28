package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/alerts"
	"github.com/evilgenius79/fable-tailscale/internal/collector"
	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// Fixed identities keyed by source IP (see fakeAuth).
const (
	ipAdmin      = "100.64.0.10"
	ipViewer     = "100.64.0.11"
	ipNoRole     = "100.64.0.12"
	ipUnknown    = "100.64.0.13"
	hostHeader   = "hub.tail.ts.net:8484"
	deviceLaptop = "n-laptop"
	deviceNAS    = "n-nas"
)

var baseTime = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- fake LocalSource ---------------------------------------------------------

type fakeLocal struct {
	mu        sync.Mutex
	status    *source.LocalStatus
	statusErr error
	whois     map[string]*source.WhoIs
	whoisErr  error
	whoisN    int
	pings     map[string]*source.PingReply
	pingErr   error
}

func (f *fakeLocal) Status(context.Context) (*source.LocalStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.status == nil {
		return &source.LocalStatus{}, nil
	}
	cp := *f.status
	cp.Peers = slices.Clone(f.status.Peers)
	return &cp, nil
}

func (f *fakeLocal) WhoIs(_ context.Context, remoteAddr string) (*source.WhoIs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.whoisN++
	if f.whoisErr != nil {
		return nil, f.whoisErr
	}
	ip, _ := remoteIP(remoteAddr)
	w, ok := f.whois[ip.String()]
	if !ok {
		return nil, errors.New("no such node")
	}
	cp := *w
	return &cp, nil
}

func (f *fakeLocal) Ping(_ context.Context, ip string, _ time.Duration) (*source.PingReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pingErr != nil {
		return nil, f.pingErr
	}
	if r, ok := f.pings[ip]; ok {
		cp := *r
		return &cp, nil
	}
	return &source.PingReply{Err: "no route"}, nil
}

func (f *fakeLocal) whoisCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.whoisN
}

// --- fake ControlAPI -----------------------------------------------------------

type apiCallRecord struct {
	method string
	id     string
	args   any
}

type fakeAPI struct {
	mu         sync.Mutex
	configured bool
	err        error
	calls      []apiCallRecord
}

func (f *fakeAPI) Configured() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.configured }
func (f *fakeAPI) Tailnet() string  { return "example.com" }
func (f *fakeAPI) Devices(context.Context) ([]source.APIDevice, error) {
	return nil, nil
}
func (f *fakeAPI) record(method, id string, args any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, apiCallRecord{method, id, args})
	return f.err
}
func (f *fakeAPI) SetAuthorized(_ context.Context, id string, v bool) error {
	return f.record("SetAuthorized", id, v)
}
func (f *fakeAPI) SetTags(_ context.Context, id string, tags []string) error {
	return f.record("SetTags", id, tags)
}
func (f *fakeAPI) SetKeyExpiryDisabled(_ context.Context, id string, v bool) error {
	return f.record("SetKeyExpiryDisabled", id, v)
}
func (f *fakeAPI) SetRoutes(_ context.Context, id string, routes []string) error {
	return f.record("SetRoutes", id, routes)
}
func (f *fakeAPI) DeleteDevice(_ context.Context, id string) error {
	return f.record("DeleteDevice", id, nil)
}
func (f *fakeAPI) SetName(_ context.Context, id string, name string) error {
	return f.record("SetName", id, name)
}
func (f *fakeAPI) recorded() []apiCallRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// --- fake AgentClient ----------------------------------------------------------

type fakeAgents struct{}

func (fakeAgents) Fetch(context.Context, string, int) (*agentproto.Report, error) {
	return nil, errors.New("connection refused")
}

// --- fake Authenticator --------------------------------------------------------

type fakeAuth struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeAuth) Authenticate(_ context.Context, remoteAddr string) (*model.Identity, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	ip, ok := remoteIP(remoteAddr)
	if !ok {
		return nil, ErrUnauthenticated
	}
	switch ip.String() {
	case ipAdmin:
		return &model.Identity{Login: "alice@example.com", DisplayName: "Alice", NodeName: "alice-mbp.tail.ts.net", NodeID: "n-alice", NodeIP: ipAdmin, Tags: []string{}, Role: model.RoleAdmin, AuthMode: "tailscale"}, nil
	case ipViewer:
		return &model.Identity{Login: "bob@example.com", DisplayName: "Bob", NodeName: "bob-pc.tail.ts.net", NodeID: "n-bob", NodeIP: ipViewer, Tags: []string{}, Role: model.RoleViewer, AuthMode: "tailscale"}, nil
	case ipNoRole:
		return nil, fmt.Errorf("%w: carol", ErrForbidden)
	}
	return nil, fmt.Errorf("%w: whois failed", ErrUnauthenticated)
}

// --- fake Notifier ---------------------------------------------------------------

type fakeNotifier struct {
	mu   sync.Mutex
	sent []alerts.Notification
	err  error
}

func (f *fakeNotifier) Name() string { return "fake" }
func (f *fakeNotifier) Send(_ context.Context, n alerts.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return f.err
}

// --- fixtures -----------------------------------------------------------------------

func peer(id, name, ip string) source.LocalPeer {
	return source.LocalPeer{
		ID:           model.DeviceID(id),
		HostName:     name,
		DNSName:      name + ".tail.ts.net",
		OS:           "linux",
		UserLogin:    "alice@example.com",
		UserDisplay:  "Alice",
		TailscaleIPs: []string{ip},
		Online:       true,
		CurAddr:      "203.0.113.1:41641",
		Created:      baseTime.Add(-30 * 24 * time.Hour),
	}
}

func hubStatus() *source.LocalStatus {
	return &source.LocalStatus{
		Version:        "1.86.0",
		BackendState:   "Running",
		TailscaleIPs:   []string{"100.64.0.1", "fd7a:115c:a1e0::1"},
		Self:           peer("n-hub", "hub", "100.64.0.1"),
		Health:         []string{},
		TailnetName:    "example.com",
		MagicDNSSuffix: "tail.ts.net",
		Peers: []source.LocalPeer{
			peer(deviceNAS, "nas", "100.64.0.3"),
			peer(deviceLaptop, "laptop", "100.64.0.2"),
		},
	}
}

func uiFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><html><body>tailwatch ui</body></html>")},
		"favicon.svg":        {Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
		"assets/app-abc.js":  {Data: []byte("console.log('app')")},
		"assets/app-abc.css": {Data: []byte("body{}")},
	}
}

// --- harness ----------------------------------------------------------------------

type harness struct {
	t     *testing.T
	cfg   *config.Config
	st    *store.Store
	col   *collector.Collector
	eng   *alerts.Engine
	local *fakeLocal
	api   *fakeAPI
	auth  *fakeAuth
	notif *fakeNotifier
	srv   *Server
	ui    fs.FS
	now   time.Time
	mu    sync.Mutex
}

type harnessOpt func(h *harness)

// withAdminActions enables admin actions and a configured control API.
func withAdminActions() harnessOpt {
	return func(h *harness) {
		h.cfg.EnableAdminActions = true
		h.cfg.APIKey = "tskey-api-test"
		h.api.configured = true
	}
}

// withUI replaces the UI file system (nil = no build).
func withUI(fsys fs.FS) harnessOpt {
	return func(h *harness) { h.ui = fsys }
}

func newHarness(t *testing.T, opts ...harnessOpt) *harness {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:", quietLogger())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg, err := config.Load([]string{"--listen", "100.64.0.1:8484", "--admins", "alice@example.com"}, nil)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	h := &harness{t: t, cfg: cfg, st: st, local: &fakeLocal{status: hubStatus()}, api: &fakeAPI{}, auth: &fakeAuth{}, notif: &fakeNotifier{}, ui: uiFS(), now: baseTime}
	for _, o := range opts {
		o(h)
	}
	h.col = collector.New(collector.Config{PollInterval: 15 * time.Second, AgentEnabled: false, Version: "test"}, st, h.local, h.api, fakeAgents{}, quietLogger())
	if err := h.col.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	h.eng = alerts.NewEngine(st, h.col, []alerts.Notifier{h.notif}, quietLogger())
	deps := Deps{
		Cfg:       cfg,
		Store:     st,
		Collector: h.col,
		Alerts:    h.eng,
		API:       h.api,
		Local:     h.local,
		UI:        h.ui,
		Log:       quietLogger(),
		Version:   "test",
		Auth:      h.auth,
	}
	srv, err := NewServer(deps)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.now = h.clock
	srv.limiter = newRateLimiter(rateLimitPerSecond, rateLimitBurst, h.clock)
	h.srv = srv
	return h
}

func (h *harness) clock() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now
}

func (h *harness) advance(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

// reqOpt tweaks a test request.
type reqOpt func(r *http.Request)

func hdr(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func noHdr(k string) reqOpt  { return func(r *http.Request) { r.Header.Del(k) } }

// do performs a request through the full handler chain. body may be nil,
// a string (sent verbatim) or any value (JSON-encoded). Non-GET requests
// carry the X-Requested-With header unless removed by an option.
func (h *harness) do(method, path string, body any, ip string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			h.t.Fatalf("encode body: %v", err)
		}
		rd = bytes.NewReader(buf)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = hostHeader
	r.RemoteAddr = ip + ":51234"
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set(requestHeader, requestHeaderValue)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(w, r)
	return w
}

// decode unmarshals a JSON response body.
func decode(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
}

// errCode returns the error code of an error envelope ("" if none).
func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var eb errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
		t.Fatalf("not an error envelope: %q", w.Body.String())
	}
	return eb.Error.Code
}

// expect asserts status and, when non-empty, the error code.
func expect(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body %q", w.Code, status, w.Body.String())
	}
	if code != "" {
		if got := errCode(t, w); got != code {
			t.Fatalf("code = %q, want %q; body %q", got, code, w.Body.String())
		}
	}
}

func (h *harness) auditEntries() []model.AuditEntry {
	h.t.Helper()
	entries, err := h.st.ListAudit(context.Background(), 100)
	if err != nil {
		h.t.Fatalf("ListAudit: %v", err)
	}
	return entries
}

func (h *harness) adminEvents() []model.Event {
	h.t.Helper()
	evs, err := h.st.ListEvents(context.Background(), model.EventQuery{Types: []model.EventType{model.EventAdminAction}})
	if err != nil {
		h.t.Fatalf("ListEvents: %v", err)
	}
	return evs
}
