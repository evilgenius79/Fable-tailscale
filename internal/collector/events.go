package collector

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// newDeviceEvent builds an event attributed to d.
func newDeviceEvent(now time.Time, d *model.Device, typ model.EventType, sev model.Severity, title, msg string, data map[string]any) model.Event {
	return model.Event{
		TS:       now,
		Type:     typ,
		Severity: sev,
		DeviceID: d.ID,
		Device:   d.Name,
		Title:    title,
		Message:  msg,
		Data:     data,
	}
}

// deviceEvents compares a device with its previous state and returns the
// resulting transition events. A nil prev means the device was never seen
// before and yields a single device.new event.
func deviceEvents(now time.Time, prev, cur *model.Device) []model.Event {
	if prev == nil {
		return []model.Event{newDeviceEvent(now, cur, model.EventDeviceNew, model.SeverityInfo,
			"New device: "+cur.Name,
			fmt.Sprintf("%s joined the tailnet (%s, %s)", cur.Name, displayOS(cur.OS), ownerLabel(cur)),
			map[string]any{"os": cur.OS, "user": cur.User, "tags": cur.Tags, "online": cur.Online})}
	}
	var evs []model.Event
	if prev.Online != cur.Online {
		if cur.Online {
			evs = append(evs, newDeviceEvent(now, cur, model.EventDeviceOnline, model.SeverityInfo,
				"Online: "+cur.Name,
				fmt.Sprintf("%s is online (%s)", cur.Name, pathLabel(cur.Connectivity.Path, cur.Connectivity.Relay)),
				map[string]any{"path": cur.Connectivity.Path, "relay": cur.Connectivity.Relay}))
		} else {
			data := map[string]any{}
			if prev.Uptime.LastChange != nil {
				data["onlineForSeconds"] = int64(now.Sub(*prev.Uptime.LastChange).Seconds())
			}
			evs = append(evs, newDeviceEvent(now, cur, model.EventDeviceOffline, model.SeverityWarning,
				"Offline: "+cur.Name,
				fmt.Sprintf("%s went offline", cur.Name), data))
		}
	}
	if isPathTransition(prev.Connectivity.Path, cur.Connectivity.Path) {
		if e, ok := pathChangedEvent(now, prev, cur.Connectivity.Path, cur.Connectivity.Relay); ok {
			e.Device = cur.Name
			evs = append(evs, e)
		}
	}
	if changes, data := describeChanges(prev, cur); len(changes) > 0 {
		evs = append(evs, newDeviceEvent(now, cur, model.EventDeviceUpdated, model.SeverityInfo,
			"Updated: "+cur.Name,
			fmt.Sprintf("%s changed: %s", cur.Name, strings.Join(changes, "; ")), data))
	}
	if !prev.Expired && cur.Expired {
		evs = append(evs, newDeviceEvent(now, cur, model.EventDeviceExpired, model.SeverityWarning,
			"Key expired: "+cur.Name,
			fmt.Sprintf("The node key of %s has expired", cur.Name), nil))
	}
	if !prev.Authorized && cur.Authorized {
		evs = append(evs, newDeviceEvent(now, cur, model.EventDeviceAuthorized, model.SeverityInfo,
			"Authorized: "+cur.Name,
			fmt.Sprintf("%s was authorized", cur.Name), nil))
	}
	return evs
}

// removedEvent describes a device that vanished from every source.
func removedEvent(now time.Time, d *model.Device, polls int) model.Event {
	return newDeviceEvent(now, d, model.EventDeviceRemoved, model.SeverityInfo,
		"Removed: "+d.Name,
		fmt.Sprintf("%s has not been reported by any source for %d consecutive polls", d.Name, polls),
		map[string]any{"polls": polls})
}

