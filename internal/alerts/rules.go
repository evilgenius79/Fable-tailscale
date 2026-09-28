package alerts

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// Limits applied by SaveRule.
const (
	maxRuleNameLen        = 100
	maxRuleDescriptionLen = 500
	maxScopeEntries       = 500
	maxThreshold          = 1e9
	maxForSeconds         = 366 * 24 * 3600
)

// ruleSpec holds the fixed, per-type behaviour of a rule kind.
type ruleSpec struct {
	unit         string  // unit of Threshold and Alert.Value ("%", "ms", "°C", "days", "x")
	hysteresis   float64 // subtracted from Threshold when deciding whether an open alert clears
	needsMetrics bool    // evaluated only for devices with usable agent metrics
	immediate    bool    // ForSeconds is not a sustain duration (new_device: auto-resolve delay)
}

// ruleSpecs is the set of known rule types.
var ruleSpecs = map[model.AlertRuleType]ruleSpec{
	model.RuleDeviceOffline:    {},
	model.RuleHighCPU:          {unit: "%", hysteresis: 5, needsMetrics: true},
	model.RuleHighMemory:       {unit: "%", hysteresis: 5, needsMetrics: true},
	model.RuleDiskFull:         {unit: "%", hysteresis: 5, needsMetrics: true},
	model.RuleHighLatency:      {unit: "ms", hysteresis: 5},
	model.RuleRelayOnly:        {},
	model.RuleKeyExpiring:      {unit: "days"},
	model.RuleUpdateAvailable:  {},
	model.RuleAgentUnreachable: {},
	model.RuleNewDevice:        {immediate: true},
	model.RuleUnauthorized:     {},
	model.RuleHighTemp:         {unit: "°C", hysteresis: 5, needsMetrics: true},
	model.RuleHighLoad:         {unit: "x", hysteresis: 0.25, needsMetrics: true},
}

// DefaultRules returns the built-in rule set, one rule per rule type with
// ID == string(type), all enabled and notifying, with the defaults from
// docs/API.md. Callers get a fresh copy on every call.
func DefaultRules() []model.AlertRule {
	mk := func(t model.AlertRuleType, name, desc string, sev model.Severity, threshold float64, forSec int) model.AlertRule {
		return model.AlertRule{
			ID:          string(t),
			Type:        t,
			Name:        name,
			Description: desc,
			Enabled:     true,
			Severity:    sev,
			Threshold:   threshold,
			ForSeconds:  forSec,
			Notify:      true,
		}
	}
	return []model.AlertRule{
		mk(model.RuleDeviceOffline, "Device offline",
			"A device has been offline for longer than the configured duration.",
			model.SeverityWarning, 0, 300),
		mk(model.RuleHighCPU, "High CPU",
			"CPU usage reported by the agent stays above the threshold (percent) for the configured duration.",
			model.SeverityWarning, 90, 600),
		mk(model.RuleHighMemory, "High memory",
			"Memory usage reported by the agent stays above the threshold (percent) for the configured duration.",
			model.SeverityWarning, 90, 600),
		mk(model.RuleDiskFull, "Disk almost full",
			"Usage of the primary volume exceeds the threshold (percent). Escalates to critical at 97%.",
			model.SeverityWarning, 90, 0),
		mk(model.RuleHighLatency, "High latency",
			"Disco ping latency from the hub stays above the threshold (milliseconds) for the configured duration.",
			model.SeverityInfo, 250, 300),
		mk(model.RuleRelayOnly, "Relayed connection",
			"The hub can only reach the device through a DERP relay for the configured duration.",
			model.SeverityInfo, 0, 900),
		mk(model.RuleKeyExpiring, "Key expiring",
			"The node key expires within the threshold (days). Escalates to critical below 2 days.",
			model.SeverityWarning, 7, 0),
		mk(model.RuleUpdateAvailable, "Update available",
			"A newer Tailscale client is available for the device.",
			model.SeverityInfo, 0, 0),
		mk(model.RuleAgentUnreachable, "Agent unreachable",
			"A tailwatch-agent that was reachable has not answered for the configured duration.",
			model.SeverityWarning, 0, 300),
		mk(model.RuleNewDevice, "New device",
			"A device joined the tailnet. The alert resolves itself after the configured duration.",
			model.SeverityInfo, 0, 86400),
		mk(model.RuleUnauthorized, "Unauthorized device",
			"A device is waiting for authorization in the admin console.",
			model.SeverityWarning, 0, 0),
		mk(model.RuleHighTemp, "High temperature",
			"The hottest sensor reported by the agent stays above the threshold (°C) for the configured duration.",
			model.SeverityWarning, 85, 300),
		mk(model.RuleHighLoad, "High load",
			"Load average (1 min) per CPU core stays above the threshold for the configured duration.",
			model.SeverityInfo, 2.0, 600),
	}
}

// defaultRuleMap returns DefaultRules keyed by ID.
func defaultRuleMap() map[string]model.AlertRule {
	out := make(map[string]model.AlertRule)
	for _, r := range DefaultRules() {
		out[r.ID] = r
	}
	return out
}

// validSeverity reports whether s is one of the known severities.
func validSeverity(s model.Severity) bool {
	switch s {
	case model.SeverityInfo, model.SeverityWarning, model.SeverityCritical:
		return true
	}
	return false
}

// severityRank orders severities for escalation decisions.
func severityRank(s model.Severity) int {
	switch s {
	case model.SeverityCritical:
		return 2
	case model.SeverityWarning:
		return 1
	default:
		return 0
	}
}

// maxSeverity returns the higher of a and b.
func maxSeverity(a, b model.Severity) model.Severity {
	if severityRank(b) > severityRank(a) {
		return b
	}
	return a
}

