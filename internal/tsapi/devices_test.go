package tsapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// devicesFixture mirrors a real GET /api/v2/tailnet/-/devices?fields=all
// response: a user-owned laptop with connectivity data, a tagged exit node
// with key expiry disabled (zero "expires"), and an external shared node with
// sparse fields and a malformed timestamp.
const devicesFixture = `{
  "devices": [
    {
      "addresses": ["100.101.102.103", "fd7a:115c:a1e0:ab12:4843:cd96:6265:6667"],
      "id": "92960230385",
      "nodeId": "nTLzc5Cf3d11CNTRL",
      "user": "alice@example.com",
      "name": "alice-laptop.tail1234.ts.net",
      "hostname": "Alice's MacBook",
      "clientVersion": "1.86.2-t1234abcd-g5678efgh",
      "updateAvailable": true,
      "os": "macOS",
      "created": "2024-03-01T10:00:00Z",
      "lastSeen": "2026-09-28T04:59:12.123456Z",
      "keyExpiryDisabled": false,
      "expires": "2027-03-01T10:00:00Z",
      "authorized": true,
      "isExternal": false,
      "machineKey": "mkey:0123456789abcdef",
      "nodeKey": "nodekey:fedcba9876543210",
      "blocksIncomingConnections": false,
      "enabledRoutes": [],
      "advertisedRoutes": [],
      "clientConnectivity": {
        "endpoints": ["203.0.113.10:41641", "192.168.1.20:41641"],
        "mappingVariesByDestIP": false,
        "latency": {
          "New York City": {"preferred": true, "latencyMs": 12.5},
          "Frankfurt": {"latencyMs": 98.25}
        },
        "clientSupports": {"hairPinning": false, "ipv6": true, "pcp": false, "pmp": true, "udp": true, "upnp": false}
      },
      "tailnetLockError": "",
      "tailnetLockKey": "tlpub:abc",
      "postureIdentity": {"serialNumbers": ["C02XYZ"]}
    },
    {
      "addresses": ["100.64.0.7"],
      "id": "112233445566",
      "nodeId": "nSRV1234CNTRL",
      "user": "tagged-devices",
      "name": "cloud-vm.tail1234.ts.net",
      "hostname": "cloud-vm",
      "clientVersion": "1.80.0-tabc",
      "updateAvailable": false,
      "os": "linux",
      "created": "2023-11-11T11:11:11Z",
      "lastSeen": "2026-09-28T05:00:00Z",
      "keyExpiryDisabled": true,
      "expires": "0001-01-01T00:00:00Z",
      "authorized": true,
      "isExternal": false,
      "tags": ["tag:server", "tag:exit"],
      "advertisedRoutes": ["0.0.0.0/0", "::/0", "10.0.0.0/24"],
      "enabledRoutes": ["10.0.0.0/24"],
      "blocksIncomingConnections": true,
      "tailnetLockError": "node key not signed",
      "clientConnectivity": {
        "endpoints": ["198.51.100.5:41641"],
        "mappingVariesByDestIP": true,
        "latency": {
          "Atlantis City": {"preferred": true, "latencyMs": 3},
          "Frankfurt": {"preferred": true, "latencyMs": 40}
        },
        "clientSupports": {"hairPinning": true, "ipv6": false, "pcp": false, "pmp": false, "udp": true, "upnp": true}
      }
    },
    {
      "addresses": ["100.90.90.90"],
      "id": "998877",
      "nodeId": "nEXT9999CNTRL",
      "user": "bob@other.example",
      "name": "shared-nas.tail9999.ts.net.",
      "hostname": "shared-nas",
      "os": "linux",
      "created": "",
      "lastSeen": "not-a-timestamp",
      "authorized": true,
      "isExternal": true
    }
  ]
}`