// pathChangedEvent builds a device.path_changed event for d moving from its
// current path to newPath. ok is false unless the move is between direct
// and relay.
func pathChangedEvent(now time.Time, d *model.Device, newPath model.PathType, newRelay string) (model.Event, bool) {
	from := d.Connectivity.Path
	if !isPathTransition(from, newPath) {
		return model.Event{}, false
	}
	return newDeviceEvent(now, d, model.EventPathChanged, model.SeverityInfo,
		"Path changed: "+d.Name,
		fmt.Sprintf("%s path changed from %s to %s", d.Name, pathLabel(from, d.Connectivity.Relay), pathLabel(newPath, newRelay)),
		map[string]any{"from": from, "to": newPath, "fromRelay": d.Connectivity.Relay, "relay": newRelay}), true
}

// isPathTransition reports whether a path change is between direct and
// relay (changes to or from unknown/none are not reported).
func isPathTransition(from, to model.PathType) bool {
	return (from == model.PathDirect && to == model.PathRelay) || (from == model.PathRelay && to == model.PathDirect)
}

// describeChanges lists human-readable changes to the fields that produce
// a device.updated event (client version, OS, tags, hostname). A field
// that is empty on either side is not a change: it reflects a source
// becoming (un)available, not the device changing.
func describeChanges(prev, cur *model.Device) ([]string, map[string]any) {
	var changes []string
	data := map[string]any{}
	if prev.ClientVersion != "" && cur.ClientVersion != "" && prev.ClientVersion != cur.ClientVersion {
		changes = append(changes, fmt.Sprintf("client version %s → %s", prev.ClientVersion, cur.ClientVersion))
		data["clientVersionFrom"], data["clientVersionTo"] = prev.ClientVersion, cur.ClientVersion
	}
	if prev.OS != "" && cur.OS != "" && prev.OS != cur.OS {
		changes = append(changes, fmt.Sprintf("OS %s → %s", prev.OS, cur.OS))
		data["osFrom"], data["osTo"] = prev.OS, cur.OS
	}
	if prev.Hostname != "" && cur.Hostname != "" && prev.Hostname != cur.Hostname {
		changes = append(changes, fmt.Sprintf("hostname %s → %s", prev.Hostname, cur.Hostname))
		data["hostnameFrom"], data["hostnameTo"] = prev.Hostname, cur.Hostname
	}
	if !sameSet(prev.Tags, cur.Tags) {
		changes = append(changes, fmt.Sprintf("tags %s → %s", tagList(prev.Tags), tagList(cur.Tags)))
		data["tagsFrom"], data["tagsTo"] = nonNil(prev.Tags), nonNil(cur.Tags)
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return changes, data
}

// sameSet reports whether two string slices hold the same elements
// regardless of order.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := slices.Clone(a), slices.Clone(b)
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

// tagList renders tags for messages.
func tagList(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	return "[" + strings.Join(tags, ", ") + "]"
}

// pathLabel renders a path for messages, e.g. "relay (nyc)".
func pathLabel(p model.PathType, relay string) string {
	if p == model.PathRelay && relay != "" {
		return fmt.Sprintf("relay (%s)", relay)
	}
	if p == "" {
		return string(model.PathUnknown)
	}
	return string(p)
}

// displayOS renders a possibly empty OS string.
func displayOS(os string) string {
	if os == "" {
		return "unknown OS"
	}
	return os
}

// ownerLabel renders the owner of a device for messages.
func ownerLabel(d *model.Device) string {
	switch {
	case d.User != "":
		return d.User
	case len(d.Tags) > 0:
		return "tagged " + strings.Join(d.Tags, ",")
	}
	return "no owner"
}

// hubErrorEvent describes a failure to reach the local tailscaled.
func hubErrorEvent(now time.Time, msg string) model.Event {
	return model.Event{
		TS:       now,
		Type:     model.EventHubError,
		Severity: model.SeverityWarning,
		Title:    "tailscaled unreachable",
		Message:  msg,
		Data:     map[string]any{"source": "tailscaled"},
	}
}
