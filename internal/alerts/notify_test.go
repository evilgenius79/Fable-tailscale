package alerts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// capture records the last request a test server received.
type capture struct {
	mu     sync.Mutex
	method string
	path   string
	header http.Header
	body   []byte
}

func (c *capture) handler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.method, c.path, c.header, c.body = r.Method, r.URL.RequestURI(), r.Header.Clone(), body
		c.mu.Unlock()
		w.WriteHeader(status)
	}
}

func sampleNotification(sev model.Severity, typ model.EventType) Notification {
	return Notification{
		Type: typ,
		Alert: model.Alert{
			ID: 7, RuleID: "device_offline", RuleType: model.RuleDeviceOffline,
			DeviceID: "nasid", DeviceName: "nas", State: model.AlertOpen, Severity: sev,
			Title: "nas is offline", Message: "Offline for 6m (last seen 12:04)",
			OpenedAt: base, UpdatedAt: base, Data: map[string]any{"offlineSeconds": 360},
		},
		Hub: model.HubInfo{Version: "1.2.3", SelfName: "hub", Tailnet: "example.com"},
	}
}

func TestWebhookSend(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusNoContent))
	defer srv.Close()

	t.Run("signed", func(t *testing.T) {
		n := NewWebhook(srv.URL+"/hook?token=SECRET", "s3cret", srv.Client())
		if n.Name() != "webhook" {
			t.Errorf("Name = %q", n.Name())
		}
		if err := n.Send(context.Background(), sampleNotification(model.SeverityCritical, model.EventAlertOpened)); err != nil {
			t.Fatalf("Send: %v", err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.method != http.MethodPost || c.path != "/hook?token=SECRET" {
			t.Errorf("request = %s %s", c.method, c.path)
		}
		if ct := c.header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if ua := c.header.Get("User-Agent"); ua != "tailwatch" {
			t.Errorf("User-Agent = %q", ua)
		}
		mac := hmac.New(sha256.New, []byte("s3cret"))
		mac.Write(c.body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := c.header.Get("X-Tailwatch-Signature"); got != want {
			t.Errorf("signature = %q, want %q", got, want)
		}
		var payload struct {
			Type  model.EventType `json:"type"`
			Alert model.Alert     `json:"alert"`
			Hub   model.HubInfo   `json:"hub"`
		}
		if err := json.Unmarshal(c.body, &payload); err != nil {
			t.Fatalf("body is not JSON: %v: %s", err, c.body)
		}
		if payload.Type != model.EventAlertOpened || payload.Alert.ID != 7 || payload.Alert.Title != "nas is offline" || payload.Hub.SelfName != "hub" {
			t.Errorf("payload = %+v", payload)
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(c.body, &raw)
		for _, k := range []string{"type", "alert", "hub"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("payload missing %q key", k)
			}
		}
	})
	t.Run("unsigned", func(t *testing.T) {
		n := NewWebhook(srv.URL, "", srv.Client())
		if err := n.Send(context.Background(), sampleNotification(model.SeverityInfo, model.EventAlertResolved)); err != nil {
			t.Fatalf("Send: %v", err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, ok := c.header["X-Tailwatch-Signature"]; ok {
			t.Error("signature header sent without a secret")
		}
	})
}

func TestSlackSend(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusOK))
	defer srv.Close()
	n := NewSlack(srv.URL+"/services/T0/B0/XXXX", srv.Client())
	if n.Name() != "slack" {
		t.Errorf("Name = %q", n.Name())
	}
	tests := []struct {
		name string
		n    Notification
		want string
	}{
		{"critical", sampleNotification(model.SeverityCritical, model.EventAlertOpened), "🚨 [critical] nas is offline — Offline for 6m (last seen 12:04)"},
		{"warning", sampleNotification(model.SeverityWarning, model.EventAlertOpened), "⚠️ [warning] nas is offline — Offline for 6m (last seen 12:04)"},
		{"info", sampleNotification(model.SeverityInfo, model.EventAlertOpened), "ℹ️ [info] nas is offline — Offline for 6m (last seen 12:04)"},
		{"resolved", sampleNotification(model.SeverityWarning, model.EventAlertResolved), "✅ [resolved] nas is offline — Offline for 6m (last seen 12:04)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := n.Send(context.Background(), tc.n); err != nil {
				t.Fatalf("Send: %v", err)
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if ct := c.header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(c.body, &body); err != nil {
				t.Fatalf("body: %v", err)
			}
			if body["text"] != tc.want || body["content"] != tc.want {
				t.Errorf("body = %v, want text/content %q", body, tc.want)
			}
		})
	}
	// Device names not already in the title are appended.
	x := sampleNotification(model.SeverityInfo, model.EventAlertOpened)
	x.Alert.Title = "Update available"
	if got := chatText(x); !strings.HasSuffix(got, " (nas)") {
		t.Errorf("chatText = %q, want device suffix", got)
	}
	x.Alert.Message = strings.Repeat("m", 5000)
	if got := chatText(x); len(got) > maxChatTextLen+len("…") {
		t.Errorf("chatText not truncated: %d bytes", len(got))
	}
}

func TestNtfySend(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusOK))
	defer srv.Close()
	tests := []struct {
		name         string
		n            Notification
		token        string
		wantPriority string
		wantTags     string
		wantTitle    string
	}{
		{"critical", sampleNotification(model.SeverityCritical, model.EventAlertOpened), "tk_123", "5", "rotating_light", "nas is offline"},
		{"warning", sampleNotification(model.SeverityWarning, model.EventAlertOpened), "", "4", "warning", "nas is offline"},
		{"info", sampleNotification(model.SeverityInfo, model.EventAlertOpened), "", "3", "information_source", "nas is offline"},
		{"resolved", sampleNotification(model.SeverityCritical, model.EventAlertResolved), "", "3", "white_check_mark", "Resolved: nas is offline"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := NewNtfy(srv.URL+"/mytopic", tc.token, srv.Client())
			if n.Name() != "ntfy" {
				t.Errorf("Name = %q", n.Name())
			}
			if err := n.Send(context.Background(), tc.n); err != nil {
				t.Fatalf("Send: %v", err)
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.path != "/mytopic" || c.method != http.MethodPost {
				t.Errorf("request = %s %s", c.method, c.path)
			}
			if string(c.body) != tc.n.Alert.Message {
				t.Errorf("body = %q, want the message", c.body)
			}
			if got := c.header.Get("Title"); got != tc.wantTitle {
				t.Errorf("Title = %q, want %q", got, tc.wantTitle)
			}
			if got := c.header.Get("Priority"); got != tc.wantPriority {
				t.Errorf("Priority = %q, want %q", got, tc.wantPriority)
			}
			if got := c.header.Get("Tags"); got != tc.wantTags {
				t.Errorf("Tags = %q, want %q", got, tc.wantTags)
			}
			if got := c.header.Get("Authorization"); (tc.token != "" && got != "Bearer "+tc.token) || (tc.token == "" && got != "") {
				t.Errorf("Authorization = %q", got)
			}
			if ua := c.header.Get("User-Agent"); ua != "tailwatch" {
				t.Errorf("User-Agent = %q", ua)
			}
		})
	}
	t.Run("non-ascii title is encoded", func(t *testing.T) {
		n := NewNtfy(srv.URL+"/t", "", srv.Client())
		x := sampleNotification(model.SeverityWarning, model.EventAlertOpened)
		x.Alert.Title = "Température élevée\r\nInjected: yes"
		if err := n.Send(context.Background(), x); err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		title := c.header.Get("Title")
		if !strings.HasPrefix(title, "=?utf-8?q?") || strings.ContainsAny(title, "\r\n") {
			t.Errorf("Title = %q, want RFC 2047 encoded without CR/LF", title)
		}
		if _, ok := c.header["Injected"]; ok {
			t.Error("header injection through the title")
		}
	})
}

