package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/bus"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// Engine evaluates alert rules against collector snapshots and manages the
// lifecycle of alerts. It is safe for concurrent use: Rules, SaveRule, Ack
// and Test may be called from HTTP handlers while Run is evaluating ticks.
type Engine struct {
	st        *store.Store
	src       Source
	notifiers []Notifier
	log       *slog.Logger
	changes   *bus.Bus[model.Alert]

	// now is the clock; tests override it.
	now func() time.Time

	// ticks and unsubscribe hold the tick subscription taken in NewEngine
	// (so the collector's first poll is never published before anyone
	// listens); Run consumes and releases them.
	ticks       <-chan model.Snapshot
	unsubscribe func()

	mu     sync.RWMutex
	loaded bool
	rules  map[string]model.AlertRule  // by rule ID
	open   map[string]*model.Alert     // open alerts by Alert.Key()
	since  map[string]time.Time        // condition start per key (pending and open)
	seen   map[model.DeviceID]struct{} // devices already known (new_device detection)
	queue  chan Notification           // bounded queue drained by the worker pool

	running   atomic.Bool
	processed atomic.Uint64 // ticks evaluated (tests)
}

// NewEngine creates an engine backed by st that evaluates the snapshots
// published by src and delivers notifications to notifiers. It loads the
// stored rules (persisting any missing built-in defaults so they can be
// tuned), the currently open alerts and the known devices so state survives
// restarts. Load failures are logged and retried by Run. A nil log uses
// slog.Default().
func NewEngine(st *store.Store, src Source, notifiers []Notifier, log *slog.Logger) *Engine {
	return newEngine(st, src, notifiers, log, time.Now)
}

// newEngine is NewEngine with an injectable clock.
func newEngine(st *store.Store, src Source, notifiers []Notifier, log *slog.Logger, now func() time.Time) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	e := &Engine{
		st:      st,
		src:     src,
		log:     log,
		changes: bus.New[model.Alert](changesBuffer),
		now:     now,
		rules:   defaultRuleMap(),
		open:    make(map[string]*model.Alert),
		since:   make(map[string]time.Time),
		seen:    make(map[model.DeviceID]struct{}),
		queue:   make(chan Notification, notifyQueueSize),
	}
	for _, n := range notifiers {
		if n != nil {
			e.notifiers = append(e.notifiers, n)
		}
	}
	if src != nil {
		e.ticks, e.unsubscribe = src.Ticks().Subscribe()
	}
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	if err := e.load(ctx); err != nil {
		log.Error("alerts: loading state failed; using built-in defaults until Run retries", "err", err)
	}
	return e
}

// hub returns the hub description attached to notifications.
func (e *Engine) hub() model.HubInfo {
	if e.src == nil {
		return model.HubInfo{}
	}
	return e.src.Hub()
}

// load reads rules, open alerts and known devices from the store. It is
// idempotent and a no-op once it has succeeded.
func (e *Engine) load(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.loaded {
		return nil
	}
	if e.st == nil {
		return errors.New("alerts: no store configured")
	}
	if err := e.loadRulesLocked(ctx); err != nil {
		return err
	}
	open, err := e.st.OpenAlertsByKey(ctx)
	if err != nil {
		return fmt.Errorf("alerts: load open alerts: %w", err)
	}
	e.open = make(map[string]*model.Alert, len(open))
	e.since = make(map[string]time.Time, len(open))
	for _, a := range open {
		if a == nil {
			continue
		}
		key := a.Key()
		e.open[key] = a
		e.since[key] = a.OpenedAt
	}
	devs, err := e.st.ListDevices(ctx)
	if err != nil {
		return fmt.Errorf("alerts: load devices: %w", err)
	}
	// Devices first seen very recently are left out of the seen set so a
	// device that joined right before a restart still raises new_device.
	now := e.now()
	window := newDeviceWindow(e.hub().PollIntervalSec)
	e.seen = make(map[model.DeviceID]struct{}, len(devs))
	for _, d := range devs {
		if d.ID == "" {
			continue
		}
		if d.FirstSeen.IsZero() || now.Sub(d.FirstSeen) > window {
			e.seen[d.ID] = struct{}{}
		}
	}
	e.loaded = true
	e.log.Info("alerts: state loaded", "rules", len(e.rules), "openAlerts", len(e.open), "knownDevices", len(e.seen))
	return nil
}