func TestDevicesDecodesFixture(t *testing.T) {
	env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fields") != "all" {
			t.Errorf("fields query = %q, want all", r.URL.Query().Get("fields"))
		}
		jsonStatus(w, 200, devicesFixture)
	}, nil)
	devs, err := env.c.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devs) != 3 {
		t.Fatalf("got %d devices, want 3", len(devs))
	}

	ts := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatalf("bad test timestamp %q: %v", s, err)
		}
		return v
	}
	f, tr := false, true
	expires := ts("2027-03-01T10:00:00Z")

	want := []source.APIDevice{
		{
			ID:               "92960230385",
			NodeID:           model.DeviceID("nTLzc5Cf3d11CNTRL"),
			Name:             "alice-laptop.tail1234.ts.net",
			Hostname:         "Alice's MacBook",
			User:             "alice@example.com",
			OS:               "macOS",
			ClientVersion:    "1.86.2-t1234abcd-g5678efgh",
			UpdateAvailable:  true,
			Addresses:        []string{"100.101.102.103", "fd7a:115c:a1e0:ab12:4843:cd96:6265:6667"},
			Created:          ts("2024-03-01T10:00:00Z"),
			LastSeen:         ts("2026-09-28T04:59:12.123456Z"),
			Expires:          &expires,
			Authorized:       true,
			AdvertisedRoutes: []string{},
			EnabledRoutes:    []string{},
			Endpoints:        []string{"203.0.113.10:41641", "192.168.1.20:41641"},
			DERPLatencyMs:    map[string]float64{"nyc": 12.5, "fra": 98.25},
			PreferredDERP:    "nyc",
			NATSupport: &model.NATSupport{
				HairPinning: false, IPv6: true, PCP: false, PMP: true, UDP: true, UPnP: false,
			},
			MappingVariesByDestIP: &f,
		},
		{
			ID:                "112233445566",
			NodeID:            model.DeviceID("nSRV1234CNTRL"),
			Name:              "cloud-vm.tail1234.ts.net",
			Hostname:          "cloud-vm",
			User:              "tagged-devices",
			OS:                "linux",
			ClientVersion:     "1.80.0-tabc",
			Addresses:         []string{"100.64.0.7"},
			Created:           ts("2023-11-11T11:11:11Z"),
			LastSeen:          ts("2026-09-28T05:00:00Z"),
			Expires:           nil, // zero time means "never"
			KeyExpiryDisabled: true,
			Authorized:        true,
			Tags:              []string{"tag:server", "tag:exit"},
			AdvertisedRoutes:  []string{"0.0.0.0/0", "::/0", "10.0.0.0/24"},
			EnabledRoutes:     []string{"10.0.0.0/24"},
			BlocksIncoming:    true,
			TailnetLockError:  "node key not signed",
			Endpoints:         []string{"198.51.100.5:41641"},
			DERPLatencyMs:     map[string]float64{"atlantis-city": 3, "fra": 40},
			PreferredDERP:     "atlantis-city", // first preferred in sorted name order
			NATSupport: &model.NATSupport{
				HairPinning: true, IPv6: false, PCP: false, PMP: false, UDP: true, UPnP: true,
			},
			MappingVariesByDestIP: &tr,
		},
		{
			ID:         "998877",
			NodeID:     model.DeviceID("nEXT9999CNTRL"),
			Name:       "shared-nas.tail9999.ts.net", // trailing dot stripped
			Hostname:   "shared-nas",
			User:       "bob@other.example",
			OS:         "linux",
			Addresses:  []string{"100.90.90.90"},
			Authorized: true,
			IsExternal: true,
			// created "" and lastSeen malformed both decode to the zero time
			// without failing the listing; no connectivity block means nil
			// pointers and empty maps.
		},
	}
	for i := range want {
		if !reflect.DeepEqual(devs[i], want[i]) {
			t.Errorf("device %d mismatch:\n got %+v\nwant %+v", i, devs[i], want[i])
		}
	}
	if devs[2].Expires != nil || devs[2].NATSupport != nil || devs[2].MappingVariesByDestIP != nil || devs[2].DERPLatencyMs != nil {
		t.Errorf("external device should have nil optional fields: %+v", devs[2])
	}
}

func TestDevicesEdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantLen  int
		wantErr  string
		wantNF   bool
		wantNilD bool
	}{
		{"null list", 200, `{"devices":null}`, 0, "", false, false},
		{"missing key", 200, `{}`, 0, "", false, false},
		{"malformed json", 200, `{"devices":[}`, 0, "decode response", false, true},
		{"wrong type", 200, `{"devices":[{"id":123}]}`, 0, "decode response", false, true},
		{"unknown tailnet", 404, `{"message":"tailnet not found"}`, 0, "tailnet not found", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, tc.status, tc.body)
			}, nil)
			devs, err := env.c.Devices(context.Background())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if devs == nil || len(devs) != tc.wantLen {
					t.Errorf("devs = %#v, want empty non-nil", devs)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
			if errors.Is(err, source.ErrNotFound) != tc.wantNF {
				t.Errorf("ErrNotFound = %v, want %v", !tc.wantNF, tc.wantNF)
			}
			if (devs == nil) != tc.wantNilD {
				t.Errorf("devs nil = %v, want %v", devs == nil, tc.wantNilD)
			}
		})
	}
}

func TestConvertLatency(t *testing.T) {
	cases := []struct {
		name      string
		in        map[string]apiLatency
		wantMap   map[string]float64
		wantPref  string
		wantNoMap bool
	}{
		{"nil", nil, nil, "", true},
		{"empty", map[string]apiLatency{}, nil, "", true},
		{"no preferred", map[string]apiLatency{"Tokyo": {LatencyMs: 80}}, map[string]float64{"tok": 80}, "", false},
		{"preferred", map[string]apiLatency{"Tokyo": {LatencyMs: 80}, "Sydney": {Preferred: true, LatencyMs: 20}}, map[string]float64{"tok": 80, "syd": 20}, "syd", false},
		{"blank name dropped", map[string]apiLatency{"  ": {Preferred: true, LatencyMs: 1}}, nil, "", true},
		{"unknown name kept", map[string]apiLatency{"Custom DERP": {Preferred: true, LatencyMs: 5}}, map[string]float64{"custom-derp": 5}, "custom-derp", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMap, gotPref := convertLatency(tc.in)
			if tc.wantNoMap && gotMap != nil {
				t.Errorf("map = %v, want nil", gotMap)
			}
			if !tc.wantNoMap && !reflect.DeepEqual(gotMap, tc.wantMap) {
				t.Errorf("map = %v, want %v", gotMap, tc.wantMap)
			}
			if gotPref != tc.wantPref {
				t.Errorf("preferred = %q, want %q", gotPref, tc.wantPref)
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	cases := []struct {
		in       string
		wantZero bool
	}{
		{"", true},
		{"   ", true},
		{"0001-01-01T00:00:00Z", true},
		{"garbage", true},
		{"2026-09-28T05:00:00Z", false},
		{"2026-09-28T05:00:00.5+02:00", false},
	}
	for _, tc := range cases {
		got := parseTime(testLogger(), "dev", "f", tc.in)
		if got.IsZero() != tc.wantZero {
			t.Errorf("parseTime(%q) = %v, zero=%v", tc.in, got, tc.wantZero)
		}
	}
	if got := parseTime(nil, "dev", "f", "bad"); !got.IsZero() {
		t.Errorf("nil logger: %v", got)
	}
}
