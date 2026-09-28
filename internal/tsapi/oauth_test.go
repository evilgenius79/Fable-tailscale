package tsapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// oauthServer is a fake token endpoint + devices endpoint.
type oauthServer struct {
	mu          sync.Mutex
	tokenCalls  int
	deviceCalls int
	// issue returns the token response for the n-th (1-based) token call.
	issue func(n int, w http.ResponseWriter, r *http.Request)
	// devices handles the devices call; auth is the bearer token presented.
	devices func(auth string, w http.ResponseWriter, r *http.Request)
}

func (s *oauthServer) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case oauthTokenPath:
		s.mu.Lock()
		s.tokenCalls++
		n := s.tokenCalls
		s.mu.Unlock()
		s.issue(n, w, r)
	case "/api/v2/tailnet/-/devices":
		s.mu.Lock()
		s.deviceCalls++
		s.mu.Unlock()
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.devices != nil {
			s.devices(auth, w, r)
			return
		}
		okDevices(w, r)
	default:
		jsonStatus(w, 404, `{"message":"no such endpoint"}`)
	}
}

func (s *oauthServer) counts() (tokens, devices int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenCalls, s.deviceCalls
}

// issueSequential returns "tok-<n>" for the n-th call after validating the
// form-encoded client credentials.
func issueSequential(t *testing.T, expiresIn string) func(int, http.ResponseWriter, *http.Request) {
	return func(n int, w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("token content-type = %q", ct)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("token request must not carry a bearer token")
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.PostForm.Get("client_id") != "client-id" || r.PostForm.Get("client_secret") != "client-secret" || r.PostForm.Get("grant_type") != "client_credentials" {
			t.Errorf("unexpected form: %v", r.PostForm)
		}
		exp := ""
		if expiresIn != "" {
			exp = `,"expires_in":` + expiresIn
		}
		jsonStatus(w, 200, fmt.Sprintf(`{"access_token":"tok-%d","token_type":"Bearer"%s}`, n, exp))
	}
}

func oauthOpts(o *Options) {
	o.APIKey = ""
	o.OAuthClientID = "client-id"
	o.OAuthClientSecret = "client-secret"
}

func TestOAuthTokenCachingAndRefresh(t *testing.T) {
	var lastAuth atomic.Value
	srv := &oauthServer{
		issue: issueSequential(t, "3600"),
		devices: func(auth string, w http.ResponseWriter, r *http.Request) {
			lastAuth.Store(auth)
			okDevices(w, r)
		},
	}
	env := newEnv(t, srv.handler, oauthOpts)
	if !env.c.Configured() {
		t.Fatal("oauth client should be configured")
	}
	call := func(wantTok string, wantTokenCalls int) {
		t.Helper()
		if _, err := env.c.Devices(context.Background()); err != nil {
			t.Fatalf("Devices: %v", err)
		}
		if got, _ := lastAuth.Load().(string); got != wantTok {
			t.Errorf("bearer = %q, want %q", got, wantTok)
		}
		if tokens, _ := srv.counts(); tokens != wantTokenCalls {
			t.Errorf("token calls = %d, want %d", tokens, wantTokenCalls)
		}
	}
	call("tok-1", 1)
	call("tok-1", 1) // cached
	env.clock.advance(3600*time.Second - 61*time.Second)
	call("tok-1", 1) // still more than 60s left
	env.clock.advance(time.Second)
	call("tok-2", 2) // within the 60s refresh margin
	env.clock.advance(3540 * time.Second)
	call("tok-3", 3)
	if _, devices := srv.counts(); devices != 5 {
		t.Errorf("device calls = %d, want 5", devices)
	}
}

func TestOAuthDefaultLifetimeWhenExpiresInMissing(t *testing.T) {
	srv := &oauthServer{issue: issueSequential(t, "")}
	env := newEnv(t, srv.handler, oauthOpts)
	if _, err := env.c.Devices(context.Background()); err != nil {
		t.Fatalf("Devices: %v", err)
	}
	env.clock.advance(defaultTokenLifetime - 2*tokenRefreshMargin)
	if _, err := env.c.Devices(context.Background()); err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if tokens, _ := srv.counts(); tokens != 1 {
		t.Errorf("token calls = %d, want 1", tokens)
	}
	env.clock.advance(2 * tokenRefreshMargin)
	if _, err := env.c.Devices(context.Background()); err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if tokens, _ := srv.counts(); tokens != 2 {
		t.Errorf("token calls = %d, want 2", tokens)
	}
}