// loadRulesLocked replaces the in-memory rule set with the stored rules
// merged with the built-in defaults; defaults missing from the store are
// persisted. Stored rules of unknown type are ignored with a warning.
func (e *Engine) loadRulesLocked(ctx context.Context) error {
	stored, err := e.st.ListRules(ctx)
	if err != nil {
		return fmt.Errorf("alerts: load rules: %w", err)
	}
	merged := make(map[string]model.AlertRule, len(stored)+len(ruleSpecs))
	for _, r := range stored {
		if r.ID == "" {
			continue
		}
		merged[r.ID] = r
	}
	for _, def := range DefaultRules() {
		cur, ok := merged[def.ID]
		if !ok {
			r := def
			if err := e.st.SaveRule(ctx, &r); err != nil {
				return fmt.Errorf("alerts: persist default rule %s: %w", r.ID, err)
			}
			merged[r.ID] = r
			e.log.Info("alerts: added built-in rule", "rule", r.ID)
			continue
		}
		// Repair stored rows that predate a field or were hand-edited.
		if cur.Type == "" {
			cur.Type = def.Type
		}
		if cur.Name == "" {
			cur.Name = def.Name
		}
		if !validSeverity(cur.Severity) {
			cur.Severity = def.Severity
		}
		merged[def.ID] = cur
	}
	for id, r := range merged {
		if _, ok := ruleSpecs[r.Type]; !ok {
			e.log.Warn("alerts: ignoring stored rule of unknown type", "rule", id, "type", r.Type)
			delete(merged, id)
		}
	}
	e.rules = merged
	return nil
}

// newDeviceWindow is how recent a device's FirstSeen must be for it to
// count as new: three poll intervals, at least one minute, so a dropped
// tick cannot hide a join.
func newDeviceWindow(pollIntervalSec int) time.Duration {
	p := time.Duration(pollIntervalSec) * time.Second
	if p <= 0 {
		p = defaultPollInterval
	}
	w := 3 * p
	if w < time.Minute {
		w = time.Minute
	}
	return w
}

// Run evaluates every snapshot published by the source until ctx is
// cancelled, at which point it returns nil once the notification workers
// have stopped. The tick subscription is taken by NewEngine, so snapshots
// published between construction and Run are queued rather than lost. It
// returns an error if the engine is already running or its state cannot be
// loaded from the store.
func (e *Engine) Run(ctx context.Context) error {
	if e.src == nil {
		return errors.New("alerts: no source configured")
	}
	if !e.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer e.running.Store(false)

	ticks, unsubscribe := e.ticks, e.unsubscribe
	e.ticks, e.unsubscribe = nil, nil
	if ticks == nil {
		// Run after an earlier Run released the subscription.
		ticks, unsubscribe = e.src.Ticks().Subscribe()
	}
	defer unsubscribe()

	if err := e.load(ctx); err != nil {
		return err
	}

	var wg sync.WaitGroup
	for i := 0; i < notifyWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.notifyLoop(ctx)
		}()
	}
	defer wg.Wait()

	names := make([]string, 0, len(e.notifiers))
	for _, n := range e.notifiers {
		names = append(names, n.Name())
	}
	e.log.Info("alerts: engine running", "notifiers", names)

	for {
		select {
		case <-ctx.Done():
			return nil
		case snap, ok := <-ticks:
			if !ok {
				return errors.New("alerts: tick bus closed")
			}
			e.evaluate(ctx, snap)
			e.processed.Add(1)
		}
	}
}

// Changes publishes every alert transition (opened, escalated, resolved,
// acknowledged).
func (e *Engine) Changes() *bus.Bus[model.Alert] { return e.changes }

// Rules returns a copy of the current rules sorted by name.
func (e *Engine) Rules() []model.AlertRule {
	e.mu.RLock()
	out := make([]model.AlertRule, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, cloneRule(r))
	}
	e.mu.RUnlock()
	sortRulesByName(out)
	return out
}

