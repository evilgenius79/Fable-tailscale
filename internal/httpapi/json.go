package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/evilgenius79/fable-tailscale/internal/alerts"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
	"github.com/evilgenius79/fable-tailscale/internal/tsapi"
	"github.com/evilgenius79/fable-tailscale/internal/tslocal"
)

// Error codes from docs/API.md.
const (
	codeBadRequest    = "bad_request"
	codeUnauthorized  = "unauthorized"
	codeForbidden     = "forbidden"
	codeNotFound      = "not_found"
	codeRateLimited   = "rate_limited"
	codeNotConfigured = "not_configured"
	codeUpstream      = "upstream"
	codeInternal      = "internal"
)

// errorBody is the documented error envelope.
type errorBody struct {
	Error errorDetail `json:"error"`
}

// errorDetail carries the code and message of an error response.
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// apiError is an error carrying its HTTP mapping.
type apiError struct {
	status  int
	code    string
	message string
}

// Error implements error.
func (e *apiError) Error() string { return e.message }

func badRequest(format string, args ...any) *apiError {
	return &apiError{status: http.StatusBadRequest, code: codeBadRequest, message: fmt.Sprintf(format, args...)}
}

// writeJSON encodes v as the response with the given status. Encoding
// happens before any byte is written so a failure yields a clean 500.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Default().Error("httpapi: encode response", "err", err)
		b = []byte(`{"error":{"code":"internal","message":"failed to encode response"}}`)
		status = http.StatusInternalServerError
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeError writes the error envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeAPIError maps err to a response. Known sentinel errors get their
// documented code; anything else is an internal error whose details are
// logged but not returned.
func (s *Server) writeAPIError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.code, ae.message)
	case errors.Is(err, store.ErrNotFound), errors.Is(err, source.ErrNotFound), errors.Is(err, alerts.ErrUnknownRule):
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, source.ErrNotConfigured):
		writeError(w, http.StatusNotImplemented, codeNotConfigured, "the control API is not configured on this hub")
	case errors.Is(err, alerts.ErrInvalidRule), errors.Is(err, alerts.ErrNotOpen):
		writeError(w, http.StatusBadRequest, codeBadRequest, trimErrorPrefix(err.Error()))
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusBadGateway, codeUpstream, "upstream request timed out")
	case errors.Is(err, context.Canceled):
		// The client went away; nothing useful can be written.
		writeError(w, http.StatusInternalServerError, codeInternal, "request cancelled")
	default:
		s.log.Error("httpapi: internal error", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
	}
}

// writeUpstreamError maps a control API / LocalAPI failure to a response.
// Only a control API error message (*tsapi.APIError, which tsapi scrubs) is
// returned to the caller; every other failure is logged in full and
// answered with a fixed message, because LocalAPI errors carry operator
// details such as the socket path and remediation hints.
func (s *Server) writeUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *tsapi.APIError
	switch {
	case errors.Is(err, source.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "device not found upstream")
	case errors.Is(err, source.ErrNotConfigured):
		writeError(w, http.StatusNotImplemented, codeNotConfigured, "the control API is not configured on this hub")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusBadGateway, codeUpstream, "upstream request timed out")
	case errors.Is(err, tslocal.ErrDaemonUnavailable), errors.Is(err, tslocal.ErrPermissionDenied):
		s.log.Warn("httpapi: tailscaled unavailable", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadGateway, codeUpstream, "tailscaled is unavailable on the hub")
	case errors.As(err, &ae):
		s.log.Warn("httpapi: control API error", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadGateway, codeUpstream, truncateText(trimErrorPrefix(ae.Error()), 300))
	default:
		s.log.Warn("httpapi: upstream error", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadGateway, codeUpstream, "upstream request failed")
	}
}

// trimErrorPrefix drops "pkg: " style prefixes for user-facing messages.
func trimErrorPrefix(msg string) string {
	for _, p := range []string{"alerts: invalid rule: ", "alerts: ", "store: ", "collector: ", "tsapi: "} {
		msg = strings.TrimPrefix(msg, p)
	}
	return msg
}

// truncateText shortens s to at most n bytes on a rune boundary.
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// decodeJSON reads a JSON body into dst. It enforces a JSON content type
// when one is given, rejects trailing data and maps oversize bodies to 413.
// allowEmpty makes an empty body succeed without touching dst.
func decodeJSON(r *http.Request, dst any, allowEmpty bool) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || mt != "application/json" {
			return badRequest("Content-Type must be application/json")
		}
	}
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return badRequest("request body required")
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		var syn *json.SyntaxError
		var ute *json.UnmarshalTypeError
		switch {
		case errors.As(err, &mbe):
			return &apiError{status: http.StatusRequestEntityTooLarge, code: codeBadRequest, message: fmt.Sprintf("request body exceeds %d bytes", maxBodyBytes)}
		case errors.Is(err, io.EOF):
			if allowEmpty {
				return nil
			}
			return badRequest("request body required")
		case errors.As(err, &syn):
			return badRequest("invalid JSON at offset %d", syn.Offset)
		case errors.As(err, &ute):
			return badRequest("invalid value for %q", ute.Field)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return badRequest("invalid JSON: unexpected end of body")
		default:
			return badRequest("invalid JSON body")
		}
	}
	if dec.More() {
		return badRequest("unexpected data after the JSON body")
	}
	return nil
}
