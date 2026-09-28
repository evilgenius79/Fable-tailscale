package tsapi

import (
	"bytes"
	"context"
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

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// --- test helpers ---------------------------------------------------------

type recReq struct {
	Method, Path, RawQuery string
	Header                 http.Header
	Body                   string
}

// recorder captures every request a test server receives.
type recorder struct {
	mu   sync.Mutex
	reqs []recReq
}

func (r *recorder) wrap(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.reqs = append(r.reqs, recReq{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Clone(), string(body)})
		r.mu.Unlock()
		req.Body = io.NopCloser(bytes.NewReader(body))
		h(w, req)
	}
}

func (r *recorder) all() []recReq {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recReq(nil), r.reqs...)
}

func (r *recorder) count(path string) int {
	n := 0
	for _, q := range r.all() {
		if path == "" || q.Path == path {
			n++
		}
	}
	return n
}

// fakeClock is a mutable, goroutine-safe time source.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

// sleepRecorder replaces the retry sleep so tests never wait.
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *sleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.delays = append(s.delays, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *sleepRecorder) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delays...)
}

// testEnv bundles a client, its server recorder, clock and sleep recorder.
type testEnv struct {
	c     *Client
	rec   *recorder
	clock *fakeClock
	sleep *sleepRecorder
	srv   *httptest.Server
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// newEnv starts a test server around h and returns a client pointed at it.
// mod, when non-nil, may adjust the Options (credentials etc.) before New.
func newEnv(t *testing.T, h http.HandlerFunc, mod func(*Options)) *testEnv {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(h))
	t.Cleanup(srv.Close)
	o := Options{BaseURL: srv.URL, APIKey: "tskey-api-TESTKEY", Version: "test"}
	if mod != nil {
		mod(&o)
	}
	c := New(o, testLogger())
	clock := newFakeClock()
	sl := &sleepRecorder{}
	c.now = clock.now
	c.sleep = sl.sleep
	return &testEnv{c: c, rec: rec, clock: clock, sleep: sl, srv: srv}
}