// SaveRule validates r, persists it and reloads the in-memory rule set. The
// rule ID must name an existing rule (wrapping ErrUnknownRule otherwise); the
// type cannot change and severity, threshold, forSeconds and scope lists are
// validated (wrapping ErrInvalidRule). The saved rule is returned.
func (e *Engine) SaveRule(ctx context.Context, r model.AlertRule) (model.AlertRule, error) {
	if e.st == nil {
		return model.AlertRule{}, errors.New("alerts: no store configured")
	}
	id := strings.TrimSpace(r.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	existing, ok := e.rules[id]
	if !ok {
		return model.AlertRule{}, fmt.Errorf("%w: %q", ErrUnknownRule, id)
	}
	clean, err := normalizeRule(r, existing)
	if err != nil {
		return model.AlertRule{}, err
	}
	if err := e.st.SaveRule(ctx, &clean); err != nil {
		return model.AlertRule{}, fmt.Errorf("alerts: save rule %s: %w", id, err)
	}
	e.rules[id] = clean
	if err := e.loadRulesLocked(ctx); err != nil {
		// The save succeeded and memory already holds the new rule; a failed
		// reload only means other rows were not re-read.
		e.log.Warn("alerts: reloading rules after save failed", "rule", id, "err", err)
	}
	saved, ok := e.rules[id]
	if !ok {
		saved = clean
	}
	e.log.Info("alerts: rule saved", "rule", id, "enabled", saved.Enabled, "severity", saved.Severity,
		"threshold", saved.Threshold, "forSeconds", saved.ForSeconds, "notify", saved.Notify)
	return cloneRule(saved), nil
}

// Ack acknowledges the open alert id on behalf of by: it stamps AckedAt and
// AckedBy, persists and publishes the change and records an alert.acked
// event. Notifications for the alert are suppressed until it resolves.
// It wraps store.ErrNotFound for unknown IDs and ErrNotOpen for alerts that
// are already resolved. Acknowledging an already acknowledged alert is a
// no-op that returns the alert.
func (e *Engine) Ack(ctx context.Context, id int64, by string) (*model.Alert, error) {
	if e.st == nil {
		return nil, errors.New("alerts: no store configured")
	}
	if id <= 0 {
		return nil, fmt.Errorf("alerts: ack alert %d: %w", id, store.ErrNotFound)
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	var a *model.Alert
	for _, o := range e.open {
		if o.ID == id {
			a = o
			break
		}
	}
	if a == nil {
		stored, err := e.st.GetAlert(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("alerts: ack alert %d: %w", id, err)
		}
		if stored.State != model.AlertOpen {
			return nil, fmt.Errorf("alerts: ack alert %d: %w", id, ErrNotOpen)
		}
		// Open in the store but unknown to us (e.g. written by another
		// process): adopt it so it resolves normally.
		a = stored
		e.open[a.Key()] = a
		if _, ok := e.since[a.Key()]; !ok {
			e.since[a.Key()] = a.OpenedAt
		}
	}
	if a.AckedAt != nil {
		cp := cloneAlert(a)
		return &cp, nil
	}
	by = strings.TrimSpace(by)
	if by == "" {
		by = "unknown"
	}
	now := e.now()
	prev := *a
	a.AckedAt = &now
	a.AckedBy = by
	a.UpdatedAt = now
	if err := e.st.UpdateAlert(ctx, a); err != nil {
		*a = prev
		return nil, fmt.Errorf("alerts: ack alert %d: %w", id, err)
	}
	cp := cloneAlert(a)
	e.changes.Publish(cp)
	e.emit(ctx, model.EventAlertAcked, cp, model.SeverityInfo, "Acknowledged: "+cp.Title, "Acknowledged by "+by, map[string]any{"ackedBy": by})
	e.log.Info("alerts: alert acknowledged", "id", id, "rule", a.RuleID, "device", a.DeviceName, "by", by)
	return &cp, nil
}

// Test sends a synthetic alert.opened notification carrying msg to every
// configured notifier and reports which succeeded (sent) and, per failed
// notifier name, why (errs). Both results are non-nil.
func (e *Engine) Test(ctx context.Context, msg string) (sent []string, errs map[string]string) {
	sent = []string{}
	errs = map[string]string{}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "This is a test notification from Tailwatch."
	}
	msg = truncate(msg, 1000)
	now := e.now()
	hub := e.hub()
	n := Notification{
		Type: model.EventAlertOpened,
		Hub:  hub,
		Alert: model.Alert{
			RuleID:     "test",
			DeviceID:   hub.SelfID,
			DeviceName: hub.SelfName,
			State:      model.AlertOpen,
			Severity:   model.SeverityInfo,
			Title:      "Test notification",
			Message:    msg,
			OpenedAt:   now,
			UpdatedAt:  now,
			Data:       map[string]any{"test": true},
		},
	}
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, nf := range e.notifiers {
		wg.Add(1)
		go func(nf Notifier) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, notifyTimeout)
			defer cancel()
			err := nf.Send(sctx, n)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[nf.Name()] = err.Error()
				return
			}
			sent = append(sent, nf.Name())
		}(nf)
	}
	wg.Wait()
	sort.Strings(sent)
	return sent, errs
}

