package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

var fixedNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func newTestClient(t *testing.T, token string, timeout time.Duration) *Client {
	t.Helper()
	c := New(token, timeout, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.now = func() time.Time { return fixedNow }
	t.Cleanup(c.CloseIdleConnections)
	return c
}

// serve starts an httptest server and returns it plus the port to pass to
// Fetch (the server listens on 127.0.0.1).
func serve(t *testing.T, h http.HandlerFunc) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return srv, port
}

func sampleReport() agentproto.Report {
	return agentproto.Report{
		ProtocolVersion: agentproto.ProtocolVersion,
		AgentVersion:    "0.1.0",
		SampledAt:       fixedNow.Add(-10 * time.Second),
		Host: agentproto.Host{
			Hostname: "pi", OS: "linux", Platform: "debian", PlatformVersion: "12",
			Kernel: "6.1", Arch: "arm64", BootTime: fixedNow.Add(-72 * time.Hour), UptimeSeconds: 259200,
		},
		CPU:    agentproto.CPU{Percent: 12.5, PerCore: []float64{10, 15}, Count: 2, Model: "Cortex", Load1: 0.5, Load5: 0.4, Load15: 0.3},
		Memory: agentproto.Memory{Total: 8 << 30, Used: 2 << 30, Available: 6 << 30, Percent: 25},
		Disks:  []agentproto.Disk{{Mount: "/", FSType: "ext4", Total: 100, Used: 40, Percent: 40, Primary: true}},
		Net: agentproto.Net{
			Interfaces:  []agentproto.Interface{{Name: "eth0", RxBytes: 1, TxBytes: 2, Physical: true}},
			TailscaleIf: "tailscale0",
		},
		Temperatures: []agentproto.Temperature{{Sensor: "cpu", Celsius: 45}},
		Processes:    120,
		Tailscale:    &agentproto.TailscaleInfo{Version: "1.102.5", BackendState: "Running", IPs: []string{"100.64.0.7"}},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

func TestFetchSuccess(t *testing.T) {
	want := sampleReport()
	var gotPath, gotAccept, gotUA string
	_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAccept, gotUA = r.URL.Path, r.Header.Get("Accept"), r.Header.Get("User-Agent")
		writeJSON(t, w, want)
	})
	c := newTestClient(t, "", time.Second)
	got, err := c.Fetch(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotPath != agentproto.PathMetrics {
		t.Errorf("path = %q, want %q", gotPath, agentproto.PathMetrics)
	}
	if gotAccept != "application/json" || gotUA != UserAgent {
		t.Errorf("Accept=%q User-Agent=%q", gotAccept, gotUA)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("report mismatch:\n got %s\nwant %s", gotJSON, wantJSON)
	}
	if got.Tailscale == nil || got.Tailscale.IPs[0] != "100.64.0.7" {
		t.Errorf("Tailscale info = %+v", got.Tailscale)
	}
}

func TestFetchTokenHeader(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"token", "s3cret-token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var present bool
			_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(agentproto.HeaderToken)
				_, present = r.Header[agentproto.HeaderToken]
				writeJSON(t, w, sampleReport())
			})
			c := newTestClient(t, tc.token, time.Second)
			if _, err := c.Fetch(context.Background(), "127.0.0.1", port); err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if tc.token == "" && present {
				t.Errorf("token header sent when no token configured: %q", got)
			}
			if tc.token != "" && got != tc.token {
				t.Errorf("token header = %q, want %q", got, tc.token)
			}
		})
	}
}

func TestFetchValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(r *agentproto.Report)
		wantIs  error
		wantOK  bool
		rawBody string // when set, sent verbatim instead of the report
	}{
		{name: "current protocol", mutate: func(*agentproto.Report) {}, wantOK: true},
		{name: "newer minor accepted", mutate: func(r *agentproto.Report) { r.ProtocolVersion = "1.7" }, wantOK: true},
		{name: "wrong major", mutate: func(r *agentproto.Report) { r.ProtocolVersion = "2.0" }, wantIs: ErrProtocol},
		{name: "empty version", mutate: func(r *agentproto.Report) { r.ProtocolVersion = "" }, wantIs: ErrProtocol},
		{name: "garbage version", mutate: func(r *agentproto.Report) { r.ProtocolVersion = "abc" }, wantIs: ErrProtocol},
		{name: "zero sampledAt", mutate: func(r *agentproto.Report) { r.SampledAt = time.Time{} }, wantIs: ErrInvalidReport},
		{name: "sampledAt far future", mutate: func(r *agentproto.Report) { r.SampledAt = fixedNow.Add(2 * time.Hour) }, wantIs: ErrInvalidReport},
		{name: "sampledAt slightly ahead accepted", mutate: func(r *agentproto.Report) { r.SampledAt = fixedNow.Add(30 * time.Minute) }, wantOK: true},
		{name: "invalid json", rawBody: `{"protocolVersion": "1.0",`, wantIs: ErrInvalidReport},
		{name: "json but wrong shape", rawBody: `[1,2,3]`, wantIs: ErrInvalidReport},
		{name: "unknown fields tolerated", rawBody: `{"protocolVersion":"1.0","sampledAt":"2026-09-28T11:59:00Z","extra":{"x":1}}`, wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.rawBody != "" {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, tc.rawBody)
					return
				}
				rep := sampleReport()
				tc.mutate(&rep)
				writeJSON(t, w, rep)
			})
			c := newTestClient(t, "", time.Second)
			got, err := c.Fetch(context.Background(), "127.0.0.1", port)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got == nil {
					t.Fatal("nil report")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error wrapping %v, got report %+v", tc.wantIs, got)
			}
			if !errors.Is(err, tc.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.wantIs)
			}
			if IsUnreachable(err) {
				t.Errorf("validation error misclassified as unreachable: %v", err)
			}
		})
	}
}

func TestFetchContentType(t *testing.T) {
	tests := []struct {
		ct     string
		wantOK bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"Application/JSON", true},
		{"text/html", false},
		{"text/plain; charset=utf-8", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.ct), func(t *testing.T) {
			_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := json.Marshal(sampleReport())
				if tc.ct == "" {
					// Force an empty Content-Type header (Go would sniff one).
					w.Header()["Content-Type"] = nil
				} else {
					w.Header().Set("Content-Type", tc.ct)
				}
				w.Write(b)
			})
			c := newTestClient(t, "", time.Second)
			_, err := c.Fetch(context.Background(), "127.0.0.1", port)
			if tc.wantOK && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantOK {
				if !errors.Is(err, ErrInvalidReport) {
					t.Fatalf("err = %v, want ErrInvalidReport", err)
				}
				if !strings.Contains(err.Error(), "content type") {
					t.Errorf("error %q should mention content type", err)
				}
			}
		})
	}
}

func TestFetchOversizedBody(t *testing.T) {
	pad := strings.Repeat("A", MaxBodyBytes)
	body := `{"protocolVersion":"1.0","sampledAt":"2026-09-28T11:59:00Z","pad":"` + pad + `"}`
	tests := []struct {
		name          string
		contentLength bool
	}{
		{"declared content-length", true},
		{"chunked", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.contentLength {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
					io.WriteString(w, body)
					return
				}
				io.WriteString(w, body[:100])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				io.WriteString(w, body[100:])
			})
			c := newTestClient(t, "", 5*time.Second)
			_, err := c.Fetch(context.Background(), "127.0.0.1", port)
			if !errors.Is(err, ErrBodyTooLarge) {
				t.Fatalf("err = %v, want ErrBodyTooLarge", err)
			}
			if IsUnreachable(err) {
				t.Error("size error misclassified as unreachable")
			}
		})
	}
}

func TestFetchBodyAtLimitAccepted(t *testing.T) {
	prefix := `{"protocolVersion":"1.0","sampledAt":"2026-09-28T11:59:00Z","pad":"`
	suffix := `"}`
	body := prefix + strings.Repeat("A", MaxBodyBytes-len(prefix)-len(suffix)) + suffix
	if len(body) != MaxBodyBytes {
		t.Fatalf("test body is %d bytes", len(body))
	}
	_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	})
	c := newTestClient(t, "", 5*time.Second)
	if _, err := c.Fetch(context.Background(), "127.0.0.1", port); err != nil {
		t.Fatalf("body of exactly MaxBodyBytes rejected: %v", err)
	}
}