func okDevices(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"devices":[]}`)
}

func jsonStatus(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// --- construction ---------------------------------------------------------

func TestNewDefaults(t *testing.T) {
	c := New(Options{APIKey: " tskey-api-x \n"}, nil)
	if got := c.base.String(); got != DefaultBaseURL {
		t.Errorf("base = %q, want %q", got, DefaultBaseURL)
	}
	if c.Tailnet() != "-" {
		t.Errorf("tailnet = %q, want -", c.Tailnet())
	}
	if c.hc == nil || c.hc.Timeout != DefaultTimeout {
		t.Errorf("default http client timeout = %v, want %v", c.hc.Timeout, DefaultTimeout)
	}
	if c.ua != "tailwatch/dev" {
		t.Errorf("user agent = %q, want tailwatch/dev", c.ua)
	}
	if c.apiKey != "tskey-api-x" {
		t.Errorf("api key not trimmed: %q", c.apiKey)
	}
	if c.log == nil {
		t.Error("logger is nil")
	}
	// Trailing slash and custom version are normalised.
	c = New(Options{BaseURL: "https://example.test/", Tailnet: "corp.example", Version: "1.2.3"}, testLogger())
	if got := c.base.String(); got != "https://example.test" {
		t.Errorf("base = %q", got)
	}
	if c.ua != "tailwatch/1.2.3" || c.Tailnet() != "corp.example" {
		t.Errorf("ua=%q tailnet=%q", c.ua, c.Tailnet())
	}
	// A version with control characters falls back to dev.
	if c := New(Options{Version: "bad\nversion"}, testLogger()); c.ua != "tailwatch/dev" {
		t.Errorf("ua = %q, want tailwatch/dev", c.ua)
	}
}

func TestConfigured(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		want bool
	}{
		{"nothing", Options{}, false},
		{"api key", Options{APIKey: "k"}, true},
		{"whitespace key", Options{APIKey: "  \n"}, false},
		{"oauth both", Options{OAuthClientID: "id", OAuthClientSecret: "s"}, true},
		{"oauth id only", Options{OAuthClientID: "id"}, false},
		{"oauth secret only", Options{OAuthClientSecret: "s"}, false},
		{"key and oauth", Options{APIKey: "k", OAuthClientID: "id", OAuthClientSecret: "s"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := New(tc.o, testLogger()).Configured(); got != tc.want {
				t.Errorf("Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}

// allMethods invokes every ControlAPI method and returns their errors by name.
func allMethods(c source.ControlAPI) map[string]error {
	ctx := context.Background()
	_, devErr := c.Devices(ctx)
	return map[string]error{
		"Devices":              devErr,
		"SetAuthorized":        c.SetAuthorized(ctx, "n1", true),
		"SetTags":              c.SetTags(ctx, "n1", []string{"tag:a"}),
		"SetKeyExpiryDisabled": c.SetKeyExpiryDisabled(ctx, "n1", true),
		"SetRoutes":            c.SetRoutes(ctx, "n1", nil),
		"SetName":              c.SetName(ctx, "n1", "x"),
		"DeleteDevice":         c.DeleteDevice(ctx, "n1"),
	}
}

func TestNotConfigured(t *testing.T) {
	env := newEnv(t, okDevices, func(o *Options) { o.APIKey = "" })
	if env.c.Configured() {
		t.Fatal("expected not configured")
	}
	devs, err := env.c.Devices(context.Background())
	if devs != nil {
		t.Errorf("Devices returned %v, want nil", devs)
	}
	if !errors.Is(err, source.ErrNotConfigured) {
		t.Errorf("Devices err = %v, want ErrNotConfigured", err)
	}
	for name, err := range allMethods(env.c) {
		if !errors.Is(err, source.ErrNotConfigured) {
			t.Errorf("%s: err = %v, want ErrNotConfigured", name, err)
		}
	}
	if n := env.rec.count(""); n != 0 {
		t.Errorf("unconfigured client made %d requests", n)
	}
}

func TestInvalidBaseURLAndCredentials(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		want string
	}{
		{"unparseable", Options{BaseURL: "http://[::1", APIKey: "k"}, "invalid base URL"},
		{"no scheme", Options{BaseURL: "api.tailscale.com", APIKey: "k"}, "invalid base URL"},
		{"ftp scheme", Options{BaseURL: "ftp://x", APIKey: "k"}, "invalid base URL"},
		{"key with newline", Options{BaseURL: "https://x", APIKey: "abc\ndef"}, "API key contains"},
		{"oauth secret with tab", Options{BaseURL: "https://x", OAuthClientID: "id", OAuthClientSecret: "se\tcret"}, "OAuth client credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.o, testLogger())
			if !c.Configured() {
				t.Fatal("client should count as configured")
			}
			for name, err := range allMethods(c) {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("%s: err = %v, want containing %q", name, err, tc.want)
				}
				if errors.Is(err, source.ErrNotConfigured) {
					t.Errorf("%s: should not be ErrNotConfigured", name)
				}
				if strings.Contains(err.Error(), "abc") || strings.Contains(err.Error(), "cret") {
					t.Errorf("%s: error leaks credential: %v", name, err)
				}
			}
		})
	}
}

// --- API key auth / request shape ----------------------------------------

func TestAPIKeyRequestShape(t *testing.T) {
	env := newEnv(t, okDevices, func(o *Options) { o.Tailnet = "corp.example.com" })
	devs, err := env.c.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if devs == nil || len(devs) != 0 {
		t.Errorf("devices = %#v, want empty non-nil", devs)
	}
	reqs := env.rec.all()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet || r.Path != "/api/v2/tailnet/corp.example.com/devices" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	if r.RawQuery != "fields=all" {
		t.Errorf("query = %q, want fields=all", r.RawQuery)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer tskey-api-TESTKEY" {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.Header.Get("User-Agent"); got != "tailwatch/test" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
}

func TestTailnetPathEscaping(t *testing.T) {
	env := newEnv(t, okDevices, func(o *Options) { o.Tailnet = "weird/name?x" })
	if _, err := env.c.Devices(context.Background()); err != nil {
		t.Fatalf("Devices: %v", err)
	}
	r := env.rec.all()[0]
	if r.Path != "/api/v2/tailnet/weird/name?x/devices" && r.Path != "/api/v2/tailnet/weird%2Fname%3Fx/devices" {
		// httptest decodes the path; what matters is that the query is intact
		// and nothing was interpreted as a separate segment/query.
		t.Errorf("path = %q", r.Path)
	}
	if r.RawQuery != "fields=all" {
		t.Errorf("query = %q", r.RawQuery)
	}
}

// --- errors ----------------------------------------------------------------

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantNF     bool
		wantMsg    string
		wantInText []string
	}{
		{"404 wraps ErrNotFound", 404, `{"message":"device not found"}`, true, "device not found", []string{"not found", "404", "device not found"}},
		{"403 with message", 403, `{"message":"insufficient permissions"}`, false, "insufficient permissions", []string{"403", "insufficient permissions"}},
		{"400 non-json body", 400, `<html>nope</html>`, false, "", []string{"400 Bad Request"}},
		{"401 empty body", 401, ``, false, "", []string{"401 Unauthorized"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, tc.status, tc.body)
			}, nil)
			_, err := env.c.Devices(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if errors.Is(err, source.ErrNotFound) != tc.wantNF {
				t.Errorf("errors.Is(ErrNotFound) = %v, want %v (err=%v)", !tc.wantNF, tc.wantNF, err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v does not wrap *APIError", err)
			}
			if apiErr.Status != tc.status || apiErr.Message != tc.wantMsg {
				t.Errorf("APIError = %+v, want status %d msg %q", apiErr, tc.status, tc.wantMsg)
			}
			for _, s := range tc.wantInText {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q lacks %q", err.Error(), s)
				}
			}
			if !strings.HasPrefix(err.Error(), "tsapi: devices: ") {
				t.Errorf("error %q lacks operation prefix", err.Error())
			}
			if n := env.sleep.all(); len(n) != 0 {
				t.Errorf("4xx must not be retried, slept %v", n)
			}
		})
	}
}

func TestAPIErrorString(t *testing.T) {
	cases := []struct {
		e    APIError
		want string
	}{
		{APIError{404, "device not found"}, "tailscale api: 404 Not Found: device not found"},
		{APIError{418, ""}, "tailscale api: 418 I'm a teapot"},
		{APIError{599, "boom"}, "tailscale api: status 599: boom"},
		{APIError{599, ""}, "tailscale api: status 599"},
	}
	for _, tc := range cases {
		if got := tc.e.Error(); got != tc.want {
			t.Errorf("%+v.Error() = %q, want %q", tc.e, got, tc.want)
		}
	}
}

func TestParseMessage(t *testing.T) {
	long := strings.Repeat("x", 600)
	cases := []struct {
		name, body, want string
	}{
		{"message", `{"message":"device not found"}`, "device not found"},
		{"rfc6749", `{"error":"invalid_client","error_description":"bad secret"}`, "bad secret"},
		{"rfc6749 code only", `{"error":"invalid_client"}`, "invalid_client"},
		{"nested error object", `{"error":{"code":"x","message":"nested"}}`, "nested"},
		{"not json", `not json`, ""},
		{"empty", ``, ""},
		{"whitespace", "  \n ", ""},
		{"control chars stripped", "{\"message\":\"line1\\nline2\\u0007  tail\"}", "line1 line2 tail"},
		{"non-string message", `{"message":42}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseMessage([]byte(tc.body)); got != tc.want {
				t.Errorf("parseMessage = %q, want %q", got, tc.want)
			}
		})
	}
	got := parseMessage([]byte(`{"message":"` + long + `"}`))
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != maxErrorMessageLen+1 {
		t.Errorf("long message not truncated: len=%d", len([]rune(got)))
	}
}