// --- evaluation -----------------------------------------------------------

// evaluate runs every enabled rule against every device of snap and
// applies the resulting transitions. It holds the engine lock for the whole
// tick so Ack/SaveRule never interleave with a half-applied evaluation.
func (e *Engine) evaluate(ctx context.Context, snap model.Snapshot) {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()

	rules := make([]model.AlertRule, 0, len(e.rules))
	for _, r := range e.rules {
		if r.Enabled {
			rules = append(rules, r)
		}
	}
	sortRulesByID(rules)

	removals, _ := e.src.(RemovalSource)
	present := make(map[model.DeviceID]struct{}, len(snap.Devices))
	active := make(map[string]struct{})
	for i := range snap.Devices {
		d := &snap.Devices[i]
		if d.ID == "" {
			continue
		}
		present[d.ID] = struct{}{}
		isNew := e.noteDeviceLocked(d, now, snap.Overview.Hub)
		// A device the collector keeps only as a memory of a node that left
		// the tailnet raises nothing; whatever is open for it resolves.
		removed := removals != nil && removals.Removed(d.ID)
		for j := range rules {
			r := &rules[j]
			if !ruleAppliesTo(r, d) {
				continue
			}
			key := alertKey(r.ID, d.ID)
			var obs observation
			if removed {
				obs = observation{resolved: "Device removed from the tailnet"}
			} else {
				obs = observe(r, d, evalContext{now: now, open: e.open[key], since: e.since[key], isNew: isNew})
			}
			if obs.active {
				active[key] = struct{}{}
				e.handleActiveLocked(ctx, r, d, key, obs, now)
			} else {
				e.handleInactiveLocked(ctx, key, obs, now)
			}
		}
	}

	// Open alerts that were not evaluated this tick belong to a rule that is
	// now disabled or no longer scopes the device, or to a device that has
	// disappeared. An empty snapshot is treated as a collector hiccup rather
	// than "every device vanished".
	if len(snap.Devices) == 0 {
		return
	}
	for key, a := range e.open {
		if _, ok := active[key]; ok {
			continue
		}
		reason := "Rule no longer applies to this device"
		if _, ok := present[a.DeviceID]; !ok {
			reason = "Device no longer present"
		}
		e.resolveLocked(ctx, a, now, reason, nil)
	}
	// Pending timers of conditions that were not evaluated this tick are
	// stale for the same reasons.
	for key := range e.since {
		if _, ok := active[key]; !ok {
			delete(e.since, key)
		}
	}
}

// noteDeviceLocked records d as seen and reports whether it counts as newly
// joined: it was unknown to the engine and the collector first observed it
// within the new-device window.
func (e *Engine) noteDeviceLocked(d *model.Device, now time.Time, hub model.HubInfo) bool {
	if _, ok := e.seen[d.ID]; ok {
		return false
	}
	e.seen[d.ID] = struct{}{}
	if d.FirstSeen.IsZero() {
		return false
	}
	window := newDeviceWindow(hub.PollIntervalSec)
	age := now.Sub(d.FirstSeen)
	return age <= window && age >= -window
}

