package tsapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestAdminActionRequests(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		call     func(c *Client) error
		method   string
		path     string
		body     string
		wantType string
	}{
		{"authorize", func(c *Client) error { return c.SetAuthorized(ctx, "nTLzc5Cf3d11CNTRL", true) },
			http.MethodPost, "/api/v2/device/nTLzc5Cf3d11CNTRL/authorized", `{"authorized":true}`, "application/json"},
		{"deauthorize legacy id", func(c *Client) error { return c.SetAuthorized(ctx, "92960230385", false) },
			http.MethodPost, "/api/v2/device/92960230385/authorized", `{"authorized":false}`, "application/json"},
		{"tags", func(c *Client) error { return c.SetTags(ctx, "n1", []string{"tag:server", "tag:exit"}) },
			http.MethodPost, "/api/v2/device/n1/tags", `{"tags":["tag:server","tag:exit"]}`, "application/json"},
		{"tags nil clears", func(c *Client) error { return c.SetTags(ctx, "n1", nil) },
			http.MethodPost, "/api/v2/device/n1/tags", `{"tags":[]}`, "application/json"},
		{"key expiry disabled", func(c *Client) error { return c.SetKeyExpiryDisabled(ctx, "n1", true) },
			http.MethodPost, "/api/v2/device/n1/key", `{"keyExpiryDisabled":true}`, "application/json"},
		{"key expiry enabled", func(c *Client) error { return c.SetKeyExpiryDisabled(ctx, "n1", false) },
			http.MethodPost, "/api/v2/device/n1/key", `{"keyExpiryDisabled":false}`, "application/json"},
		{"routes", func(c *Client) error { return c.SetRoutes(ctx, "n1", []string{"10.0.0.0/24", "0.0.0.0/0"}) },
			http.MethodPost, "/api/v2/device/n1/routes", `{"routes":["10.0.0.0/24","0.0.0.0/0"]}`, "application/json"},
		{"routes nil clears", func(c *Client) error { return c.SetRoutes(ctx, "n1", nil) },
			http.MethodPost, "/api/v2/device/n1/routes", `{"routes":[]}`, "application/json"},
		{"name", func(c *Client) error { return c.SetName(ctx, "n1", "new-name") },
			http.MethodPost, "/api/v2/device/n1/name", `{"name":"new-name"}`, "application/json"},
		{"name reset", func(c *Client) error { return c.SetName(ctx, "n1", "") },
			http.MethodPost, "/api/v2/device/n1/name", `{"name":""}`, "application/json"},
		{"delete", func(c *Client) error { return c.DeleteDevice(ctx, "n1") },
			http.MethodDelete, "/api/v2/device/n1", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 200, `{}`)
			}, nil)
			if err := tc.call(env.c); err != nil {
				t.Fatalf("call: %v", err)
			}
			reqs := env.rec.all()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(reqs))
			}
			r := reqs[0]
			if r.Method != tc.method || r.Path != tc.path {
				t.Errorf("request = %s %s, want %s %s", r.Method, r.Path, tc.method, tc.path)
			}
			if r.RawQuery != "" {
				t.Errorf("unexpected query %q", r.RawQuery)
			}
			if r.Body != tc.body {
				t.Errorf("body = %q, want %q", r.Body, tc.body)
			}
			if got := r.Header.Get("Content-Type"); got != tc.wantType {
				t.Errorf("content-type = %q, want %q", got, tc.wantType)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-api-key-TESTKEY" {
				t.Errorf("Authorization = %q", got)
			}
		})
	}
}

func TestAdminActionStatuses(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr bool
		wantNF  bool
	}{
		{"200", 200, `{}`, false, false},
		{"204 no content", 204, ``, false, false},
		{"404", 404, `{"message":"device not found"}`, true, true},
		{"403", 403, `{"message":"access denied"}`, true, false},
		{"400", 400, `{"message":"invalid tag"}`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, tc.status, tc.body)
			}, nil)
			err := env.c.DeleteDevice(context.Background(), "n1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil {
				return
			}
			if errors.Is(err, source.ErrNotFound) != tc.wantNF {
				t.Errorf("ErrNotFound = %v, want %v (%v)", !tc.wantNF, tc.wantNF, err)
			}
			if !strings.Contains(err.Error(), "delete device n1") {
				t.Errorf("error %q lacks op/device context", err.Error())
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tc.status {
				t.Errorf("err = %v, want APIError %d", err, tc.status)
			}
		})
	}
}

func TestAdminActionRejectsBadDeviceIDs(t *testing.T) {
	bad := []string{"", " ", "a/b", "..", "a.b", "n1?x=1", "n1#f", "a b", "n\n1", "ü", strings.Repeat("a", maxDeviceIDLen+1)}
	env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be reached for invalid ids (%s)", r.URL.Path)
	}, nil)
	ctx := context.Background()
	for _, id := range bad {
		calls := map[string]error{
			"SetAuthorized":        env.c.SetAuthorized(ctx, id, true),
			"SetTags":              env.c.SetTags(ctx, id, nil),
			"SetKeyExpiryDisabled": env.c.SetKeyExpiryDisabled(ctx, id, true),
			"SetRoutes":            env.c.SetRoutes(ctx, id, nil),
			"SetName":              env.c.SetName(ctx, id, "x"),
			"DeleteDevice":         env.c.DeleteDevice(ctx, id),
		}
		for name, err := range calls {
			if err == nil {
				t.Errorf("%s(%q): expected error", name, id)
				continue
			}
			if errors.Is(err, source.ErrNotConfigured) || errors.Is(err, source.ErrNotFound) {
				t.Errorf("%s(%q): wrong sentinel: %v", name, id, err)
			}
		}
	}
	good := []string{"n1", "nTLzc5Cf3d11CNTRL", "92960230385", "abc-DEF_123", strings.Repeat("a", maxDeviceIDLen)}
	for _, id := range good {
		if err := validateDeviceID(id); err != nil {
			t.Errorf("validateDeviceID(%q) = %v, want nil", id, err)
		}
	}
}
