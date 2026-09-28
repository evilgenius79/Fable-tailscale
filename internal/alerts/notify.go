package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

const (
	// userAgent is sent with every outbound notification request.
	userAgent = "tailwatch"
	// maxDrainBytes bounds how much of a response body is read (and
	// discarded) so connections can be reused.
	maxDrainBytes = 4096
	// maxChatTextLen keeps Slack/Discord messages under their limits.
	maxChatTextLen = 1900
	// headerSignature carries the HMAC of the webhook body.
	headerSignature = "X-Tailwatch-Signature"
)

// httpTarget is the shared HTTP plumbing of the built-in notifiers. The
// configured URL may embed a secret (Slack and Discord webhooks do, ntfy
// URLs may carry a token in the query), so it is never logged and every
// error refers to the endpoint by scheme and host only.
type httpTarget struct {
	name    string
	rawURL  string // full URL; never logged
	display string // scheme://host, safe for errors and logs
	err     error  // set when the configured URL is unusable
	hc      *http.Client
}

// newTarget parses rawURL and prepares an HTTP client. Redirects are not
// followed by the default client so a token is never re-sent to a different
// host.
func newTarget(name, rawURL string, hc *http.Client) httpTarget {
	t := httpTarget{name: name, hc: hc, display: name + " endpoint"}
	if t.hc == nil {
		t.hc = &http.Client{
			Timeout: notifyTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		t.err = fmt.Errorf("%s: invalid URL: an http(s) URL with a host is required", name)
		return t
	}
	t.rawURL = u.String()
	t.display = u.Scheme + "://" + u.Host
	return t
}

// post sends body with the given headers and returns an error for
// transport failures and non-2xx responses.
func (t *httpTarget) post(ctx context.Context, body []byte, hdr http.Header) error {
	if t.err != nil {
		return t.err
	}
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.rawURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: build request for %s: %w", t.name, t.display, t.scrub(err))
	}
	req.Header.Set("User-Agent", userAgent)
	for k, vs := range hdr {
		req.Header[http.CanonicalHeaderKey(k)] = append([]string(nil), vs...)
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s: post to %s: %w", t.name, t.display, t.scrub(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s: %s responded with status %d", t.name, t.display, resp.StatusCode)
	}
	return nil
}

// scrub removes the request URL from a transport error. net/url and net/http
// errors embed the full URL (including any token in its path or query), so
// the underlying cause is kept for errors.Is/As while the text is rewritten.
func (t *httpTarget) scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	msg := err.Error()
	if t.rawURL != "" {
		msg = strings.ReplaceAll(msg, t.rawURL, t.display)
		if u, perr := url.Parse(t.rawURL); perr == nil {
			if pq := u.RequestURI(); len(pq) > 1 {
				msg = strings.ReplaceAll(msg, pq, "/[redacted]")
			}
			if u.RawQuery != "" {
				msg = strings.ReplaceAll(msg, u.RawQuery, "[redacted]")
			}
		}
	}
	return &scrubbedError{msg: msg, cause: err}
}

// scrubbedError is a transport error whose text has been redacted.
type scrubbedError struct {
	msg   string
	cause error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.cause }

// --- webhook --------------------------------------------------------------

type webhookNotifier struct {
	t      httpTarget
	secret []byte
}

// NewWebhook returns a notifier that POSTs the Notification as JSON
// ({"type","alert","hub"}) to url with Content-Type application/json and
// User-Agent tailwatch. When secret is non-empty the request carries
// X-Tailwatch-Signature: sha256=<hex HMAC-SHA256 of the body>. A nil hc
// uses a client with a 10s timeout that does not follow redirects.
func NewWebhook(url, secret string, hc *http.Client) Notifier {
	return &webhookNotifier{t: newTarget("webhook", url, hc), secret: []byte(secret)}
}

func (w *webhookNotifier) Name() string { return "webhook" }

func (w *webhookNotifier) Send(ctx context.Context, n Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("webhook: encode notification: %w", err)
	}
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	if len(w.secret) > 0 {
		hdr.Set(headerSignature, "sha256="+signBody(w.secret, body))
	}
	return w.t.post(ctx, body, hdr)
}

