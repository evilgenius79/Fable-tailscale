package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

const (
	maxTags   = 100
	maxRoutes = 1000
)

var (
	// tagPattern is the Tailscale tag syntax.
	tagPattern = regexp.MustCompile(`^tag:[a-z0-9-]+$`)
	// namePattern is a DNS label: 1-63 lower-case alphanumerics or hyphens,
	// not starting or ending with a hyphen.
	namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// apiCall performs one control API action on a device.
type apiCall func(ctx context.Context, api source.ControlAPI, dev model.Device) error

// adminActionsEnabled reports whether device admin actions can run: the
// flag is set and a configured control API client is present.
func (s *Server) adminActionsEnabled() bool {
	return s.cfg.EnableAdminActions && s.d.API != nil && s.d.API.Configured()
}

// deviceAction runs the common flow of every device admin action: gate
// (admin role is enforced by the router; here: feature enabled), device
// lookup, body parsing, control API call, audit entry and admin.action
// event for every attempt, collector refresh and response.
//
// parse decodes and validates the body, returning the audit details and
// the API call to make. remove marks the DELETE action (204, device
// forgotten).
func (s *Server) deviceAction(w http.ResponseWriter, r *http.Request, action string, remove bool,
	parse func(r *http.Request) (map[string]any, apiCall, error)) {
	id, ok := s.identity(w, r)
	if !ok {
		return
	}
	if !s.adminActionsEnabled() {
		writeError(w, http.StatusNotImplemented, codeNotConfigured,
			"device admin actions are disabled: start the hub with --enable-admin-actions and control API credentials")
		return
	}
	raw := strings.TrimSpace(r.PathValue("id"))
	dev, found := s.d.Collector.Device(model.DeviceID(raw))
	if !found {
		err := errors.New("device not found")
		s.audit(r, id, action, truncateText(raw, 255), nil, err)
		s.adminEvent(r, id, action, nil, truncateText(raw, 64), nil, err)
		writeError(w, http.StatusNotFound, codeNotFound, "device not found")
		return
	}
	details, call, err := parse(r)
	if err != nil {
		s.audit(r, id, action, string(dev.ID), details, err)
		s.adminEvent(r, id, action, &dev, string(dev.ID), details, err)
		s.writeAPIError(w, r, err)
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	details["deviceName"] = dev.Name

	err = call(r.Context(), s.d.API, dev)
	s.audit(r, id, action, string(dev.ID), details, err)
	s.adminEvent(r, id, action, &dev, string(dev.ID), details, err)
	if err != nil {
		s.writeUpstreamError(w, r, err)
		return
	}
	s.log.Info("httpapi: admin action", "action", action, "device", dev.ID, "name", dev.Name, "login", id.Login, "node", id.NodeName)

	if remove {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		if ferr := s.d.Collector.Forget(fctx, dev.ID); ferr != nil && !errors.Is(ferr, source.ErrNotFound) {
			s.log.Warn("httpapi: forgetting deleted device failed", "device", dev.ID, "err", ferr)
		}
		s.d.Collector.RefreshNow()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.d.Collector.RefreshNow()
	current, ok := s.d.Collector.Device(dev.ID)
	if !ok {
		current = dev
	}
	writeJSON(w, http.StatusOK, current)
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, "device.authorize", false, func(r *http.Request) (map[string]any, apiCall, error) {
		var body struct {
			Authorized *bool `json:"authorized"`
		}
		if err := decodeJSON(r, &body, false); err != nil {
			return nil, nil, err
		}
		if body.Authorized == nil {
			return nil, nil, badRequest("authorized (boolean) is required")
		}
		v := *body.Authorized
		return map[string]any{"authorized": v}, func(ctx context.Context, api source.ControlAPI, dev model.Device) error {
			return api.SetAuthorized(ctx, string(dev.ID), v)
		}, nil
	})
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, "device.tags", false, func(r *http.Request) (map[string]any, apiCall, error) {
		var body struct {
			Tags *[]string `json:"tags"`
		}
		if err := decodeJSON(r, &body, false); err != nil {
			return nil, nil, err
		}
		if body.Tags == nil {
			return nil, nil, badRequest("tags (array of \"tag:...\") is required")
		}
		tags, err := validateTags(*body.Tags)
		if err != nil {
			return map[string]any{"tags": *body.Tags}, nil, err
		}
		return map[string]any{"tags": tags}, func(ctx context.Context, api source.ControlAPI, dev model.Device) error {
			return api.SetTags(ctx, string(dev.ID), tags)
		}, nil
	})
}

func (s *Server) handleKeyExpiry(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, "device.key-expiry", false, func(r *http.Request) (map[string]any, apiCall, error) {
		var body struct {
			Disabled *bool `json:"disabled"`
		}
		if err := decodeJSON(r, &body, false); err != nil {
			return nil, nil, err
		}
		if body.Disabled == nil {
			return nil, nil, badRequest("disabled (boolean) is required")
		}
		v := *body.Disabled
		return map[string]any{"disabled": v}, func(ctx context.Context, api source.ControlAPI, dev model.Device) error {
			return api.SetKeyExpiryDisabled(ctx, string(dev.ID), v)
		}, nil
	})
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, "device.routes", false, func(r *http.Request) (map[string]any, apiCall, error) {
		var body struct {
			Routes *[]string `json:"routes"`
		}
		if err := decodeJSON(r, &body, false); err != nil {
			return nil, nil, err
		}
		if body.Routes == nil {
			return nil, nil, badRequest("routes (array of CIDR prefixes) is required")
		}
		routes, err := validateRoutes(*body.Routes)
		if err != nil {
			return map[string]any{"routes": *body.Routes}, nil, err
		}
		return map[string]any{"routes": routes}, func(ctx context.Context, api source.ControlAPI, dev model.Device) error {
			return api.SetRoutes(ctx, string(dev.ID), routes)
		}, nil
	})
}