// normalizeRule validates an incoming rule against the rule it replaces and
// returns the cleaned copy that will be persisted. Every failure wraps
// ErrInvalidRule.
func normalizeRule(in, existing model.AlertRule) (model.AlertRule, error) {
	invalid := func(format string, args ...any) (model.AlertRule, error) {
		return model.AlertRule{}, fmt.Errorf("%w: %s", ErrInvalidRule, fmt.Sprintf(format, args...))
	}
	r := in
	r.ID = existing.ID
	if r.Type == "" {
		r.Type = existing.Type
	}
	if _, ok := ruleSpecs[r.Type]; !ok {
		return invalid("unknown rule type %q", r.Type)
	}
	if r.Type != existing.Type {
		return invalid("rule %q has type %q; the type cannot be changed", r.ID, existing.Type)
	}
	if !validSeverity(r.Severity) {
		return invalid("severity must be one of info, warning, critical")
	}
	if math.IsNaN(r.Threshold) || math.IsInf(r.Threshold, 0) {
		return invalid("threshold must be a finite number")
	}
	if r.Threshold < 0 || r.Threshold > maxThreshold {
		return invalid("threshold must be between 0 and %g", maxThreshold)
	}
	if r.ForSeconds < 0 || r.ForSeconds > maxForSeconds {
		return invalid("forSeconds must be between 0 and %d", maxForSeconds)
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = existing.Name
	}
	if len(r.Name) > maxRuleNameLen {
		return invalid("name must be at most %d characters", maxRuleNameLen)
	}
	r.Description = strings.TrimSpace(r.Description)
	if len(r.Description) > maxRuleDescriptionLen {
		return invalid("description must be at most %d characters", maxRuleDescriptionLen)
	}
	var err error
	if r.IncludeTags, err = cleanStrings("includeTags", r.IncludeTags); err != nil {
		return invalid("%v", err)
	}
	if r.ExcludeTags, err = cleanStrings("excludeTags", r.ExcludeTags); err != nil {
		return invalid("%v", err)
	}
	if r.IncludeDevice, err = cleanDeviceIDs("includeDevices", r.IncludeDevice); err != nil {
		return invalid("%v", err)
	}
	if r.ExcludeDevice, err = cleanDeviceIDs("excludeDevices", r.ExcludeDevice); err != nil {
		return invalid("%v", err)
	}
	r.UpdatedAt = existing.UpdatedAt // the store stamps the real value
	return r, nil
}

// cleanStrings trims, drops empty entries, de-duplicates and bounds a list.
func cleanStrings(field string, in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len(s) > maxRuleNameLen {
			return nil, fmt.Errorf("%s: entry longer than %d characters", field, maxRuleNameLen)
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) > maxScopeEntries {
		return nil, fmt.Errorf("%s: at most %d entries", field, maxScopeEntries)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// cleanDeviceIDs is cleanStrings for device ID lists.
func cleanDeviceIDs(field string, in []model.DeviceID) ([]model.DeviceID, error) {
	if len(in) == 0 {
		return nil, nil
	}
	tmp := make([]string, len(in))
	for i, id := range in {
		tmp[i] = string(id)
	}
	cleaned, err := cleanStrings(field, tmp)
	if err != nil {
		return nil, err
	}
	if len(cleaned) == 0 {
		return nil, nil
	}
	out := make([]model.DeviceID, len(cleaned))
	for i, s := range cleaned {
		out[i] = model.DeviceID(s)
	}
	return out, nil
}

// ruleAppliesTo reports whether r's scope includes d. Device lists match the
// stable node ID or (case-insensitively) the MagicDNS base name; tag lists
// match any of the device's tags.
func ruleAppliesTo(r *model.AlertRule, d *model.Device) bool {
	if len(r.IncludeDevice) > 0 && !matchesDevice(r.IncludeDevice, d) {
		return false
	}
	if len(r.ExcludeDevice) > 0 && matchesDevice(r.ExcludeDevice, d) {
		return false
	}
	if len(r.IncludeTags) > 0 && !hasAnyTag(d.Tags, r.IncludeTags) {
		return false
	}
	if len(r.ExcludeTags) > 0 && hasAnyTag(d.Tags, r.ExcludeTags) {
		return false
	}
	return true
}

func matchesDevice(list []model.DeviceID, d *model.Device) bool {
	for _, id := range list {
		if id == "" {
			continue
		}
		if id == d.ID || (d.Name != "" && strings.EqualFold(string(id), d.Name)) {
			return true
		}
	}
	return false
}

func hasAnyTag(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}

// cloneRule returns a deep copy of r (its slices are copied).
func cloneRule(r model.AlertRule) model.AlertRule {
	r.IncludeTags = cloneStrings(r.IncludeTags)
	r.ExcludeTags = cloneStrings(r.ExcludeTags)
	r.IncludeDevice = cloneIDs(r.IncludeDevice)
	r.ExcludeDevice = cloneIDs(r.ExcludeDevice)
	return r
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

func cloneIDs(s []model.DeviceID) []model.DeviceID {
	if s == nil {
		return nil
	}
	return append([]model.DeviceID(nil), s...)
}

// sortRulesByName orders rules by name (case-insensitive), then ID.
func sortRulesByName(rules []model.AlertRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := strings.ToLower(rules[i].Name), strings.ToLower(rules[j].Name)
		if a != b {
			return a < b
		}
		return rules[i].ID < rules[j].ID
	})
}

// sortRulesByID orders rules by ID for deterministic evaluation.
func sortRulesByID(rules []model.AlertRule) {
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
}