// signBody returns the hex HMAC-SHA256 of body under key.
func signBody(key, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- slack / discord ------------------------------------------------------

type slackNotifier struct {
	t httpTarget
}

// NewSlack returns a notifier for Slack-compatible incoming webhooks. The
// payload carries the same one-line text as both "text" (Slack) and
// "content" (Discord), so either service accepts it.
func NewSlack(url string, hc *http.Client) Notifier {
	return &slackNotifier{t: newTarget("slack", url, hc)}
}

func (s *slackNotifier) Name() string { return "slack" }

func (s *slackNotifier) Send(ctx context.Context, n Notification) error {
	text := chatText(n)
	body, err := json.Marshal(map[string]string{"text": text, "content": text})
	if err != nil {
		return fmt.Errorf("slack: encode payload: %w", err)
	}
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	return s.t.post(ctx, body, hdr)
}

// chatText renders "<emoji> [severity] title — message (device)".
func chatText(n Notification) string {
	a := n.Alert
	label := string(a.Severity)
	if n.Type == model.EventAlertResolved {
		label = "resolved"
	}
	emoji, _ := glyphs(n)
	var b strings.Builder
	b.WriteString(emoji)
	b.WriteString(" [")
	b.WriteString(label)
	b.WriteString("] ")
	b.WriteString(a.Title)
	if a.Message != "" {
		b.WriteString(" — ")
		b.WriteString(a.Message)
	}
	if a.DeviceName != "" && !strings.Contains(a.Title, a.DeviceName) {
		b.WriteString(" (")
		b.WriteString(a.DeviceName)
		b.WriteString(")")
	}
	return truncate(b.String(), maxChatTextLen)
}

// glyphs returns the emoji and ntfy tag for a notification.
func glyphs(n Notification) (emoji, tag string) {
	if n.Type == model.EventAlertResolved {
		return "✅", "white_check_mark"
	}
	switch n.Alert.Severity {
	case model.SeverityCritical:
		return "🚨", "rotating_light"
	case model.SeverityWarning:
		return "⚠️", "warning"
	default:
		return "ℹ️", "information_source"
	}
}

// --- ntfy -----------------------------------------------------------------

type ntfyNotifier struct {
	t     httpTarget
	token string
}

// NewNtfy returns a notifier that publishes to an ntfy topic URL (for
// example https://ntfy.sh/mytopic). The alert message is the plain-text
// body; Title, Priority (critical=5, warning=4, info=3) and Tags
// (rotating_light / warning / information_source) headers are set, and
// Authorization: Bearer token is sent when token is non-empty.
func NewNtfy(url, token string, hc *http.Client) Notifier {
	return &ntfyNotifier{t: newTarget("ntfy", url, hc), token: token}
}

func (nt *ntfyNotifier) Name() string { return "ntfy" }

func (nt *ntfyNotifier) Send(ctx context.Context, n Notification) error {
	a := n.Alert
	title := a.Title
	if n.Type == model.EventAlertResolved {
		title = "Resolved: " + title
	}
	_, tag := glyphs(n)
	hdr := http.Header{}
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	hdr.Set("Title", headerValue(title))
	hdr.Set("Priority", ntfyPriority(n))
	hdr.Set("Tags", tag)
	if nt.token != "" {
		hdr.Set("Authorization", "Bearer "+nt.token)
	}
	body := a.Message
	if body == "" {
		body = a.Title
	}
	return nt.t.post(ctx, []byte(body), hdr)
}

// ntfyPriority maps severity to ntfy's 1..5 scale; resolutions are sent at
// the default priority.
func ntfyPriority(n Notification) string {
	if n.Type == model.EventAlertResolved {
		return "3"
	}
	switch n.Alert.Severity {
	case model.SeverityCritical:
		return "5"
	case model.SeverityWarning:
		return "4"
	default:
		return "3"
	}
}

// headerValue makes s safe for an HTTP header: control characters become
// spaces and non-ASCII text is RFC 2047 encoded, as ntfy expects.
func headerValue(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}