func TestFetchStatusErrors(t *testing.T) {
	tests := []struct {
		code       int
		body       string
		wantUnauth bool
		wantBody   string
	}{
		{http.StatusUnauthorized, "missing token\n", true, "missing token"},
		{http.StatusForbidden, "forbidden: alice@example.com not allowed\x00\x01", true, "forbidden: alice@example.com not allowed"},
		{http.StatusNotFound, "", false, ""},
		{http.StatusInternalServerError, strings.Repeat("x", 500), false, strings.Repeat("x", statusBodySnippet)},
		{http.StatusTooManyRequests, "slow down", false, "slow down"},
	}
	for _, tc := range tests {
		t.Run(strconv.Itoa(tc.code), func(t *testing.T) {
			_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tc.code)
				io.WriteString(w, tc.body)
			})
			const token = "s3cret-QmVhcm"
			c := newTestClient(t, token, time.Second)
			_, err := c.Fetch(context.Background(), "127.0.0.1", port)
			if err == nil {
				t.Fatal("expected error")
			}
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err %v is not a *StatusError", err)
			}
			if se.Code != tc.code {
				t.Errorf("Code = %d, want %d", se.Code, tc.code)
			}
			if se.Body != tc.wantBody {
				t.Errorf("Body = %q, want %q", se.Body, tc.wantBody)
			}
			if got := errors.Is(err, ErrUnauthorized); got != tc.wantUnauth {
				t.Errorf("errors.Is(ErrUnauthorized) = %v, want %v", got, tc.wantUnauth)
			}
			if IsUnreachable(err) {
				t.Error("status error misclassified as unreachable")
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("error text leaks the token: %q", err)
			}
		})
	}
}

func TestFetchTimeoutIsUnreachable(t *testing.T) {
	_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		// Never respond; return once the client gives up.
		<-r.Context().Done()
	})
	c := newTestClient(t, "", 150*time.Millisecond)
	start := time.Now()
	_, err := c.Fetch(context.Background(), "127.0.0.1", port)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !IsUnreachable(err) {
		t.Errorf("timeout not classified as unreachable: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Fetch took %v, timeout not honoured", d)
	}
}

func TestFetchCallerContextCancel(t *testing.T) {
	_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c := newTestClient(t, "", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := c.Fetch(ctx, "127.0.0.1", port)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if IsUnreachable(err) {
		t.Error("caller cancellation must not count as unreachable")
	}
}

func TestFetchConnectionRefusedIsUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	c := newTestClient(t, "", time.Second)
	_, err = c.Fetch(context.Background(), "127.0.0.1", port)
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !IsUnreachable(err) {
		t.Errorf("connection refused not classified as unreachable: %v", err)
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrProtocol) {
		t.Errorf("unexpected classification: %v", err)
	}
}

func TestFetchRedirectRefused(t *testing.T) {
	var followed atomic.Int32
	_, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Add(1)
			writeJSON(t, w, sampleReport())
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	c := newTestClient(t, "", time.Second)
	_, err := c.Fetch(context.Background(), "127.0.0.1", port)
	if !errors.Is(err, ErrRedirect) {
		t.Fatalf("err = %v, want ErrRedirect", err)
	}
	if n := followed.Load(); n != 0 {
		t.Errorf("redirect target was requested %d times", n)
	}
	if IsUnreachable(err) {
		t.Error("redirect misclassified as unreachable")
	}
}