func TestOAuthBasicAuthFallback(t *testing.T) {
	type tokenReq struct {
		basicUser, basicPass string
		hasBasic             bool
		formSecret           string
	}
	var mu sync.Mutex
	var seen []tokenReq
	srv := &oauthServer{}
	srv.issue = func(n int, w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		u, p, ok := r.BasicAuth()
		mu.Lock()
		seen = append(seen, tokenReq{u, p, ok, r.PostForm.Get("client_secret")})
		mu.Unlock()
		if !ok {
			// This server only speaks RFC 6749 §2.3.1 basic auth.
			jsonStatus(w, 401, `{"error":"invalid_client","error_description":"use basic auth"}`)
			return
		}
		if u != "client-id" || p != "client-secret" {
			jsonStatus(w, 401, `{"error":"invalid_client"}`)
			return
		}
		jsonStatus(w, 200, fmt.Sprintf(`{"access_token":"tok-%d","token_type":"bearer","expires_in":100}`, n))
	}
	env := newEnv(t, srv.handler, oauthOpts)
	if _, err := env.c.Devices(context.Background()); err != nil {
		t.Fatalf("Devices: %v", err)
	}
	mu.Lock()
	got := append([]tokenReq(nil), seen...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("token requests = %d, want 2 (form, then basic)", len(got))
	}
	if got[0].hasBasic || got[0].formSecret != "client-secret" {
		t.Errorf("first token request should use form credentials: %+v", got[0])
	}
	if !got[1].hasBasic || got[1].basicUser != "client-id" || got[1].basicPass != "client-secret" || got[1].formSecret != "" {
		t.Errorf("second token request should use basic auth only: %+v", got[1])
	}
	// The working style is remembered: the next refresh goes straight to basic.
	env.clock.advance(time.Hour)
	if _, err := env.c.Devices(context.Background()); err != nil {
		t.Fatalf("Devices after expiry: %v", err)
	}
	mu.Lock()
	got = append([]tokenReq(nil), seen...)
	mu.Unlock()
	if len(got) != 3 || !got[2].hasBasic {
		t.Errorf("refresh should use basic auth directly, got %d requests: %+v", len(got), got)
	}
}

func TestOAuthSingleFlight(t *testing.T) {
	srv := &oauthServer{}
	srv.issue = func(n int, w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond) // give every goroutine time to pile up
		jsonStatus(w, 200, fmt.Sprintf(`{"access_token":"tok-%d","expires_in":3600}`, n))
	}
	env := newEnv(t, srv.handler, oauthOpts)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := env.c.Devices(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Devices: %v", err)
		}
	}
	tokens, devices := srv.counts()
	if tokens != 1 {
		t.Errorf("token calls = %d, want 1 (single-flight)", tokens)
	}
	if devices != workers {
		t.Errorf("device calls = %d, want %d", devices, workers)
	}
}

func TestOAuthReauthOnRejectedToken(t *testing.T) {
	t.Run("recovers with fresh token", func(t *testing.T) {
		srv := &oauthServer{issue: issueSequential(t, "3600")}
		srv.devices = func(auth string, w http.ResponseWriter, r *http.Request) {
			if auth == "tok-1" {
				jsonStatus(w, 401, `{"message":"invalid token"}`)
				return
			}
			okDevices(w, r)
		}
		env := newEnv(t, srv.handler, oauthOpts)
		if _, err := env.c.Devices(context.Background()); err != nil {
			t.Fatalf("Devices: %v", err)
		}
		tokens, devices := srv.counts()
		if tokens != 2 || devices != 2 {
			t.Errorf("token calls = %d, device calls = %d; want 2 and 2", tokens, devices)
		}
		if len(env.sleep.all()) != 0 {
			t.Errorf("re-auth must not sleep")
		}
	})
	t.Run("gives up after one re-auth", func(t *testing.T) {
		srv := &oauthServer{issue: issueSequential(t, "3600")}
		srv.devices = func(auth string, w http.ResponseWriter, r *http.Request) {
			jsonStatus(w, 401, `{"message":"invalid token"}`)
		}
		env := newEnv(t, srv.handler, oauthOpts)
		_, err := env.c.Devices(context.Background())
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 401 {
			t.Fatalf("err = %v, want APIError 401", err)
		}
		tokens, devices := srv.counts()
		if tokens != 2 || devices != 2 {
			t.Errorf("token calls = %d, device calls = %d; want 2 and 2", tokens, devices)
		}
	})
	t.Run("api key is never re-fetched", func(t *testing.T) {
		env := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
			jsonStatus(w, 401, `{"message":"bad key"}`)
		}, nil)
		_, err := env.c.Devices(context.Background())
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 401 {
			t.Fatalf("err = %v, want APIError 401", err)
		}
		if n := env.rec.count(""); n != 1 {
			t.Errorf("requests = %d, want 1", n)
		}
	})
}

