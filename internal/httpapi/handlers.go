package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

const (
	// apiPrefix is the versioned API root.
	apiPrefix = "/api/v1"
	// defaultRange is the series/uptime range when none is given.
	defaultRange = 24 * time.Hour
	// maxRange bounds the series/uptime range.
	maxRange = 90 * 24 * time.Hour
	// minRange is the smallest accepted range.
	minRange = time.Minute
	// pingTimeout bounds an on-demand ping.
	pingTimeout = 10 * time.Second
	// detailEvents and detailUptime shape the device detail response.
	detailEvents = 20
	detailUptime = 24 * time.Hour
	// maxListLimit caps every list endpoint.
	maxListLimit = 1000
)

// namedRanges are the documented shorthand range values.
var namedRanges = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"3h":  3 * time.Hour,
	"6h":  6 * time.Hour,
	"12h": 12 * time.Hour,
	"24h": 24 * time.Hour,
	"2d":  48 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"14d": 14 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// apiMux builds the versioned API router. Every route except the SSE
// stream is wrapped in the handler timeout; unmatched paths and methods get
// a JSON 404/405.
func (s *Server) apiMux() http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.timeout(h))
	}
	admin := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.timeout(requireAdmin(h)))
	}

	route("GET "+apiPrefix+"/me", s.handleMe)
	route("GET "+apiPrefix+"/overview", s.handleOverview)
	route("GET "+apiPrefix+"/settings", s.handleSettings)
	route("GET "+apiPrefix+"/devices", s.handleDevices)
	route("GET "+apiPrefix+"/devices/{id}", s.handleDevice)
	route("GET "+apiPrefix+"/devices/{id}/series", s.handleSeries)
	route("GET "+apiPrefix+"/devices/{id}/uptime", s.handleUptime)
	route("GET "+apiPrefix+"/devices/{id}/events", s.handleDeviceEvents)
	route("POST "+apiPrefix+"/devices/{id}/ping", s.handlePing)
	admin("POST "+apiPrefix+"/devices/{id}/authorize", s.handleAuthorize)
	admin("POST "+apiPrefix+"/devices/{id}/tags", s.handleTags)
	admin("POST "+apiPrefix+"/devices/{id}/key-expiry", s.handleKeyExpiry)
	admin("POST "+apiPrefix+"/devices/{id}/routes", s.handleRoutes)
	admin("POST "+apiPrefix+"/devices/{id}/name", s.handleName)
	admin("DELETE "+apiPrefix+"/devices/{id}", s.handleDeleteDevice)
	route("GET "+apiPrefix+"/events", s.handleEvents)
	route("GET "+apiPrefix+"/alerts", s.handleAlerts)
	route("GET "+apiPrefix+"/alerts/rules", s.handleRules)
	admin("PUT "+apiPrefix+"/alerts/rules/{id}", s.handleSaveRule)
	admin("POST "+apiPrefix+"/alerts/test", s.handleAlertTest)
	admin("POST "+apiPrefix+"/alerts/{id}/ack", s.handleAck)
	route("GET "+apiPrefix+"/network/topology", s.handleTopology)
	admin("GET "+apiPrefix+"/audit", s.handleAudit)
	admin("POST "+apiPrefix+"/refresh", s.handleRefresh)
	mux.Handle("GET "+apiPrefix+"/stream", http.HandlerFunc(s.handleStream))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			// Serve through the mux so it binds the path values.
			mux.ServeHTTP(w, r)
			return
		}
		// No pattern: the mux would answer 404 or 405 in plain text. Run its
		// handler against a header-only recorder to learn which, then write
		// the JSON envelope (keeping the Allow header for 405).
		rec := &probeWriter{header: http.Header{}}
		h.ServeHTTP(rec, r)
		if rec.status == http.StatusMethodNotAllowed {
			if allow := rec.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			writeError(w, http.StatusMethodNotAllowed, codeNotFound, "method not allowed")
			return
		}
		writeError(w, http.StatusNotFound, codeNotFound, "no such endpoint")
	})
}

// probeWriter captures the status and headers the mux writes for an
// unmatched request; the body is discarded.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header         { return p.header }
func (p *probeWriter) WriteHeader(code int)        { p.status = code }
func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }

// identity returns the caller's identity; the auth middleware guarantees
// it is present, so a missing identity is a programming error mapped to
// 401 defensively.
func (s *Server) identity(w http.ResponseWriter, r *http.Request) (*model.Identity, bool) {
	id, ok := IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "request could not be attributed to a tailnet identity")
		return nil, false
	}
	return id, true
}

// device resolves the {id} path value (stable node ID or MagicDNS base
// name) to a device, answering 404 when unknown.
func (s *Server) device(w http.ResponseWriter, r *http.Request) (model.Device, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" || len(raw) > 255 {
		writeError(w, http.StatusBadRequest, codeBadRequest, "invalid device id")
		return model.Device{}, false
	}
	d, ok := s.d.Collector.Device(model.DeviceID(raw))
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "device not found")
		return model.Device{}, false
	}
	return d, true
}

