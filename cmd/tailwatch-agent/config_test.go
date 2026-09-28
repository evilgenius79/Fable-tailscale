package main

import (
	"errors"
	"flag"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(nil, envMap(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != listenAuto || cfg.Port != agentproto.DefaultPort || cfg.Auth != authWhois ||
		cfg.Interval != defaultInterval || cfg.LogLevel != "info" || cfg.LogJSON || cfg.InsecureListenAny ||
		cfg.Token != "" || cfg.AllowUsers != nil || cfg.AllowTags != nil || cfg.AllowNodes != nil || cfg.Socket != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", cfg.Warnings)
	}
}

func TestParseConfigEnvFallbackAndFlagOverride(t *testing.T) {
	env := envMap(map[string]string{
		"TAILWATCH_AGENT_PORT":                "5000",
		"TAILWATCH_AGENT_LISTEN":              "100.64.0.1:5000",
		"TAILWATCH_AGENT_ALLOW_TAG":           "tag:a, tag:b,,",
		"TAILWATCH_AGENT_ALLOW_USER":          "Alice@Example.com",
		"TAILWATCH_AGENT_ALLOW_NODE":          "Hub.example.ts.net.",
		"TAILWATCH_AGENT_INTERVAL":            "10",
		"TAILWATCH_AGENT_SOCKET":              "/tmp/ts.sock",
		"TAILWATCH_AGENT_INSECURE_LISTEN_ANY": "true",
		"TAILWATCH_AGENT_LOG_LEVEL":           "DEBUG",
		"TAILWATCH_AGENT_LOG_JSON":            "1",
	})
	cfg, err := parseConfig([]string{"--port", "6000", "--allow-tag", "tag:c", "--allow-tag", "tag:a"}, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 6000 {
		t.Errorf("flag should override env port: %d", cfg.Port)
	}
	if cfg.Listen != "100.64.0.1:5000" || cfg.Socket != "/tmp/ts.sock" || !cfg.InsecureListenAny || cfg.LogLevel != "debug" || !cfg.LogJSON {
		t.Errorf("env values not applied: %+v", cfg)
	}
	if want := []string{"tag:a", "tag:b", "tag:c"}; !slices.Equal(cfg.AllowTags, want) {
		t.Errorf("AllowTags = %v, want %v", cfg.AllowTags, want)
	}
	if want := []string{"alice@example.com"}; !slices.Equal(cfg.AllowUsers, want) {
		t.Errorf("AllowUsers = %v, want %v", cfg.AllowUsers, want)
	}
	if want := []string{"hub.example.ts.net"}; !slices.Equal(cfg.AllowNodes, want) {
		t.Errorf("AllowNodes = %v, want %v", cfg.AllowNodes, want)
	}
	if cfg.Interval != 10*time.Second {
		t.Errorf("Interval = %s", cfg.Interval)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "insecure-listen-any") {
		t.Errorf("Warnings = %v", cfg.Warnings)
	}
}

func TestParseConfigToken(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		env      map[string]string
		wantErr  string
		wantTok  string
		wantWarn string
	}{
		{"token mode without token", []string{"--auth", "token"}, nil, "requires the shared token", "", ""},
		{"both mode without token", []string{"--auth", "both"}, nil, "requires the shared token", "", ""},
		{"token from env trimmed", []string{"--auth", "token"}, map[string]string{"TAILWATCH_AGENT_TOKEN": " s3cret-s3cret-s3cret \n"}, "", "s3cret-s3cret-s3cret", ""},
		{"short token warns", []string{"--auth", "both"}, map[string]string{"TAILWATCH_AGENT_TOKEN": "short"}, "", "short", "shorter than"},
		{"token ignored in whois mode warns", nil, map[string]string{"TAILWATCH_AGENT_TOKEN": "abcdefghijklmnopqrstuvwxyz"}, "", "abcdefghijklmnopqrstuvwxyz", "ignores it"},
		{"token flag refused", []string{"--token", "x"}, nil, "not as a flag", "", ""},
		{"token= flag refused", []string{"--token=x"}, nil, "not as a flag", "", ""},
		{"allow-lists ignored in token mode warns", []string{"--auth", "token", "--allow-user", "a"}, map[string]string{"TAILWATCH_AGENT_TOKEN": "abcdefghijklmnopqrstuvwxyz"}, "", "abcdefghijklmnopqrstuvwxyz", "ignored with --auth token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig(tc.args, envMap(tc.env), io.Discard)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Token != tc.wantTok {
				t.Fatalf("Token = %q, want %q", cfg.Token, tc.wantTok)
			}
			if tc.wantWarn != "" {
				found := false
				for _, w := range cfg.Warnings {
					if strings.Contains(w, tc.wantWarn) {
						found = true
					}
				}
				if !found {
					t.Fatalf("Warnings = %v, want containing %q", cfg.Warnings, tc.wantWarn)
				}
			}
		})
	}
}