func TestOAuthTokenEndpointErrors(t *testing.T) {
	cases := []struct {
		name           string
		issue          func(n int, w http.ResponseWriter, r *http.Request)
		wantStatus     int // APIError status, 0 if not an APIError
		wantText       string
		wantTokenCalls int
		wantSleeps     int
	}{
		{
			name: "server error retried once then fails",
			issue: func(n int, w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 500, `{"message":"oops"}`)
			},
			wantStatus: 500, wantText: "oauth token", wantTokenCalls: 2, wantSleeps: 1,
		},
		{
			name: "forbidden is not retried with basic",
			issue: func(n int, w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 403, `{"message":"client disabled"}`)
			},
			wantStatus: 403, wantText: "client disabled", wantTokenCalls: 1,
		},
		{
			name: "unauthorized with both styles",
			issue: func(n int, w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 401, `{"error":"invalid_client","error_description":"unknown client"}`)
			},
			wantStatus: 401, wantText: "unknown client", wantTokenCalls: 2,
		},
		{
			name: "missing access_token",
			issue: func(n int, w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 200, `{"token_type":"bearer","expires_in":3600}`)
			},
			wantText: "no access_token", wantTokenCalls: 1,
		},
		{
			name: "malformed json",
			issue: func(n int, w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 200, `{"access_token":`)
			},
			wantText: "decode response", wantTokenCalls: 1,
		},
		{
			name: "token with control characters",
			issue: func(n int, w http.ResponseWriter, r *http.Request) {
				jsonStatus(w, 200, `{"access_token":"bad\ntoken","expires_in":3600}`)
			},
			wantText: "not allowed in a header", wantTokenCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &oauthServer{issue: tc.issue}
			env := newEnv(t, srv.handler, oauthOpts)
			_, err := env.c.Devices(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("err %q lacks %q", err.Error(), tc.wantText)
			}
			if !strings.HasPrefix(err.Error(), "tsapi: devices: ") {
				t.Errorf("err %q lacks op prefix", err.Error())
			}
			var apiErr *APIError
			if tc.wantStatus != 0 {
				if !errors.As(err, &apiErr) || apiErr.Status != tc.wantStatus {
					t.Errorf("err = %v, want APIError %d", err, tc.wantStatus)
				}
			} else if errors.As(err, &apiErr) {
				t.Errorf("err = %v should not be an APIError", err)
			}
			if strings.Contains(err.Error(), "client-secret") {
				t.Errorf("error leaks client secret: %v", err)
			}
			tokens, devices := srv.counts()
			if tokens != tc.wantTokenCalls || devices != 0 {
				t.Errorf("token calls = %d (want %d), device calls = %d (want 0)", tokens, tc.wantTokenCalls, devices)
			}
			if n := len(env.sleep.all()); n != tc.wantSleeps {
				t.Errorf("sleeps = %d, want %d", n, tc.wantSleeps)
			}
		})
	}
}

func TestInvalidateTokenOnlyDropsMatchingToken(t *testing.T) {
	c := New(Options{OAuthClientID: "id", OAuthClientSecret: "s"}, testLogger())
	c.tok, c.tokExp = "fresh", time.Now().Add(time.Hour)
	c.invalidateToken("stale")
	if tok, ok := c.cachedToken(); !ok || tok != "fresh" {
		t.Errorf("fresh token was dropped: %q %v", tok, ok)
	}
	c.invalidateToken("fresh")
	if _, ok := c.cachedToken(); ok {
		t.Error("token should have been invalidated")
	}
}
