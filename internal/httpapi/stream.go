package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

const (
	// sseHeartbeat is the interval of the ": ping" comment frame.
	sseHeartbeat = 15 * time.Second
	// maxStreamsPerIdentity and maxStreamsTotal cap concurrently open
	// streams. Each stream costs a goroutine, three bus subscriptions and a
	// full Snapshot encode per poll, and the connect rate limit alone would
	// let one viewer accumulate thousands of them.
	maxStreamsPerIdentity = 8
	maxStreamsTotal       = 256
)

// streamGauge counts open SSE streams per identity and in total.
type streamGauge struct {
	mu    sync.Mutex
	byKey map[string]int
	total int
}

// acquire reserves a slot for key, failing when either cap is reached.
func (g *streamGauge) acquire(key string, perKey, total int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.total >= total || g.byKey[key] >= perKey {
		return false
	}
	if g.byKey == nil {
		g.byKey = make(map[string]int)
	}
	g.byKey[key]++
	g.total++
	return true
}

// release frees a slot reserved by acquire.
func (g *streamGauge) release(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.total--
	if g.byKey[key] <= 1 {
		delete(g.byKey, key)
	} else {
		g.byKey[key]--
	}
}

// open returns the number of open streams for key and in total.
func (g *streamGauge) open(key string) (perKey, total int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.byKey[key], g.total
}

// sseHello is the payload of the hello event.
type sseHello struct {
	Identity *model.Identity `json:"identity"`
	Hub      model.HubInfo   `json:"hub"`
}

// handleStream serves the Server-Sent Events feed: a hello frame, then a
// tick after every collector poll, every event and every alert change,
// with a heartbeat comment every 15 seconds. The stream ends when the
// client disconnects or the server shuts down. At most
// maxStreamsPerIdentity streams per identity and maxStreamsTotal overall
// may be open; further connects get 429.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeInternal, "streaming is not supported by this connection")
		return
	}
	key := limiterKey(id)
	if !s.streams.acquire(key, s.maxStreamsPerIdentity, s.maxStreamsTotal) {
		perKey, total := s.streams.open(key)
		s.log.Info("httpapi: stream limit reached", "login", id.Login, "node", id.NodeName, "openForIdentity", perKey, "openTotal", total)
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, codeRateLimited, "too many open streams")
		return
	}
	defer s.streams.release(key)
	// The listener's ReadTimeout would otherwise cancel this request's
	// context after 30 seconds even though nothing is being read.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ticks, unsubTicks := s.d.Collector.Ticks().Subscribe()
	defer unsubTicks()
	events, unsubEvents := s.d.Collector.Events().Subscribe()
	defer unsubEvents()
	changes, unsubChanges := s.d.Alerts.Changes().Subscribe()
	defer unsubChanges()

	write := func(event string, v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			s.log.Error("httpapi: encode SSE payload", "event", event, "err", err)
			return true
		}
		if _, err := w.Write([]byte("event: " + event + "\ndata: " + string(b) + "\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !write("hello", sseHello{Identity: id, Hub: s.d.Collector.Hub()}) {
		return
	}

	heartbeat := time.NewTicker(s.heartbeat)
	defer heartbeat.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdown:
			return
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case snap, ok := <-ticks:
			if !ok || !write("tick", snap) {
				return
			}
		case ev, ok := <-events:
			if !ok || !write("event", ev) {
				return
			}
		case a, ok := <-changes:
			if !ok || !write("alert", a) {
				return
			}
		}
	}
}
