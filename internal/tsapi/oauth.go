package tsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// tokenRefreshMargin is how long before expiry a cached token is
	// considered stale and refreshed.
	tokenRefreshMargin = 60 * time.Second
	// defaultTokenLifetime is assumed when the token endpoint omits expires_in.
	defaultTokenLifetime = time.Hour
	oauthTokenPath       = "/api/v2/oauth/token"
)

// bearer returns the value to send as the bearer token: the API key, or a
// valid (possibly freshly fetched) OAuth access token.
func (c *Client) bearer(ctx context.Context) (string, error) {
	if c.apiKey != "" {
		return c.apiKey, nil
	}
	if tok, ok := c.cachedToken(); ok {
		return tok, nil
	}
	// Single-flight: one fetch at a time; waiters re-check the cache.
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if tok, ok := c.cachedToken(); ok {
		return tok, nil
	}
	tok, exp, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	c.tokMu.Lock()
	c.tok, c.tokExp = tok, exp
	c.tokMu.Unlock()
	c.log.Debug("obtained OAuth access token", "expiresIn", exp.Sub(c.now()).Round(time.Second))
	return tok, nil
}

// cachedToken returns the cached access token if it is still valid for more
// than tokenRefreshMargin.
func (c *Client) cachedToken() (string, bool) {
	c.tokMu.RLock()
	defer c.tokMu.RUnlock()
	if c.tok == "" || !c.now().Before(c.tokExp.Add(-tokenRefreshMargin)) {
		return "", false
	}
	return c.tok, true
}

// peekToken returns the cached token regardless of validity.
func (c *Client) peekToken() string {
	c.tokMu.RLock()
	defer c.tokMu.RUnlock()
	return c.tok
}

// invalidateToken forgets the cached token if it is still the given one, so
// a token refreshed concurrently by another caller is not thrown away.
func (c *Client) invalidateToken(tok string) {
	c.tokMu.Lock()
	defer c.tokMu.Unlock()
	if c.tok == tok {
		c.tok, c.tokExp = "", time.Time{}
	}
}

// fetchToken performs the client_credentials grant. Credentials are sent as
// form fields (the documented Tailscale style); if the server rejects them
// with 401 or invalid_client, HTTP basic authentication (RFC 6749 §2.3.1) is
// tried and remembered. Must be called with refreshMu held.
func (c *Client) fetchToken(ctx context.Context) (string, time.Time, error) {
	for attempt := 0; ; attempt++ {
		form := url.Values{"grant_type": {"client_credentials"}}
		r := request{
			method:      http.MethodPost,
			path:        oauthTokenPath,
			contentType: "application/x-www-form-urlencoded",
			noAuth:      true,
		}
		if c.useBasic {
			r.basicUser, r.basicPass = c.oauthID, c.oauthSecret
		} else {
			form.Set("client_id", c.oauthID)
			form.Set("client_secret", c.oauthSecret)
		}
		r.body = []byte(form.Encode())

		resp, err := c.do(ctx, "oauth token", r)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("oauth token: %w", err)
		}
		rejected := resp.status == http.StatusUnauthorized ||
			(resp.status == http.StatusBadRequest && oauthErrorCode(resp.body) == "invalid_client")
		if rejected && !c.useBasic && attempt == 0 {
			c.useBasic = true
			c.log.Debug("oauth token endpoint rejected form credentials; retrying with HTTP basic auth")
			continue
		}
		if err := checkStatus(resp); err != nil {
			return "", time.Time{}, fmt.Errorf("oauth token: %w", err)
		}

		var tr struct {
			AccessToken string  `json:"access_token"`
			TokenType   string  `json:"token_type"`
			ExpiresIn   float64 `json:"expires_in"`
		}
		if err := json.Unmarshal(resp.body, &tr); err != nil {
			return "", time.Time{}, fmt.Errorf("oauth token: decode response: %w", err)
		}
		tr.AccessToken = strings.TrimSpace(tr.AccessToken)
		switch {
		case tr.AccessToken == "":
			return "", time.Time{}, errors.New("oauth token: response contains no access_token")
		case !headerSafe(tr.AccessToken):
			return "", time.Time{}, errors.New("oauth token: access_token contains characters not allowed in a header")
		}
		if tr.TokenType != "" && !strings.EqualFold(tr.TokenType, "bearer") {
			c.log.Warn("oauth token endpoint returned unexpected token_type; using it as bearer", "tokenType", sanitizeMessage(tr.TokenType))
		}
		lifetime := defaultTokenLifetime
		if tr.ExpiresIn > 0 && tr.ExpiresIn < float64(time.Duration(1<<62)/time.Second) {
			lifetime = time.Duration(tr.ExpiresIn * float64(time.Second))
		}
		return tr.AccessToken, c.now().Add(lifetime), nil
	}
}
