package tsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// maxDeviceIDLen bounds the length of a device id accepted in a path.
const maxDeviceIDLen = 128

// SetAuthorized authorizes or de-authorizes a device
// (POST /api/v2/device/{id}/authorized).
func (c *Client) SetAuthorized(ctx context.Context, deviceID string, authorized bool) error {
	return c.deviceAction(ctx, "set authorized", deviceID, http.MethodPost, "/authorized",
		struct {
			Authorized bool `json:"authorized"`
		}{authorized})
}

// SetTags replaces the ACL tags of a device (POST /api/v2/device/{id}/tags).
// A nil slice clears the tags.
func (c *Client) SetTags(ctx context.Context, deviceID string, tags []string) error {
	return c.deviceAction(ctx, "set tags", deviceID, http.MethodPost, "/tags",
		struct {
			Tags []string `json:"tags"`
		}{nonNil(tags)})
}

// SetKeyExpiryDisabled enables or disables key expiry for a device
// (POST /api/v2/device/{id}/key).
func (c *Client) SetKeyExpiryDisabled(ctx context.Context, deviceID string, disabled bool) error {
	return c.deviceAction(ctx, "set key expiry", deviceID, http.MethodPost, "/key",
		struct {
			KeyExpiryDisabled bool `json:"keyExpiryDisabled"`
		}{disabled})
}

// SetRoutes sets the enabled subnet routes of a device
// (POST /api/v2/device/{id}/routes). A nil slice disables all routes.
func (c *Client) SetRoutes(ctx context.Context, deviceID string, routes []string) error {
	return c.deviceAction(ctx, "set routes", deviceID, http.MethodPost, "/routes",
		struct {
			Routes []string `json:"routes"`
		}{nonNil(routes)})
}

// SetName sets the machine name (base name without the tailnet domain)
// (POST /api/v2/device/{id}/name). An empty name resets it to the hostname.
func (c *Client) SetName(ctx context.Context, deviceID string, name string) error {
	return c.deviceAction(ctx, "set name", deviceID, http.MethodPost, "/name",
		struct {
			Name string `json:"name"`
		}{name})
}

// DeleteDevice removes a device from the tailnet (DELETE /api/v2/device/{id}).
func (c *Client) DeleteDevice(ctx context.Context, deviceID string) error {
	return c.deviceAction(ctx, "delete device", deviceID, http.MethodDelete, "", nil)
}

// deviceAction performs one /api/v2/device/{id}{suffix} call. payload, when
// non-nil, is sent as JSON.
func (c *Client) deviceAction(ctx context.Context, op, deviceID, method, suffix string, payload any) error {
	if err := c.ready(op); err != nil {
		return err
	}
	if err := validateDeviceID(deviceID); err != nil {
		return fmt.Errorf("tsapi: %s: %w", op, err)
	}
	r := request{method: method, path: "/api/v2/device/" + url.PathEscape(deviceID) + suffix}
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("tsapi: %s: encode request: %w", op, err)
		}
		r.body, r.contentType = body, "application/json"
	}
	resp, err := c.do(ctx, op, r)
	if err != nil {
		return fmt.Errorf("tsapi: %s %s: %w", op, deviceID, err)
	}
	if err := checkStatus(resp); err != nil {
		return fmt.Errorf("tsapi: %s %s: %w", op, deviceID, err)
	}
	c.log.Info("control api admin action", "op", op, "device", deviceID)
	return nil
}

// validateDeviceID accepts the legacy numeric id and the stable node id
// ("nXXXXCNTRL"): ASCII letters, digits, '-' and '_' only, so the value can
// never alter the request path.
func validateDeviceID(id string) error {
	if id == "" {
		return errors.New("empty device id")
	}
	if len(id) > maxDeviceIDLen {
		return errors.New("device id too long")
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
		default:
			return fmt.Errorf("invalid device id %q", id)
		}
	}
	return nil
}

// nonNil turns a nil slice into an empty one so it encodes as [] not null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
