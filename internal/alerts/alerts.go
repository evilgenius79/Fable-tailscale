// Package alerts is the Tailwatch watchdog. An Engine subscribes to the
// collector's per-poll snapshots, evaluates every enabled alert rule against
// every device the rule scopes to, and opens, escalates and resolves alerts in
// the store. Every state change is published on Changes() (for the SSE
// stream), recorded as an event through the Source and fanned out to the
// configured notifiers (generic webhook, Slack/Discord, ntfy) from a bounded
// background worker pool so slow endpoints never delay evaluation.
//
// Rules are keyed by ID; the built-in defaults (see DefaultRules) use the rule
// type as the ID and are merged with any stored overrides at start-up so
// thresholds, durations, severities and scopes can be tuned through the API.
//
// Evaluation semantics (docs/API.md, "Alert rule parameters"):
//
//   - Sustained conditions ("for"): the engine remembers when a condition was
//     first observed true per (rule, device) and fires once it has held for
//     ForSeconds. Where the collector knows a better start time (a device's
//     last online/offline transition, an agent's last successful poll) that
//     time is used, so alerts fire at the right moment even after a restart.
//   - Hysteresis: numeric rules clear only once the value drops below the
//     threshold minus a small margin (5 for percent/ms/°C, 0.25 for load
//     ratios) so a value hovering around the threshold does not flap.
//   - Escalation: severity only ever increases while an alert is open
//     (disk_full ≥ 97% and key_expiring < 2 days become critical); an
//     escalation is persisted, published and re-notified.
//   - One open alert per (rule, device). Acknowledging suppresses
//     notifications until the alert resolves; the resolution itself is
//     still recorded and published.
package alerts

import (
	"context"
	"errors"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/bus"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// Notifier delivers alert notifications to one destination.
type Notifier interface {
	// Name identifies the notifier kind ("webhook", "slack", "ntfy").
	Name() string
	// Send delivers n. Implementations must honour ctx, must not block
	// longer than a few seconds and must never include URLs or tokens in
	// returned errors.
	Send(ctx context.Context, n Notification) error
}

// Notification is what notifiers receive for every alert transition. It is
// also the JSON document posted by the generic webhook.
type Notification struct {
	Type  model.EventType `json:"type"` // alert.opened (also used for escalations) or alert.resolved
	Alert model.Alert     `json:"alert"`
	Hub   model.HubInfo   `json:"hub"`
}

// Source is what the engine needs from the collector. It is an interface so
// this package does not import internal/collector.
type Source interface {
	// Ticks publishes a snapshot after every collector poll.
	Ticks() *bus.Bus[model.Snapshot]
	// Emit persists and publishes an event.
	Emit(ctx context.Context, e model.Event)
	// Hub describes the hub node; it is attached to every notification.
	Hub() model.HubInfo
}

// Sentinel errors returned by Engine methods. Callers use errors.Is to map
// them to HTTP status codes.
var (
	// ErrUnknownRule is returned by SaveRule when the rule ID does not name
	// an existing rule. New rule IDs cannot be created through the API.
	ErrUnknownRule = errors.New("alerts: unknown rule")
	// ErrInvalidRule wraps every validation failure reported by SaveRule.
	ErrInvalidRule = errors.New("alerts: invalid rule")
	// ErrNotOpen is returned by Ack when the alert exists but is not open.
	ErrNotOpen = errors.New("alerts: alert is not open")
	// ErrAlreadyRunning is returned by Run when the engine is already running.
	ErrAlreadyRunning = errors.New("alerts: engine already running")
)

const (
	// notifyTimeout bounds every notifier call.
	notifyTimeout = 10 * time.Second
	// notifyQueueSize bounds the number of notifications waiting for the
	// worker pool; when it is full, new notifications are dropped and logged.
	notifyQueueSize = 256
	// notifyWorkers is the number of background delivery goroutines.
	notifyWorkers = 2
	// changesBuffer is the per-subscriber buffer of the Changes bus.
	changesBuffer = 64
	// loadTimeout bounds the store reads performed by NewEngine.
	loadTimeout = 30 * time.Second

	// metricsStaleAfter is how long metrics carried over from the last
	// successful agent poll are still trusted once the agent has become
	// unreachable. After that, metric-based rules treat the device as having
	// no metrics and their alerts resolve.
	metricsStaleAfter = 5 * time.Minute
	// diskCriticalPercent is the disk usage at or above which a disk_full
	// alert is escalated to critical regardless of the rule's severity.
	diskCriticalPercent = 97.0
	// keyCriticalDays is the remaining key lifetime below which a
	// key_expiring alert is escalated to critical.
	keyCriticalDays = 2.0
	// defaultPollInterval is assumed when the hub does not report one.
	defaultPollInterval = 15 * time.Second
)