// parseRange parses the range query parameter (named value, Go duration or
// day suffix) and bounds it to [1m, 90d]; empty means 24h.
func parseRange(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultRange, nil
	}
	d, ok := namedRanges[raw]
	if !ok {
		var err error
		d, err = config.ParseDuration(raw)
		if err != nil {
			return 0, badRequest("invalid range %q: use 15m, 1h, 24h, 7d, ... or a duration up to 90d", raw)
		}
	}
	if d < minRange {
		return 0, badRequest("range must be at least %s", minRange)
	}
	if d > maxRange {
		return 0, badRequest("range must be at most 90d")
	}
	return d, nil
}

// parseLimit parses an optional positive integer query parameter and
// clamps it to [1, max]; empty means def.
func parseLimit(r *http.Request, def int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, badRequest("limit must be a non-negative integer")
	}
	if n == 0 {
		return def, nil
	}
	if n > maxListLimit {
		n = maxListLimit
	}
	return n, nil
}

// parseTime parses an optional RFC 3339 query parameter.
func parseTime(r *http.Request, name string) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, raw)
	}
	if err != nil {
		return time.Time{}, badRequest("%s must be an RFC 3339 timestamp", name)
	}
	return t, nil
}

// --- read endpoints -----------------------------------------------------------

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, id)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Collector.Snapshot().Overview)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	stats, err := s.d.Store.Stats(r.Context())
	if err != nil {
		if r.Context().Err() == nil {
			s.log.Warn("httpapi: store stats failed; reporting empty stats", "err", err)
		}
		stats = model.StoreStats{}
	}
	writeJSON(w, http.StatusOK, s.cfg.Settings(stats))
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	devs := s.d.Collector.Snapshot().Devices
	sort.SliceStable(devs, func(i, j int) bool {
		a, b := strings.ToLower(devs[i].Name), strings.ToLower(devs[j].Name)
		if a != b {
			return a < b
		}
		return devs[i].ID < devs[j].ID
	})
	writeJSON(w, http.StatusOK, devs)
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.device(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	events, err := s.d.Store.ListEvents(ctx, model.EventQuery{DeviceID: dev.ID, Limit: detailEvents})
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	open, err := s.d.Store.ListAlerts(ctx, model.AlertQuery{State: model.AlertOpen, DeviceID: dev.ID})
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	now := s.now()
	uptime, err := s.d.Store.OnlineTimeline(ctx, dev.ID, now.Add(-detailUptime), now)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, model.DeviceDetail{
		Device:       dev,
		RecentEvents: events,
		OpenAlerts:   open,
		Uptime24h:    uptime,
	})
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.device(w, r)
	if !ok {
		return
	}
	rng, err := parseRange(r.URL.Query().Get("range"))
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	now := s.now()
	series, err := s.d.Store.QuerySeries(r.Context(), dev.ID, now.Add(-rng), now)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, series)
}

func (s *Server) handleUptime(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.device(w, r)
	if !ok {
		return
	}
	rng, err := parseRange(r.URL.Query().Get("range"))
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	now := s.now()
	rep, err := s.d.Store.OnlineTimeline(r.Context(), dev.ID, now.Add(-rng), now)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleDeviceEvents(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.device(w, r)
	if !ok {
		return
	}
	limit, err := parseLimit(r, 50)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	events, err := s.d.Store.ListEvents(r.Context(), model.EventQuery{DeviceID: dev.ID, Limit: limit})
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.device(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout+time.Second)
	defer cancel()
	res, err := s.d.Collector.PingNow(ctx, dev.ID)
	if err != nil {
		if errors.Is(err, source.ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "device not found")
			return
		}
		s.writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r, 100)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	since, err := parseTime(r, "since")
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	before, err := parseTime(r, "before")
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	q := model.EventQuery{Limit: limit, Since: since, Before: before}
	if dev := strings.TrimSpace(r.URL.Query().Get("device")); dev != "" {
		if d, ok := s.d.Collector.Device(model.DeviceID(dev)); ok {
			dev = string(d.ID)
		}
		q.DeviceID = model.DeviceID(dev)
	}
	if typ := strings.TrimSpace(r.URL.Query().Get("type")); typ != "" {
		for _, t := range strings.Split(typ, ",") {
			if t = strings.TrimSpace(t); t != "" {
				q.Types = append(q.Types, model.EventType(t))
			}
		}
	}
	events, err := s.d.Store.ListEvents(r.Context(), q)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r, 200)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	q := model.AlertQuery{Limit: limit}
	switch st := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state"))); st {
	case "", "all":
	case string(model.AlertOpen), string(model.AlertResolved):
		q.State = model.AlertState(st)
	default:
		writeError(w, http.StatusBadRequest, codeBadRequest, "state must be open, resolved or all")
		return
	}
	if dev := strings.TrimSpace(r.URL.Query().Get("device")); dev != "" {
		if d, ok := s.d.Collector.Device(model.DeviceID(dev)); ok {
			dev = string(d.ID)
		}
		q.DeviceID = model.DeviceID(dev)
	}
	alerts, err := s.d.Store.ListAlerts(r.Context(), q)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Alerts.Rules())
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Collector.Topology())
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r, 100)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	entries, err := s.d.Store.ListAudit(r.Context(), limit)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	s.d.Collector.RefreshNow()
	s.log.Info("httpapi: refresh requested", "login", id.Login, "node", id.NodeName)
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

