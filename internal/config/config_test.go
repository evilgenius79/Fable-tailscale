package config

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// env builds a getenv func from a map.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func mustLoad(t *testing.T, args []string, e map[string]string) *Config {
	t.Helper()
	cfg, err := Load(args, env(e))
	if err != nil {
		t.Fatalf("Load(%v): %v", args, err)
	}
	return cfg
}

func TestLoadDefaults(t *testing.T) {
	cfg := mustLoad(t, nil, nil)
	want := &Config{
		Listen:           ListenAuto,
		DataDir:          DefaultDataDir,
		Admins:           []string{},
		AdminTags:        []string{},
		Viewers:          []string{"*"},
		ViewerTags:       []string{},
		Tailnet:          DefaultTailnet,
		APIBaseURL:       DefaultAPIBaseURL,
		PollInterval:     15 * time.Second,
		APIInterval:      60 * time.Second,
		PingInterval:     30 * time.Second,
		PingConcurrency:  8,
		AgentEnabled:     true,
		AgentPort:        41820,
		AgentConcurrency: 8,
		AgentTimeout:     5 * time.Second,
		RawRetention:     48 * time.Hour,
		RollupRetention:  90 * 24 * time.Hour,
		EventRetention:   90 * 24 * time.Hour,
		LogLevel:         "info",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("defaults mismatch:\n got %+v\nwant %+v", cfg, want)
	}
	if cfg.AuthMode() != "tailscale" || cfg.HasControlAPI() || len(cfg.Notifiers()) != 0 {
		t.Errorf("derived defaults: auth=%s api=%v notifiers=%v", cfg.AuthMode(), cfg.HasControlAPI(), cfg.Notifiers())
	}
	if got := cfg.DBPath(); got != filepath.Join(DefaultDataDir, DBFileName) {
		t.Errorf("DBPath = %q", got)
	}
}

func TestEnvAndFlagPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		env   map[string]string
		check func(t *testing.T, c *Config)
	}{
		{
			name: "env only",
			env:  map[string]string{"TAILWATCH_POLL_INTERVAL": "30s", "TAILWATCH_DEMO": "true", "TAILWATCH_AGENT_PORT": "5000", "TAILWATCH_LISTEN": "127.0.0.1:9000"},
			check: func(t *testing.T, c *Config) {
				if c.PollInterval != 30*time.Second || !c.Demo || c.AgentPort != 5000 || c.Listen != "127.0.0.1:9000" {
					t.Errorf("env not applied: %+v", c)
				}
			},
		},
		{
			name: "flag overrides env",
			args: []string{"--poll-interval", "45s", "--demo=false", "--agent-port=6000", "--listen", "127.0.0.1:9001"},
			env:  map[string]string{"TAILWATCH_POLL_INTERVAL": "30s", "TAILWATCH_DEMO": "true", "TAILWATCH_AGENT_PORT": "5000", "TAILWATCH_LISTEN": "127.0.0.1:9000"},
			check: func(t *testing.T, c *Config) {
				if c.PollInterval != 45*time.Second || c.Demo || c.AgentPort != 6000 || c.Listen != "127.0.0.1:9001" {
					t.Errorf("flags did not override env: %+v", c)
				}
			},
		},
		{
			name: "list flag replaces env list",
			args: []string{"--admins", "b@x.com"},
			env:  map[string]string{"TAILWATCH_ADMINS": "a@x.com,c@x.com"},
			check: func(t *testing.T, c *Config) {
				if !reflect.DeepEqual(c.Admins, []string{"b@x.com"}) {
					t.Errorf("Admins = %v, want [b@x.com]", c.Admins)
				}
			},
		},
		{
			name: "list from env",
			env:  map[string]string{"TAILWATCH_ADMINS": " a@x.com , C@x.com ", "TAILWATCH_VIEWER_TAGS": "tag:a"},
			check: func(t *testing.T, c *Config) {
				if !reflect.DeepEqual(c.Admins, []string{"a@x.com", "c@x.com"}) {
					t.Errorf("Admins = %v", c.Admins)
				}
				if !reflect.DeepEqual(c.ViewerTags, []string{"tag:a"}) {
					t.Errorf("ViewerTags = %v", c.ViewerTags)
				}
			},
		},
		{
			name: "secrets from env only",
			env: map[string]string{
				"TS_API_KEY": " tskey-api-abc ", "TS_OAUTH_CLIENT_ID": "id", "TS_OAUTH_CLIENT_SECRET": "sec",
				"TAILWATCH_AGENT_TOKEN": "agent-tok", "TAILWATCH_WEBHOOK_SECRET": "whs", "TAILWATCH_NTFY_TOKEN": "ntok",
			},
			check: func(t *testing.T, c *Config) {
				if c.APIKey != "tskey-api-abc" || c.OAuthClientID != "id" || c.OAuthClientSecret != "sec" ||
					c.AgentToken != "agent-tok" || c.WebhookSecret != "whs" || c.NtfyToken != "ntok" {
					t.Errorf("secrets not loaded: %+v", c)
				}
				if !c.HasControlAPI() {
					t.Error("HasControlAPI = false")
				}
			},
		},
		{
			name: "durations with day suffix and trailing slash trimmed",
			args: []string{"--raw-retention", "2d", "--rollup-retention=180d", "--api-base-url", "https://api.example.com/"},
			check: func(t *testing.T, c *Config) {
				if c.RawRetention != 48*time.Hour || c.RollupRetention != 180*24*time.Hour {
					t.Errorf("retention = %s / %s", c.RawRetention, c.RollupRetention)
				}
				if c.APIBaseURL != "https://api.example.com" {
					t.Errorf("APIBaseURL = %q", c.APIBaseURL)
				}
			},
		},
		{
			name: "log level normalized",
			args: []string{"--log-level", "WARN", "--log-json"},
			check: func(t *testing.T, c *Config) {
				if c.LogLevel != "warn" || !c.LogJSON || c.SlogLevel().String() != "WARN" {
					t.Errorf("log config = %q json=%v", c.LogLevel, c.LogJSON)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, mustLoad(t, tc.args, tc.env))
		})
	}
}

func TestListParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"repeatable", []string{"--admins", "a@x.com", "--admins", "b@x.com"}, []string{"a@x.com", "b@x.com"}},
		{"comma separated", []string{"--admins", "a@x.com,b@x.com"}, []string{"a@x.com", "b@x.com"}},
		{"mixed with spaces and duplicates", []string{"--admins", " a@x.com , A@X.COM ", "--admins=b@x.com,,"}, []string{"a@x.com", "b@x.com"}},
		{"empty", []string{"--admins", ""}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustLoad(t, tc.args, nil)
			if !reflect.DeepEqual(cfg.Admins, tc.want) {
				t.Errorf("Admins = %#v, want %#v", cfg.Admins, tc.want)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"15s", 15 * time.Second, false},
		{"1h30m", 90 * time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{" 0.5d ", 12 * time.Hour, false},
		{"0", 0, false},
		{"", 0, true},
		{"d", 0, true},
		{"-1d", 0, true},
		{"nope", 0, true},
		{"5", 0, true},
	}
	for _, tc := range tests {
		got, err := ParseDuration(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseDuration(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDuration(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	if FormatDuration(48*time.Hour) != "2d" || FormatDuration(90*time.Minute) != "1h30m0s" {
		t.Errorf("FormatDuration: %s %s", FormatDuration(48*time.Hour), FormatDuration(90*time.Minute))
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantSub string
	}{
		{"secret flag api-key", []string{"--api-key", "x"}, nil, "TS_API_KEY"},
		{"secret flag agent-token with equals", []string{"--agent-token=x"}, nil, "TAILWATCH_AGENT_TOKEN"},
		{"secret flag single dash", []string{"-webhook-secret", "x"}, nil, "TAILWATCH_WEBHOOK_SECRET"},
		{"secret flag ntfy-token", []string{"--ntfy-token", "x"}, nil, "TAILWATCH_NTFY_TOKEN"},
		{"secret flag oauth", []string{"--oauth-client-secret=x"}, nil, "TS_OAUTH_CLIENT_SECRET"},
		{"unknown flag", []string{"--bogus"}, nil, "bogus"},
		{"positional", []string{"extra"}, nil, "unexpected argument"},
		{"bad env bool", nil, map[string]string{"TAILWATCH_DEMO": "maybe"}, "TAILWATCH_DEMO"},
		{"bad env int", nil, map[string]string{"TAILWATCH_AGENT_PORT": "x"}, "TAILWATCH_AGENT_PORT"},
		{"bad env duration", nil, map[string]string{"TAILWATCH_POLL_INTERVAL": "soon"}, "TAILWATCH_POLL_INTERVAL"},
		{"bad flag duration", []string{"--poll-interval", "soon"}, nil, "poll-interval"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.args, env(tc.env))
			if err == nil {
				t.Fatalf("Load(%v) succeeded, want error containing %q", tc.args, tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestLoadHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		_, err := Load(args, nil)
		if !errors.Is(err, flag.ErrHelp) {
			t.Errorf("Load(%v) err = %v, want flag.ErrHelp", args, err)
		}
	}
	// --version skips validation so a broken config can still print its version.
	cfg, err := Load([]string{"--version", "--poll-interval", "1s"}, nil)
	if err != nil || !cfg.ShowVersion {
		t.Errorf("Load(--version) = %+v, %v", cfg, err)
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	for _, f := range []string{cert, key} {
		if err := os.WriteFile(f, []byte("pem"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	valid := func() *Config { return mustLoad(t, nil, nil) }

	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantSub string // "" = valid
	}{
		{"defaults valid", func(*Config) {}, ""},
		{"poll too short", func(c *Config) { c.PollInterval = 4 * time.Second }, "--poll-interval"},
		{"poll too long", func(c *Config) { c.PollInterval = 2 * time.Hour }, "--poll-interval"},
		{"api too short", func(c *Config) { c.APIInterval = 29 * time.Second }, "--api-interval"},
		{"ping too short", func(c *Config) { c.PingInterval = 5 * time.Second }, "--ping-interval"},
		{"ping disabled ok", func(c *Config) { c.PingInterval = 0 }, ""},
		{"ping negative", func(c *Config) { c.PingInterval = -time.Second }, "--ping-interval"},
		{"raw retention short", func(c *Config) { c.RawRetention = 30 * time.Minute }, "--raw-retention"},
		{"rollup retention short", func(c *Config) { c.RollupRetention = 0 }, "--rollup-retention"},
		{"event retention short", func(c *Config) { c.EventRetention = time.Minute }, "--event-retention"},
		{"agent port zero", func(c *Config) { c.AgentPort = 0 }, "--agent-port"},
		{"agent port high", func(c *Config) { c.AgentPort = 70000 }, "--agent-port"},
		{"agent concurrency zero", func(c *Config) { c.AgentConcurrency = 0 }, "--agent-concurrency"},
		{"ping concurrency huge", func(c *Config) { c.PingConcurrency = 1000 }, "--ping-concurrency"},
		{"agent timeout short", func(c *Config) { c.AgentTimeout = 100 * time.Millisecond }, "--agent-timeout"},
		{"listen empty", func(c *Config) { c.Listen = "" }, "--listen"},
		{"listen no port", func(c *Config) { c.Listen = "100.64.0.1" }, "--listen"},
		{"listen bad port", func(c *Config) { c.Listen = "100.64.0.1:0" }, "--listen"},
		{"listen empty host", func(c *Config) { c.Listen = ":8484" }, "--listen"},
		{"listen host:port ok", func(c *Config) { c.Listen = "100.64.0.1:8484" }, ""},
		{"listen ipv6 ok", func(c *Config) { c.Listen = "[::1]:8484" }, ""},
		{"insecure-no-auth with auto", func(c *Config) { c.InsecureNoAuth = true }, "--insecure-no-auth"},
		{"insecure-no-auth with tailscale ip", func(c *Config) { c.InsecureNoAuth = true; c.Listen = "100.64.0.1:8484" }, "--insecure-no-auth"},
		{"insecure-no-auth loopback ok", func(c *Config) { c.InsecureNoAuth = true; c.Listen = "127.0.0.1:8484" }, ""},
		{"insecure-no-auth localhost ok", func(c *Config) { c.InsecureNoAuth = true; c.Listen = "localhost:8484" }, ""},
		{"insecure-no-auth ipv6 loopback ok", func(c *Config) { c.InsecureNoAuth = true; c.Listen = "[::1]:8484" }, ""},
		{"demo ok", func(c *Config) { c.Demo = true }, ""},
		{"tls cert only", func(c *Config) { c.TLSCert = cert }, "--tls-cert requires --tls-key"},
		{"tls key only", func(c *Config) { c.TLSKey = key }, "--tls-key requires --tls-cert"},
		{"tls missing file", func(c *Config) { c.TLSCert = filepath.Join(dir, "nope.pem"); c.TLSKey = key }, "nope.pem"},
		{"tls directory", func(c *Config) { c.TLSCert = dir; c.TLSKey = key }, "is a directory"},
		{"tls both ok", func(c *Config) { c.TLSCert = cert; c.TLSKey = key }, ""},
		{"admin actions without api", func(c *Config) { c.EnableAdminActions = true }, "--enable-admin-actions"},
		{"admin actions with api key", func(c *Config) { c.EnableAdminActions = true; c.APIKey = "k" }, ""},
		{"admin actions with oauth", func(c *Config) { c.EnableAdminActions = true; c.OAuthClientID = "i"; c.OAuthClientSecret = "s" }, ""},
		{"admin actions in demo", func(c *Config) { c.EnableAdminActions = true; c.Demo = true }, ""},
		{"oauth id without secret", func(c *Config) { c.OAuthClientID = "i" }, "TS_OAUTH_CLIENT_SECRET"},
		{"oauth secret without id", func(c *Config) { c.OAuthClientSecret = "s" }, "TS_OAUTH_CLIENT_ID"},
		{"admins wildcard", func(c *Config) { c.Admins = []string{"*"} }, "--admins"},
		{"admin tag prefix", func(c *Config) { c.AdminTags = []string{"server"} }, "--admin-tags"},
		{"viewer tag prefix", func(c *Config) { c.ViewerTags = []string{"server"} }, "--viewer-tags"},
		{"tags ok", func(c *Config) { c.AdminTags = []string{"tag:admin"}; c.ViewerTags = []string{"tag:viewer"} }, ""},
		{"tailnet empty", func(c *Config) { c.Tailnet = "" }, "--tailnet"},
		{"api url bad scheme", func(c *Config) { c.APIBaseURL = "ftp://x" }, "--api-base-url"},
		{"api url no host", func(c *Config) { c.APIBaseURL = "https://" }, "--api-base-url"},
		{"webhook url bad", func(c *Config) { c.WebhookURL = "not a url" }, "--webhook-url"},
		{"slack url bad", func(c *Config) { c.SlackWebhookURL = "/relative" }, "--slack-webhook-url"},
		{"ntfy url ok", func(c *Config) { c.NtfyURL = "https://ntfy.sh/topic" }, ""},
		{"log level bad", func(c *Config) { c.LogLevel = "loud" }, "--log-level"},
		{"log level warning ok", func(c *Config) { c.LogLevel = "warning" }, ""},
		{"data dir empty", func(c *Config) { c.DataDir = "" }, "--data-dir"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(c)
			err := c.Validate()
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	c := mustLoad(t, nil, nil)
	c.PollInterval = time.Second
	c.AgentPort = 0
	c.LogLevel = "x"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"--poll-interval", "--agent-port", "--log-level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("joined error %q lacks %q", err, want)
		}
	}
}

func TestSettingsProjection(t *testing.T) {
	secrets := map[string]string{
		"TS_API_KEY":               "SECRET-api-key-9f8e7d",
		"TS_OAUTH_CLIENT_ID":       "SECRET-oauth-id-1a2b3c",
		"TS_OAUTH_CLIENT_SECRET":   "SECRET-oauth-secret-4d5e6f",
		"TAILWATCH_AGENT_TOKEN":    "SECRET-agent-token-7g8h9i",
		"TAILWATCH_WEBHOOK_SECRET": "SECRET-webhook-0j1k2l",
		"TAILWATCH_NTFY_TOKEN":     "SECRET-ntfy-3m4n5o",
	}
	args := []string{
		"--admins", "Alice@Example.com", "--admin-tags", "tag:admin", "--viewers", "bob@example.com", "--viewer-tags", "tag:viewer",
		"--enable-admin-actions", "--tailnet", "example.com", "--poll-interval", "20s", "--api-interval", "2m", "--ping-interval", "0",
		"--agent-port", "41821", "--agent-enabled=false", "--raw-retention", "72h", "--rollup-retention", "30d", "--event-retention", "45d",
		"--webhook-url", "https://hooks.example.com/tw?token=SECRET-in-url", "--ntfy-url", "https://ntfy.sh/topic", "--listen", "100.64.0.9:8484",
	}
	cfg := mustLoad(t, args, secrets)
	now := time.Now()
	stats := model.StoreStats{Devices: 3, Samples: 100, SizeBytes: 4096, OldestSample: &now}
	s := cfg.Settings(stats)

	want := model.Settings{
		AuthMode:            "tailscale",
		AdminUsers:          []string{"alice@example.com"},
		AdminTags:           []string{"tag:admin"},
		ViewerUsers:         []string{"bob@example.com"},
		ViewerTags:          []string{"tag:viewer"},
		AdminActions:        true,
		ControlAPI:          true,
		Tailnet:             "example.com",
		PollIntervalSec:     20,
		APIIntervalSec:      120,
		PingIntervalSec:     0,
		AgentPort:           41821,
		AgentEnabled:        false,
		RawRetentionHours:   72,
		RollupRetentionDays: 30,
		EventRetentionDays:  45,
		Notifiers:           []string{"webhook", "ntfy"},
		Listen:              "100.64.0.9:8484",
		DemoMode:            false,
		Store:               stats,
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("Settings mismatch:\n got %+v\nwant %+v", s, want)
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, v := range secrets {
		if strings.Contains(js, v) {
			t.Errorf("settings JSON leaks secret %q: %s", v, js)
		}
	}
	for _, sub := range []string{"SECRET", "hooks.example.com", "apiKey", "token", "Token"} {
		if strings.Contains(js, sub) {
			t.Errorf("settings JSON contains %q: %s", sub, js)
		}
	}

	// Copies, not aliases.
	s.AdminUsers[0] = "mallory"
	if cfg.Admins[0] != "alice@example.com" {
		t.Error("Settings aliases Config.Admins")
	}
}

func TestSettingsAuthModeAndEmptyLists(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default tailscale", nil, "tailscale"},
		{"demo none", []string{"--demo"}, "none"},
		{"insecure none", []string{"--insecure-no-auth", "--listen", "127.0.0.1:8484"}, "none"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustLoad(t, tc.args, nil)
			s := cfg.Settings(model.StoreStats{})
			if s.AuthMode != tc.want {
				t.Errorf("AuthMode = %q, want %q", s.AuthMode, tc.want)
			}
			b, _ := json.Marshal(s)
			if strings.Contains(string(b), "null") {
				t.Errorf("settings JSON has null lists: %s", b)
			}
			if tc.name == "demo none" && !s.ControlAPI {
				t.Error("demo should report the control API as available")
			}
		})
	}
}

func TestUsage(t *testing.T) {
	u := Usage()
	for _, want := range []string{
		"Server:", "Access control:", "Tailscale control API:", "Collection:", "Retention:", "Notifications:", "Logging:",
		"--listen", "--admins", "--enable-admin-actions", "--poll-interval", "--raw-retention", "--webhook-url", "--log-level",
		"env TAILWATCH_LISTEN", "env TS_API_KEY", "env TAILWATCH_AGENT_TOKEN", "env TAILWATCH_WEBHOOK_SECRET", "env TAILWATCH_NTFY_TOKEN",
		`(default "auto")`, "(default 15s)", "(default 90d)", `(default "*")`,
	} {
		if !strings.Contains(u, want) {
			t.Errorf("usage lacks %q", want)
		}
	}
	for _, never := range []string{"--api-key", "--agent-token", "--webhook-secret", "--ntfy-token", "--oauth-client"} {
		if strings.Contains(u, never) {
			t.Errorf("usage advertises secret flag %q", never)
		}
	}
}

func TestIsLoopbackListen(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:8484", true},
		{"127.5.5.5:1", true},
		{"[::1]:8484", true},
		{"localhost:8484", true},
		{"LOCALHOST:8484", true},
		{"[::ffff:127.0.0.1]:8484", true},
		{"100.64.0.1:8484", false},
		{"0.0.0.0:8484", false},
		{"auto", false},
		{"127.0.0.1", false},
		{"example.com:80", false},
	}
	for _, tc := range tests {
		if got := IsLoopbackListen(tc.in); got != tc.want {
			t.Errorf("IsLoopbackListen(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