// handleActiveLocked advances the pending timer for key or, when the
// condition has held long enough, opens the alert; for an already open
// alert it applies value/message updates and escalations.
func (e *Engine) handleActiveLocked(ctx context.Context, r *model.AlertRule, d *model.Device, key string, obs observation, now time.Time) {
	start := obs.since
	if start.IsZero() {
		start = now
	}
	e.since[key] = start

	if open := e.open[key]; open != nil {
		e.updateOpenLocked(ctx, r, open, obs, now)
		return
	}
	if !ruleSpecs[r.Type].immediate && now.Sub(start) < time.Duration(r.ForSeconds)*time.Second {
		return // still pending
	}
	sev := obs.severity
	if !validSeverity(sev) {
		sev = model.SeverityInfo
	}
	a := &model.Alert{
		RuleID:     r.ID,
		RuleType:   r.Type,
		DeviceID:   d.ID,
		DeviceName: displayName(d),
		State:      model.AlertOpen,
		Severity:   sev,
		Title:      obs.title,
		Message:    obs.message,
		Value:      obs.value,
		OpenedAt:   now,
		UpdatedAt:  now,
		Data:       obs.data,
	}
	if err := e.st.OpenAlert(ctx, a); err != nil {
		if ctx.Err() == nil {
			e.log.Error("alerts: persisting new alert failed", "rule", r.ID, "device", a.DeviceName, "err", err)
		}
		return // retried on the next tick
	}
	e.open[key] = a
	cp := cloneAlert(a)
	e.changes.Publish(cp)
	e.emit(ctx, model.EventAlertOpened, cp, cp.Severity, cp.Title, cp.Message, nil)
	if r.Notify {
		e.enqueue(model.EventAlertOpened, cp)
	}
	e.log.Info("alerts: alert opened", "id", a.ID, "rule", r.ID, "device", a.DeviceName, "severity", a.Severity, "title", a.Title)
}

// updateOpenLocked refreshes an open alert with the latest observation.
// Severity only ever increases; an escalation is published, recorded as an
// event and re-notified (unless acknowledged). Other changes are persisted
// silently so the stored message and value stay current.
func (e *Engine) updateOpenLocked(ctx context.Context, r *model.AlertRule, open *model.Alert, obs observation, now time.Time) {
	escalated := false
	changed := false
	if validSeverity(obs.severity) && severityRank(obs.severity) > severityRank(open.Severity) {
		open.Severity = obs.severity
		escalated = true
		changed = true
	}
	if obs.title != "" && obs.title != open.Title {
		open.Title = obs.title
		changed = true
	}
	if obs.message != "" && obs.message != open.Message {
		open.Message = obs.message
		changed = true
	}
	if !floatPtrEqual(open.Value, obs.value) {
		open.Value = obs.value
		changed = true
	}
	if !changed {
		return
	}
	if obs.data != nil {
		open.Data = obs.data
	}
	if escalated {
		data := maps.Clone(open.Data)
		if data == nil {
			data = map[string]any{}
		}
		data["escalatedAt"] = now.UTC().Format(time.RFC3339)
		open.Data = data
	}
	open.UpdatedAt = now
	if err := e.st.UpdateAlert(ctx, open); err != nil {
		if ctx.Err() == nil {
			e.log.Warn("alerts: updating alert failed", "id", open.ID, "rule", open.RuleID, "err", err)
		}
	}
	if !escalated {
		return
	}
	cp := cloneAlert(open)
	e.changes.Publish(cp)
	e.emit(ctx, model.EventAlertOpened, cp, cp.Severity, "Escalated: "+cp.Title, cp.Message, map[string]any{"escalated": true})
	if r.Notify && open.AckedAt == nil {
		e.enqueue(model.EventAlertOpened, cp)
	}
	e.log.Info("alerts: alert escalated", "id", open.ID, "rule", open.RuleID, "device", open.DeviceName, "severity", open.Severity)
}

// handleInactiveLocked clears any pending timer for key and resolves the
// open alert, if there is one.
func (e *Engine) handleInactiveLocked(ctx context.Context, key string, obs observation, now time.Time) {
	if a := e.open[key]; a != nil {
		e.resolveLocked(ctx, a, now, obs.resolved, obs.value)
		return
	}
	delete(e.since, key)
}