// --- retry -----------------------------------------------------------------

func TestRetryPolicy(t *testing.T) {
	base := newFakeClock().now()
	cases := []struct {
		name       string
		statuses   []int // response per attempt; last repeats
		retryAfter string
		wantDelays []time.Duration
		wantStatus int // 0 = success expected
	}{
		{"429 retry-after seconds", []int{429, 200}, "3", []time.Duration{3 * time.Second}, 0},
		{"503 no header uses minimum", []int{503, 200}, "", []time.Duration{time.Second}, 0},
		{"500 capped at 10s", []int{500, 200}, "30", []time.Duration{10 * time.Second}, 0},
		{"429 zero header uses minimum", []int{429, 200}, "0", []time.Duration{time.Second}, 0},
		{"429 negative header", []int{429, 200}, "-5", []time.Duration{time.Second}, 0},
		{"429 garbage header", []int{429, 200}, "soon", []time.Duration{time.Second}, 0},
		{"429 http-date", []int{429, 200}, base.Add(5 * time.Second).UTC().Format(http.TimeFormat), []time.Duration{5 * time.Second}, 0},
		{"429 http-date in past", []int{429, 200}, base.Add(-5 * time.Second).UTC().Format(http.TimeFormat), []time.Duration{time.Second}, 0},
		{"two failures give up", []int{500, 502}, "", []time.Duration{time.Second}, 502},
		{"429 twice", []int{429, 429, 200}, "2", []time.Duration{2 * time.Second}, 429},
		{"400 not retried", []int{400}, "", nil, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			n := 0
			env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				i := n
				n++
				mu.Unlock()
				if i >= len(tc.statuses) {
					i = len(tc.statuses) - 1
				}
				st := tc.statuses[i]
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				if st == 200 {
					okDevices(w, r)
					return
				}
				jsonStatus(w, st, `{"message":"try later"}`)
			}, nil)
			_, err := env.c.Devices(context.Background())
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Status != tc.wantStatus {
					t.Fatalf("err = %v, want APIError status %d", err, tc.wantStatus)
				}
			}
			got := env.sleep.all()
			if len(got) != len(tc.wantDelays) {
				t.Fatalf("delays = %v, want %v", got, tc.wantDelays)
			}
			for i := range got {
				if got[i] != tc.wantDelays[i] {
					t.Errorf("delay[%d] = %v, want %v", i, got[i], tc.wantDelays[i])
				}
			}
			wantReqs := len(tc.wantDelays) + 1
			if n := env.rec.count(""); n != wantReqs {
				t.Errorf("requests = %d, want %d", n, wantReqs)
			}
		})
	}
}