func TestNotifierErrorsAreRedacted(t *testing.T) {
	t.Run("non-2xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope: "+r.URL.String(), http.StatusForbidden)
		}))
		defer srv.Close()
		for _, n := range []Notifier{
			NewWebhook(srv.URL+"/hook/SECRETPATH?token=SECRETQ", "k", srv.Client()),
			NewSlack(srv.URL+"/services/SECRETPATH", srv.Client()),
			NewNtfy(srv.URL+"/topic?auth=SECRETQ", "SECRETTOKEN", srv.Client()),
		} {
			err := n.Send(context.Background(), sampleNotification(model.SeverityInfo, model.EventAlertOpened))
			if err == nil {
				t.Fatalf("%s: expected error for 403", n.Name())
			}
			msg := err.Error()
			if !strings.Contains(msg, "status 403") || !strings.HasPrefix(msg, n.Name()+": ") {
				t.Errorf("%s: error = %q, want name prefix and status", n.Name(), msg)
			}
			for _, secret := range []string{"SECRETPATH", "SECRETQ", "SECRETTOKEN"} {
				if strings.Contains(msg, secret) {
					t.Errorf("%s: error leaks %q: %q", n.Name(), secret, msg)
				}
			}
		}
	})
	t.Run("transport error", func(t *testing.T) {
		// A listener that is closed immediately yields a connection refused.
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		n := NewWebhook("http://"+addr+"/hook/SECRETPATH?token=SECRETQ", "", nil)
		err = n.Send(context.Background(), sampleNotification(model.SeverityInfo, model.EventAlertOpened))
		if err == nil {
			t.Fatal("expected a transport error")
		}
		msg := err.Error()
		if strings.Contains(msg, "SECRET") {
			t.Errorf("error leaks the URL: %q", msg)
		}
		if !strings.Contains(msg, "webhook: post to http://"+addr) {
			t.Errorf("error = %q, want scheme+host only", msg)
		}
		var opErr *net.OpError
		if !errors.As(err, &opErr) {
			t.Errorf("underlying cause not preserved: %T", errors.Unwrap(err))
		}
	})
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		defer srv.Close()
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		n := NewNtfy(srv.URL+"/topic?auth=SECRETQ", "", &http.Client{})
		err := n.Send(ctx, sampleNotification(model.SeverityInfo, model.EventAlertOpened))
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v, want context.DeadlineExceeded in chain", err)
		}
		if strings.Contains(err.Error(), "SECRETQ") {
			t.Errorf("timeout error leaks the URL: %q", err)
		}
	})
	t.Run("invalid url", func(t *testing.T) {
		for _, raw := range []string{"", "not a url", "ftp://host/x", "http://", "://x", "http://user:SECRETPW@"} {
			for _, n := range []Notifier{NewWebhook(raw, "", nil), NewSlack(raw, nil), NewNtfy(raw, "", nil)} {
				err := n.Send(context.Background(), sampleNotification(model.SeverityInfo, model.EventAlertOpened))
				if err == nil {
					t.Errorf("%s: Send with URL %q must fail", n.Name(), raw)
					continue
				}
				if !strings.Contains(err.Error(), "invalid URL") || strings.Contains(err.Error(), "SECRETPW") {
					t.Errorf("%s: error = %q", n.Name(), err)
				}
			}
		}
	})
	t.Run("redirects are not followed", func(t *testing.T) {
		var hits int
		var mu sync.Mutex
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits++
			mu.Unlock()
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}))
		defer srv.Close()
		n := NewWebhook(srv.URL+"/hook", "", nil)
		err := n.Send(context.Background(), sampleNotification(model.SeverityInfo, model.EventAlertOpened))
		if err == nil || !strings.Contains(err.Error(), "status 302") {
			t.Errorf("error = %v, want status 302", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if hits != 1 {
			t.Errorf("server hit %d times; the redirect was followed", hits)
		}
	})
}

func TestEngineNotificationsUseRealNotifiers(t *testing.T) {
	// End-to-end: an alert opened by the engine reaches an HTTP webhook with
	// a valid signature, through the asynchronous worker pool.
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusOK))
	defer srv.Close()

	st := newStore(t)
	src := newFakeSource()
	clk := &fakeClock{t: base}
	e := newEngine(st, src, []Notifier{NewWebhook(srv.URL, "k", srv.Client())}, testLogger(), clk.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	waitFor(t, "subscription", func() bool { return src.ticks.Len() == 1 })

	d := dev("vm", "vm", true)
	d.UpdateAvailable = true
	src.ticks.Publish(model.Snapshot{Overview: model.Overview{Hub: src.hub}, Devices: []model.Device{d}})
	waitFor(t, "webhook delivery", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.body) > 0
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	mac := hmac.New(sha256.New, []byte("k"))
	mac.Write(c.body)
	if got, want := c.header.Get("X-Tailwatch-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
	var n Notification
	if err := json.Unmarshal(c.body, &n); err != nil || n.Type != model.EventAlertOpened || n.Alert.RuleID != "update_available" || n.Alert.DeviceName != "vm" {
		t.Errorf("delivered notification = %+v (%v)", n, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}