// resolveLocked marks a as resolved, persists and publishes it, records an
// alert.resolved event and notifies unless the alert was acknowledged.
func (e *Engine) resolveLocked(ctx context.Context, a *model.Alert, now time.Time, message string, clearedValue *float64) {
	key := a.Key()
	delete(e.open, key)
	delete(e.since, key)

	t := now
	a.State = model.AlertResolved
	a.ResolvedAt = &t
	a.UpdatedAt = now
	if message != "" {
		a.Message = message
	}
	data := maps.Clone(a.Data)
	if data == nil {
		data = map[string]any{}
	}
	data["durationSeconds"] = int64(now.Sub(a.OpenedAt).Seconds())
	if clearedValue != nil {
		data["resolvedValue"] = *clearedValue
	}
	a.Data = data

	if err := e.st.UpdateAlert(ctx, a); err != nil {
		if ctx.Err() == nil {
			e.log.Warn("alerts: persisting resolution failed", "id", a.ID, "rule", a.RuleID, "err", err)
		}
	}
	cp := cloneAlert(a)
	e.changes.Publish(cp)
	e.emit(ctx, model.EventAlertResolved, cp, model.SeverityInfo, "Resolved: "+cp.Title, cp.Message, nil)
	if r, ok := e.rules[a.RuleID]; ok && r.Notify && a.AckedAt == nil {
		e.enqueue(model.EventAlertResolved, cp)
	}
	e.log.Info("alerts: alert resolved", "id", a.ID, "rule", a.RuleID, "device", a.DeviceName, "acked", a.AckedAt != nil)
}

// emit records an alert lifecycle event through the source.
func (e *Engine) emit(ctx context.Context, t model.EventType, a model.Alert, sev model.Severity, title, message string, extra map[string]any) {
	if e.src == nil {
		return
	}
	data := map[string]any{
		"alertId":  a.ID,
		"ruleId":   a.RuleID,
		"ruleType": string(a.RuleType),
		"state":    string(a.State),
		"severity": string(a.Severity),
	}
	if a.Value != nil {
		data["value"] = *a.Value
	}
	for k, v := range extra {
		data[k] = v
	}
	e.src.Emit(ctx, model.Event{
		TS:       a.UpdatedAt,
		Type:     t,
		Severity: sev,
		DeviceID: a.DeviceID,
		Device:   a.DeviceName,
		Title:    title,
		Message:  message,
		Data:     data,
	})
}

// --- notifications --------------------------------------------------------

// enqueue hands a notification to the worker pool without blocking. When
// the queue is full the notification is dropped and logged.
func (e *Engine) enqueue(t model.EventType, a model.Alert) {
	if len(e.notifiers) == 0 {
		return
	}
	n := Notification{Type: t, Alert: a, Hub: e.hub()}
	select {
	case e.queue <- n:
	default:
		e.log.Warn("alerts: notification queue full, dropping notification", "type", t, "alert", a.ID, "rule", a.RuleID)
	}
}

// notifyLoop is one delivery worker.
func (e *Engine) notifyLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-e.queue:
			e.deliver(ctx, n)
		}
	}
}

// deliver sends n to every notifier concurrently, each bounded by
// notifyTimeout. Failures are logged at warn without URLs or tokens.
func (e *Engine) deliver(ctx context.Context, n Notification) {
	var wg sync.WaitGroup
	for _, nf := range e.notifiers {
		wg.Add(1)
		go func(nf Notifier) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, notifyTimeout)
			defer cancel()
			if err := nf.Send(sctx, n); err != nil {
				if ctx.Err() != nil {
					return // shutting down
				}
				e.log.Warn("alerts: notification failed", "notifier", nf.Name(), "type", n.Type, "alert", n.Alert.ID, "rule", n.Alert.RuleID, "err", err)
				return
			}
			e.log.Debug("alerts: notification sent", "notifier", nf.Name(), "type", n.Type, "alert", n.Alert.ID)
		}(nf)
	}
	wg.Wait()
}

// --- small helpers --------------------------------------------------------

// alertKey builds the same key as model.Alert.Key.
func alertKey(ruleID string, id model.DeviceID) string {
	return model.Alert{RuleID: ruleID, DeviceID: id}.Key()
}

// cloneAlert returns a copy of a whose Data map and pointer fields are
// independent of the engine's mutable copy.
func cloneAlert(a *model.Alert) model.Alert {
	cp := *a
	cp.Data = maps.Clone(a.Data)
	if a.Value != nil {
		v := *a.Value
		cp.Value = &v
	}
	if a.ResolvedAt != nil {
		t := *a.ResolvedAt
		cp.ResolvedAt = &t
	}
	if a.AckedAt != nil {
		t := *a.AckedAt
		cp.AckedAt = &t
	}
	return cp
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
