package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/alerts"
	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

func noEnv(string) string { return "" }

func TestRunVersionAndHelp(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--version"}, noEnv, &out, &errb); code != exitOK {
		t.Fatalf("--version exit %d, stderr %q", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "tailwatch ") {
		t.Fatalf("unexpected version output %q", out.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"--help"}, noEnv, &out, &errb); code != exitOK {
		t.Fatalf("--help exit %d", code)
	}
	if !strings.Contains(out.String(), "--listen") {
		t.Fatalf("usage missing flags: %q", out.String())
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--poll-interval", "1s"}, noEnv, &out, &errb); code != exitUsage {
		t.Fatalf("expected usage exit, got %d (stderr %q)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "tailwatch:") {
		t.Fatalf("expected an error message, got %q", errb.String())
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestRunDemo boots the hub in demo mode on a loopback port, checks the API
// answers, then shuts it down cleanly via context cancellation.
func TestRunDemo(t *testing.T) {
	// A full day of demo backfill (~90k rows) takes tens of seconds under
	// the race detector; an hour is plenty to exercise the same code path.
	prev := demoBackfill
	demoBackfill = time.Hour
	t.Cleanup(func() { demoBackfill = prev })

	port := freePort(t)
	listen := fmt.Sprintf("127.0.0.1:%d", port)
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out, errb bytes.Buffer
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- run(ctx, []string{"--demo", "--listen", listen, "--data-dir", dir, "--log-level", "warn"}, noEnv, &out, &errb)
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + listen
	// Startup (store open, backfill, listen) and the first poll each get
	// their own generous budget; CI runners are slow and may run under -race.
	deadline := time.Now().Add(120 * time.Second)
	var healthy bool
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				healthy = true
				break
			}
		}
		select {
		case code := <-codeCh:
			t.Fatalf("hub exited early with code %d: %s", code, errb.String())
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !healthy {
		t.Fatalf("hub never became healthy; stderr: %s", errb.String())
	}

	// The HTTP server comes up concurrently with the collector's first poll,
	// so wait until the overview reports devices.
	deadline = time.Now().Add(60 * time.Second)
	var ov struct {
		Hub struct {
			DemoMode bool `json:"demoMode"`
		} `json:"hub"`
		Devices int `json:"devices"`
	}
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/api/v1/overview")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("overview status %d", resp.StatusCode)
		}
		if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			resp.Body.Close()
			t.Fatalf("missing CSP header, got %q", csp)
		}
		err = json.NewDecoder(resp.Body).Decode(&ov)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if ov.Devices > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ov.Hub.DemoMode || ov.Devices == 0 {
		t.Fatalf("unexpected overview after first poll: %+v", ov)
	}

	cancel()
	select {
	case code := <-codeCh:
		if code != exitOK {
			t.Fatalf("exit code %d, stderr: %s", code, errb.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("hub did not shut down after cancellation")
	}
}

// TestBuildNotifiersRefuseRedirects checks that the production notifier
// client never follows a redirect: a redirecting endpoint must not make the
// hub re-send the alert payload (or its signature/token) elsewhere.
func TestBuildNotifiersRefuseRedirects(t *testing.T) {
	var hits sync.Map
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Store(r.URL.Path, true)
		if r.URL.Path == "/leaked" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/leaked", http.StatusTemporaryRedirect)
	}))
	defer ts.Close()

	cfg := &config.Config{
		WebhookURL:      ts.URL + "/hook",
		WebhookSecret:   "s3cret",
		SlackWebhookURL: ts.URL + "/slack",
		NtfyURL:         ts.URL + "/ntfy",
		NtfyToken:       "tk_secret",
	}
	notifiers := buildNotifiers(cfg)
	if len(notifiers) != 3 {
		t.Fatalf("notifiers = %d, want 3", len(notifiers))
	}
	for _, n := range notifiers {
		err := n.Send(context.Background(), alerts.Notification{Type: model.EventAlertOpened})
		if err == nil || !strings.Contains(err.Error(), "307") {
			t.Errorf("%s: expected a status 307 error, got %v", n.Name(), err)
		}
	}
	if _, leaked := hits.Load("/leaked"); leaked {
		t.Fatal("a notifier followed the redirect and re-sent the payload")
	}
	for _, p := range []string{"/hook", "/slack", "/ntfy"} {
		if _, ok := hits.Load(p); !ok {
			t.Errorf("%s was never called", p)
		}
	}
}
