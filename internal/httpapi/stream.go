package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// sseHeartbeat is the interval of the ": ping" comment frame.
const sseHeartbeat = 15 * time.Second

// sseHello is the payload of the hello event.
type sseHello struct {
	Identity *model.Identity `json:"identity"`
	Hub      model.HubInfo   `json:"hub"`
}

// handleStream serves the Server-Sent Events feed: a hello frame, then a
// tick after every collector poll, every event and every alert change,
// with a heartbeat comment every 15 seconds. The stream ends when the
// client disconnects or the server shuts down.
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