func TestRetryReplaysBody(t *testing.T) {
	first := true
	var mu sync.Mutex
	env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		f := first
		first = false
		mu.Unlock()
		if f {
			jsonStatus(w, 503, `{"message":"maintenance"}`)
			return
		}
		jsonStatus(w, 200, `{}`)
	}, nil)
	if err := env.c.SetTags(context.Background(), "n1", []string{"tag:web"}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	reqs := env.rec.all()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	for i, r := range reqs {
		if r.Method != http.MethodPost || r.Path != "/api/v2/device/n1/tags" {
			t.Errorf("req[%d] = %s %s", i, r.Method, r.Path)
		}
		if r.Body != `{"tags":["tag:web"]}` {
			t.Errorf("req[%d] body = %q", i, r.Body)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("req[%d] content-type = %q", i, ct)
		}
	}
}

func TestRetryHonoursContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		cancel() // cancelled while the first response is in flight
		jsonStatus(w, 503, `{"message":"maintenance"}`)
	}, nil)
	env.c.sleep = sleepCtx // real sleep: must return promptly on cancellation
	start := time.Now()
	_, err := env.c.Devices(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("retry waited despite cancelled context")
	}
	if n := env.rec.count(""); n != 1 {
		t.Errorf("requests = %d, want 1 (no retry after cancel)", n)
	}
}

func TestRetryDelay(t *testing.T) {
	now := newFakeClock().now()
	cases := []struct {
		header string
		want   time.Duration
	}{
		{"", time.Second},
		{"0", time.Second},
		{"1", time.Second},
		{"4", 4 * time.Second},
		{"10", 10 * time.Second},
		{"11", 10 * time.Second},
		{"99999999999999999999", 10 * time.Second},
		{"-3", time.Second},
		{"abc", time.Second},
		{now.Add(7 * time.Second).UTC().Format(http.TimeFormat), 7 * time.Second},
		{now.Add(time.Hour).UTC().Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-time.Hour).UTC().Format(http.TimeFormat), time.Second},
	}
	for _, tc := range cases {
		if got := retryDelay(tc.header, now); got != tc.want {
			t.Errorf("retryDelay(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestResponseTooLarge(t *testing.T) {
	env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		jsonStatus(w, 200, `{"devices":[`+strings.Repeat(`{"id":"1"},`, 20)+`{"id":"2"}]}`)
	}, nil)
	env.c.maxBody = 64
	_, err := env.c.Devices(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds 64 bytes") {
		t.Fatalf("err = %v, want body size error", err)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepCtx: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx cancelled: %v", err)
	}
}

