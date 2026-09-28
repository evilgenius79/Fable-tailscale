package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
	deadline := time.Now().Add(30 * time.Second)
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