func TestParseConfigRejectsBadValues(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{"bad auth", []string{"--auth", "nope"}, nil, "--auth"},
		{"port too high", []string{"--port", "70000"}, nil, "out of range"},
		{"port zero", []string{"--port", "0"}, nil, "out of range"},
		{"interval too small", []string{"--interval", "100ms"}, nil, "--interval"},
		{"interval too large", []string{"--interval", "2h"}, nil, "--interval"},
		{"tag without prefix", []string{"--allow-tag", "server"}, nil, "must start with"},
		{"listen hostname", []string{"--listen", "example.com:1"}, nil, "IP literal"},
		{"listen bad port", []string{"--listen", "100.64.0.1:99999"}, nil, "invalid port"},
		{"listen empty", []string{"--listen", ""}, nil, "must not be empty"},
		{"log level", []string{"--log-level", "loud"}, nil, "--log-level"},
		{"unknown flag", []string{"--bogus"}, nil, "parse flags"},
		{"positional", []string{"extra"}, nil, "unexpected argument"},
		{"env port not int", nil, map[string]string{"TAILWATCH_AGENT_PORT": "abc"}, "not an integer"},
		{"env bool bad", nil, map[string]string{"TAILWATCH_AGENT_LOG_JSON": "maybe"}, "not a boolean"},
		{"env duration bad", nil, map[string]string{"TAILWATCH_AGENT_INTERVAL": "soon"}, "not a duration"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.args, envMap(tc.env), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseConfigHelpAndVersion(t *testing.T) {
	var out strings.Builder
	_, err := parseConfig([]string{"-h"}, envMap(nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out.String(), envToken) || strings.Contains(out.String(), "-token ") {
		t.Fatalf("usage should mention %s and not define a -token flag:\n%s", envToken, out.String())
	}
	cfg, err := parseConfig([]string{"--version"}, envMap(nil), io.Discard)
	if err != nil || !cfg.ShowVersion {
		t.Fatalf("--version: cfg=%+v err=%v", cfg, err)
	}
}

func TestSplitListen(t *testing.T) {
	tests := []struct {
		in       string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{"100.64.0.1:41820", "100.64.0.1", 41820, false},
		{"100.64.0.1", "100.64.0.1", 1234, false},
		{"[fd7a:115c:a1e0::1]:99", "fd7a:115c:a1e0::1", 99, false},
		{"fd7a:115c:a1e0::1", "fd7a:115c:a1e0::1", 1234, false},
		{"localhost:80", "127.0.0.1", 80, false},
		{"127.0.0.1:", "127.0.0.1", 1234, false},
		{":41820", "", 0, true},
		{"example.com:1", "", 0, true},
		{"100.64.0.1:0", "", 0, true},
		{"100.64.0.1:abc", "", 0, true},
		{"", "", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			h, p, err := splitListen(tc.in, 1234)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && (h != tc.wantHost || p != tc.wantPort) {
				t.Fatalf("got %s:%d, want %s:%d", h, p, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"5": 5 * time.Second, "5s": 5 * time.Second, "2m": 2 * time.Minute} {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseDuration("later"); err == nil {
		t.Error("expected error")
	}
}
