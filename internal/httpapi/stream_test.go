package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// sseFrame is one parsed SSE frame (comments have event "" and data set
// to the comment text).
type sseFrame struct {
	event string
	data  string
}

// readFrame reads the next frame from an SSE body.
func readFrame(t *testing.T, br *bufio.Reader, timeout time.Duration) sseFrame {
	t.Helper()
	done := make(chan sseFrame, 1)
	errc := make(chan error, 1)
	go func() {
		var f sseFrame
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				errc <- err
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if f.event != "" || f.data != "" {
					done <- f
					return
				}
			case strings.HasPrefix(line, ":"):
				f.data = line
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data += strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	select {
	case f := <-done:
		return f
	case err := <-errc:
		t.Fatalf("read frame: %v", err)
	case <-time.After(timeout):
		t.Fatal("timed out waiting for an SSE frame")
	}
	return sseFrame{}
}

func TestStream(t *testing.T) {
	h := newHarness(t)
	h.srv.heartbeat = 30 * time.Millisecond
	h.srv.auth = alwaysViewer{}
	ts := httptest.NewServer(h.srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/stream", nil)
	// httptest's Host (127.0.0.1) is not one of the hub's names when the
	// configured listen address is a Tailscale IP; send the hub's name.
	req.Host = hostHeader
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for k, v := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-store", "X-Accel-Buffering": "no", "X-Frame-Options": "DENY"} {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	br := bufio.NewReader(resp.Body)

	hello := readFrame(t, br, 3*time.Second)
	if hello.event != "hello" {
		t.Fatalf("first frame = %+v, want hello", hello)
	}
	var hp struct {
		Identity model.Identity `json:"identity"`
		Hub      model.HubInfo  `json:"hub"`
	}
	if err := json.Unmarshal([]byte(hello.data), &hp); err != nil || hp.Identity.Login != "bob@example.com" || hp.Hub.SelfName != "hub" {
		t.Fatalf("hello payload = %s (%v)", hello.data, err)
	}

	// Heartbeat comment.
	if f := readFrame(t, br, 3*time.Second); f.data != ": ping" {
		t.Fatalf("expected heartbeat, got %+v", f)
	}

	// A collector tick.
	h.col.Ticks().Publish(h.col.Snapshot())
	var tick sseFrame
	for {
		tick = readFrame(t, br, 3*time.Second)
		if tick.event != "" {
			break
		}
	}
	if tick.event != "tick" {
		t.Fatalf("expected tick, got %+v", tick)
	}
	var snap model.Snapshot
	if err := json.Unmarshal([]byte(tick.data), &snap); err != nil || len(snap.Devices) != 3 {
		t.Fatalf("tick payload = %s (%v)", tick.data, err)
	}

	// An event and an alert change.
	h.col.Events().Publish(model.Event{ID: 7, Type: model.EventDeviceOffline, Title: "nas offline", DeviceID: deviceNAS})
	h.eng.Changes().Publish(model.Alert{ID: 9, RuleID: "device_offline", State: model.AlertOpen})
	seen := map[string]string{}
	for len(seen) < 2 {
		f := readFrame(t, br, 3*time.Second)
		if f.event != "" {
			seen[f.event] = f.data
		}
	}
	if !strings.Contains(seen["event"], `"id":7`) || !strings.Contains(seen["alert"], `"id":9`) {
		t.Errorf("frames = %v", seen)
	}

	// The stream ends when the server shuts down.
	close(h.srv.shutdown)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := br.ReadString('\n'); err != nil {
			return
		}
	}
	t.Fatal("stream did not close on shutdown")
}

func TestStreamRequiresAuth(t *testing.T) {
	h := newHarness(t)
	// The alert engine holds a tick subscription from construction, so
	// compare against the baseline rather than expecting zero.
	before := h.col.Ticks().Len()
	w := h.do("GET", "/api/v1/stream", nil, ipUnknown)
	expect(t, w, 401, codeUnauthorized)
	if got := h.col.Ticks().Len(); got != before {
		t.Errorf("unauthenticated stream subscribed to the tick bus (subscribers %d -> %d)", before, got)
	}
}

// TestStreamCap checks that concurrently open streams are capped per
// identity and in total, and that closed streams free their slot.
func TestStreamCap(t *testing.T) {
	h := newHarness(t)
	h.srv.auth = alwaysViewer{}
	h.srv.maxStreamsPerIdentity = 2
	h.srv.maxStreamsTotal = 3
	ts := httptest.NewServer(h.srv.Handler())
	defer ts.Close()

	open := func(ctx context.Context, remote string) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/stream", nil)
		req.Host = hostHeader
		// Distinct identities come from distinct source addresses under
		// alwaysViewer; X-Forwarded-For is not consulted, so bind the
		// client to a different loopback address per identity.
		tr := &http.Transport{DisableKeepAlives: true}
		if remote != "" {
			d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(remote)}}
			tr.DialContext = d.DialContext
		}
		resp, err := (&http.Client{Transport: tr}).Do(req)
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		return resp
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var opened []*http.Response
	for i := 0; i < 2; i++ {
		resp := open(ctx, "")
		if resp.StatusCode != 200 {
			t.Fatalf("stream %d status = %d", i, resp.StatusCode)
		}
		readFrame(t, bufio.NewReader(resp.Body), 3*time.Second) // hello: the slot is held
		opened = append(opened, resp)
	}
	third := open(ctx, "")
	if third.StatusCode != http.StatusTooManyRequests || third.Header.Get("Retry-After") == "" {
		t.Fatalf("third stream for one identity: status %d, Retry-After %q", third.StatusCode, third.Header.Get("Retry-After"))
	}
	var eb errorBody
	if err := json.NewDecoder(third.Body).Decode(&eb); err != nil || eb.Error.Code != codeRateLimited {
		t.Fatalf("third stream body: %+v (%v)", eb, err)
	}
	third.Body.Close()
	if perKey, total := h.srv.streams.open("bob@example.com|n-bob|127.0.0.1"); perKey != 2 || total != 2 {
		t.Fatalf("gauge = %d/%d, want 2/2", perKey, total)
	}

	// Another identity gets the last global slot, then the global cap bites.
	other := open(ctx, "127.0.0.2")
	if other.StatusCode == http.StatusTooManyRequests {
		t.Skip("127.0.0.2 is not bindable on this host")
	}
	if other.StatusCode != 200 {
		t.Fatalf("other identity status = %d", other.StatusCode)
	}
	readFrame(t, bufio.NewReader(other.Body), 3*time.Second)
	opened = append(opened, other)
	if fourth := open(ctx, "127.0.0.3"); fourth.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("beyond the global cap: status %d", fourth.StatusCode)
	} else {
		fourth.Body.Close()
	}

	// Closing streams frees their slots.
	cancel()
	for _, r := range opened {
		r.Body.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, total := h.srv.streams.open(""); total == 0 {
			break
		}
		if time.Now().After(deadline) {
			_, total := h.srv.streams.open("")
			t.Fatalf("%d streams still counted after close", total)
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp := open(context.Background(), "")
	if resp.StatusCode != 200 {
		t.Fatalf("stream after release: status %d", resp.StatusCode)
	}
	resp.Body.Close()
}