// --- redaction -------------------------------------------------------------

// echoAuthTransport fails every request with an error that quotes the
// Authorization header, the way a misbehaving proxy or debug wrapper might.
type echoAuthTransport struct {
	wrap error // optional sentinel to wrap
	pass http.RoundTripper
	only string // when set, only this path fails; others go to pass
}

func (e echoAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if e.only != "" && req.URL.Path != e.only && e.pass != nil {
		return e.pass.RoundTrip(req)
	}
	msg := fmt.Sprintf("proxy rejected request with Authorization: %s", req.Header.Get("Authorization"))
	if e.wrap != nil {
		return nil, fmt.Errorf("%s: %w", msg, e.wrap)
	}
	return nil, errors.New(msg)
}

func TestRedactsAuthorizationInErrors(t *testing.T) {
	const key = "tskey-api-SUPERSECRET-1234"
	t.Run("api key", func(t *testing.T) {
		c := New(Options{
			BaseURL:    "https://api.example.test",
			APIKey:     key,
			HTTPClient: &http.Client{Transport: echoAuthTransport{wrap: context.DeadlineExceeded}},
		}, testLogger())
		for name, err := range allMethods(c) {
			if err == nil {
				t.Fatalf("%s: expected error", name)
			}
			s := err.Error()
			if strings.Contains(s, key) || strings.Contains(s, "SUPERSECRET") {
				t.Errorf("%s: error leaks api key: %q", name, s)
			}
			if !strings.Contains(s, "[REDACTED]") {
				t.Errorf("%s: error not marked redacted: %q", name, s)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("%s: redaction lost the context sentinel: %v", name, err)
			}
		}
	})

	t.Run("oauth token", func(t *testing.T) {
		const token = "tsoauth-ACCESS-TOKEN-9876"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jsonStatus(w, 200, `{"access_token":"`+token+`","token_type":"bearer","expires_in":3600}`)
		}))
		defer srv.Close()
		c := New(Options{
			BaseURL:           srv.URL,
			OAuthClientID:     "client-id",
			OAuthClientSecret: "client-SECRET",
			HTTPClient: &http.Client{Transport: echoAuthTransport{
				only: "/api/v2/tailnet/-/devices", pass: http.DefaultTransport,
			}},
		}, testLogger())
		_, err := c.Devices(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		s := err.Error()
		if strings.Contains(s, token) || strings.Contains(s, "ACCESS-TOKEN") {
			t.Errorf("error leaks access token: %q", s)
		}
		if !strings.Contains(s, "[REDACTED]") {
			t.Errorf("error not marked redacted: %q", s)
		}
	})
}

func TestRedactString(t *testing.T) {
	c := New(Options{APIKey: "tskey-api-abc123", OAuthClientID: "cid", OAuthClientSecret: "tskey-client-xyz"}, testLogger())
	c.tok, c.tokExp = "tok-live", c.now().Add(time.Hour)
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"Authorization: Bearer tskey-api-abc123", "Authorization: Bearer [REDACTED]"},
		{"got bearer something.else-1", "got bearer [REDACTED]"},
		{"Basic Y2lkOnRza2V5LWNsaWVudC14eXo=", "Basic [REDACTED]"},
		{"secret=tskey-client-xyz", "secret=[REDACTED]"},
		{"token tok-live in body", "token [REDACTED] in body"},
		{"Bearer", "Bearer"},
	}
	for _, tc := range cases {
		if got := c.redactString(tc.in); got != tc.want {
			t.Errorf("redactString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A token that has already expired is still scrubbed.
	c.tokExp = c.now().Add(-time.Hour)
	if got := c.redactString("expired tok-live"); got != "expired [REDACTED]" {
		t.Errorf("expired token not redacted: %q", got)
	}
	if err := c.redact(nil); err != nil {
		t.Errorf("redact(nil) = %v", err)
	}
	plain := errors.New("nothing to see")
	if err := c.redact(plain); err != plain {
		t.Errorf("clean errors must be returned unchanged")
	}
}