func TestFetchBadInput(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		port int
	}{
		{"hostname", "agent.example.ts.net", 41820},
		{"empty ip", "", 41820},
		{"ip with port", "100.64.0.1:41820", 41820},
		{"negative port", "100.64.0.1", -1},
		{"port too large", "100.64.0.1", 70000},
		{"url injection", "100.64.0.1/../../etc", 41820},
	}
	c := newTestClient(t, "", time.Second)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), tc.ip, tc.port)
			if err == nil {
				t.Fatal("expected error")
			}
			if IsUnreachable(err) {
				t.Errorf("input error misclassified as unreachable: %v", err)
			}
		})
	}
}

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		ip   string
		port int
		want string
	}{
		{"100.64.0.5", 41820, "http://100.64.0.5:41820/v1/metrics"},
		{"100.64.0.5", 0, "http://100.64.0.5:41820/v1/metrics"},
		{" 100.64.0.5 ", 8080, "http://100.64.0.5:8080/v1/metrics"},
		{"fd7a:115c:a1e0::1", 41820, "http://[fd7a:115c:a1e0::1]:41820/v1/metrics"},
		{"::ffff:100.64.0.5", 41820, "http://100.64.0.5:41820/v1/metrics"},
		{"fe80::1%eth0", 41820, "http://[fe80::1%25eth0]:41820/v1/metrics"},
		{"nope", 1, ""},
		{"1.2.3.4", 65536, ""},
	}
	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			got, err := endpointURL(tc.ip, tc.port)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestIsUnreachable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline", context.DeadlineExceeded, true},
		{"wrapped deadline", fmt.Errorf("x: %w", context.DeadlineExceeded), true},
		{"canceled", context.Canceled, false},
		{"net timeout", timeoutErr{}, true},
		{"dial op error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("whatever")}, true},
		{"read op error without errno", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("whatever")}, false},
		{"econnrefused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"ehostunreach", &net.OpError{Op: "read", Err: syscall.EHOSTUNREACH}, true},
		{"enetunreach", syscall.ENETUNREACH, true},
		{"econnreset", fmt.Errorf("read: %w", syscall.ECONNRESET), true},
		{"etimedout", syscall.ETIMEDOUT, true},
		{"status error", &StatusError{Code: 403}, false},
		{"unauthorized", ErrUnauthorized, false},
		{"protocol", fmt.Errorf("x: %w", ErrProtocol), false},
		{"invalid report", ErrInvalidReport, false},
		{"body too large", ErrBodyTooLarge, false},
		{"redirect", ErrRedirect, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUnreachable(tc.err); got != tc.want {
				t.Errorf("IsUnreachable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestValidateReport(t *testing.T) {
	ok := sampleReport()
	tests := []struct {
		name   string
		rep    *agentproto.Report
		wantIs error
	}{
		{"nil", nil, ErrInvalidReport},
		{"ok", &ok, nil},
		{"major mismatch", &agentproto.Report{ProtocolVersion: "0.9", SampledAt: fixedNow}, ErrProtocol},
		{"v prefix accepted", &agentproto.Report{ProtocolVersion: "v1.0", SampledAt: fixedNow}, nil},
		{"negative major", &agentproto.Report{ProtocolVersion: "-1.0", SampledAt: fixedNow}, ErrProtocol},
		{"missing sampledAt", &agentproto.Report{ProtocolVersion: "1.0"}, ErrInvalidReport},
		{"exactly at skew limit", &agentproto.Report{ProtocolVersion: "1.0", SampledAt: fixedNow.Add(maxFutureSkew)}, nil},
		{"past skew limit", &agentproto.Report{ProtocolVersion: "1.0", SampledAt: fixedNow.Add(maxFutureSkew + time.Second)}, ErrInvalidReport},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateReport(tc.rep, fixedNow)
			if tc.wantIs == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantIs) {
				t.Errorf("err = %v, want %v", err, tc.wantIs)
			}
		})
	}
}

func TestParseProtocolMajor(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"1.0", 1, true},
		{"1", 1, true},
		{"12.34.56", 12, true},
		{" 2.1 ", 2, true},
		{"v3.0", 3, true},
		{"", 0, false},
		{".1", 0, false},
		{"x.1", 0, false},
		{"-1.0", 0, false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.in), func(t *testing.T) {
			got, err := ParseProtocolMajor(tc.in)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, ok = %v", err, tc.ok)
			}
			if tc.ok && got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestStatusError(t *testing.T) {
	if got := (&StatusError{Code: 500}).Error(); got != "agent returned HTTP 500" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&StatusError{Code: 403, Body: "nope"}).Error(); got != "agent returned HTTP 403: nope" {
		t.Errorf("Error() = %q", got)
	}
	if (&StatusError{Code: 500}).Unwrap() != nil {
		t.Error("500 should not unwrap to ErrUnauthorized")
	}
}

func TestNewDefaults(t *testing.T) {
	c := New("", 0, nil)
	if c.timeout != DefaultTimeout || c.hc.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v / %v, want %v", c.timeout, c.hc.Timeout, DefaultTimeout)
	}
	if c.log == nil {
		t.Error("nil logger not defaulted")
	}
	tr, ok := c.hc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.hc.Transport)
	}
	if tr.Proxy != nil {
		t.Error("transport must not use environment proxies")
	}
	if tr.MaxConnsPerHost != maxConnsPerHost || tr.ResponseHeaderTimeout != DefaultTimeout || tr.DisableKeepAlives {
		t.Errorf("transport = %+v", tr)
	}
}

func TestReadSnippet(t *testing.T) {
	got := readSnippet(strings.NewReader("  line1\nline2\ttab\x00\x07 \xff end  "), 100)
	if got != "line1 line2 tab  end" {
		t.Errorf("readSnippet = %q", got)
	}
	if got := readSnippet(strings.NewReader(strings.Repeat("a", 50)), 10); len(got) != 10 {
		t.Errorf("snippet length = %d, want 10", len(got))
	}
}