func (s *Server) handleName(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, "device.name", false, func(r *http.Request) (map[string]any, apiCall, error) {
		var body struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &body, false); err != nil {
			return nil, nil, err
		}
		name, err := validateName(body.Name)
		if err != nil {
			return map[string]any{"name": truncateText(body.Name, 100)}, nil, err
		}
		return map[string]any{"name": name}, func(ctx context.Context, api source.ControlAPI, dev model.Device) error {
			return api.SetName(ctx, string(dev.ID), name)
		}, nil
	})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, "device.delete", true, func(r *http.Request) (map[string]any, apiCall, error) {
		return map[string]any{}, func(ctx context.Context, api source.ControlAPI, dev model.Device) error {
			return api.DeleteDevice(ctx, string(dev.ID))
		}, nil
	})
}

// validateTags trims, lower-cases, de-duplicates and syntax-checks tags.
// The result is never nil.
func validateTags(in []string) ([]string, error) {
	if len(in) > maxTags {
		return nil, badRequest("at most %d tags are allowed", maxTags)
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if !tagPattern.MatchString(t) {
			return nil, badRequest("invalid tag %q: tags look like tag:name (lower-case letters, digits and hyphens)", truncateText(t, 64))
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out, nil
}

// validateRoutes parses and canonicalizes CIDR prefixes. The result is
// never nil.
func validateRoutes(in []string) ([]string, error) {
	if len(in) > maxRoutes {
		return nil, badRequest("at most %d routes are allowed", maxRoutes)
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		p, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, badRequest("invalid route %q: use CIDR notation such as 10.0.0.0/24", truncateText(r, 64))
		}
		canon := p.Masked().String()
		if _, dup := seen[canon]; dup {
			continue
		}
		seen[canon] = struct{}{}
		out = append(out, canon)
	}
	return out, nil
}

// validateName normalizes and checks a machine name (DNS label rules).
func validateName(in string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(in))
	if name == "" {
		return "", badRequest("name is required")
	}
	if len(name) > 63 || !namePattern.MatchString(name) {
		return "", badRequest("invalid name: use 1-63 lower-case letters, digits and hyphens, not starting or ending with a hyphen")
	}
	return name, nil
}