// --- alert administration ----------------------------------------------------

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || alertID <= 0 {
		writeError(w, http.StatusBadRequest, codeBadRequest, "invalid alert id")
		return
	}
	a, err := s.d.Alerts.Ack(r.Context(), alertID, id.Login)
	s.audit(r, id, "alert.ack", strconv.FormatInt(alertID, 10), map[string]any{"alertId": alertID}, err)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleSaveRule(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	ruleID := strings.TrimSpace(r.PathValue("id"))
	if ruleID == "" || len(ruleID) > 100 {
		writeError(w, http.StatusBadRequest, codeBadRequest, "invalid rule id")
		return
	}
	var rule model.AlertRule
	if err := decodeJSON(r, &rule, false); err != nil {
		s.audit(r, id, "rule.update", ruleID, nil, err)
		s.writeAPIError(w, r, err)
		return
	}
	rule.ID = ruleID // the path wins over the body
	saved, err := s.d.Alerts.SaveRule(r.Context(), rule)
	details := ruleDetails(rule)
	if err == nil {
		details = ruleDetails(saved)
	}
	s.audit(r, id, "rule.update", ruleID, details, err)
	s.adminEvent(r, id, "rule.update", nil, ruleID, details, err)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// ruleDetails summarizes a rule for audit entries and events.
func ruleDetails(r model.AlertRule) map[string]any {
	return map[string]any{
		"ruleId":     r.ID,
		"type":       string(r.Type),
		"name":       r.Name,
		"enabled":    r.Enabled,
		"severity":   string(r.Severity),
		"threshold":  r.Threshold,
		"forSeconds": r.ForSeconds,
		"notify":     r.Notify,
	}
}

func (s *Server) handleAlertTest(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &body, true); err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	sent, errs := s.d.Alerts.Test(r.Context(), body.Message)
	var err error
	if len(errs) > 0 {
		err = errors.New("some notifiers failed")
	}
	s.audit(r, id, "alerts.test", "", map[string]any{"sent": sent, "errors": errs}, err)
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "errors": errs})
}

// --- audit & events -----------------------------------------------------------

// audit records an administrative attempt. err == nil means success.
// Store failures are logged, never surfaced.
func (s *Server) audit(r *http.Request, id *model.Identity, action, target string, details map[string]any, err error) {
	ip, _ := remoteIP(r.RemoteAddr)
	entry := &model.AuditEntry{
		TS:        s.now(),
		Actor:     id.Login,
		ActorNode: id.NodeName,
		Action:    action,
		Target:    target,
		Details:   details,
		OK:        err == nil,
		RemoteIP:  ip.String(),
	}
	if err != nil {
		entry.Error = truncateText(err.Error(), 500)
	}
	// Use a detached context so the entry is written even when the client
	// has already disconnected.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if ierr := s.d.Store.InsertAudit(ctx, entry); ierr != nil {
		s.log.Error("httpapi: audit write failed", "action", action, "target", target, "err", ierr)
	}
}

// adminEvent emits an admin.action event: info on success, warning on
// failure.
func (s *Server) adminEvent(r *http.Request, id *model.Identity, action string, dev *model.Device, target string, details map[string]any, err error) {
	data := map[string]any{
		"action":    action,
		"actor":     id.Login,
		"actorNode": id.NodeName,
		"target":    target,
		"ok":        err == nil,
	}
	for k, v := range details {
		if _, taken := data[k]; !taken {
			data[k] = v
		}
	}
	ev := model.Event{
		TS:       s.now(),
		Type:     model.EventAdminAction,
		Severity: model.SeverityInfo,
		Data:     data,
	}
	subject := target
	if dev != nil {
		ev.DeviceID = dev.ID
		ev.Device = dev.Name
		subject = dev.Name
	}
	verb := actionVerb(action)
	if err != nil {
		ev.Severity = model.SeverityWarning
		ev.Title = "Admin action failed: " + verb + " " + subject
		ev.Message = id.Login + " could not " + verb + " " + subject + ": " + truncateText(trimErrorPrefix(err.Error()), 200)
		data["error"] = truncateText(err.Error(), 300)
	} else {
		ev.Title = "Admin action: " + verb + " " + subject
		ev.Message = id.Login + " (" + id.NodeName + ") ran " + verb + " on " + subject
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	s.d.Collector.Emit(ctx, ev)
}

// actionVerb turns an audit action name into a short verb phrase.
func actionVerb(action string) string {
	switch action {
	case "device.authorize":
		return "authorize"
	case "device.tags":
		return "set tags on"
	case "device.key-expiry":
		return "set key expiry on"
	case "device.routes":
		return "set routes on"
	case "device.name":
		return "rename"
	case "device.delete":
		return "delete"
	case "rule.update":
		return "update rule"
	}
	return action
}
