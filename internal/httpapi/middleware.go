package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

const (
	// maxBodyBytes bounds every /api/ request body.
	maxBodyBytes = 64 << 10
	// handlerTimeout bounds every non-streaming API handler.
	handlerTimeout = 30 * time.Second
	// requestHeader and requestHeaderValue are the anti-CSRF header every
	// non-GET API request must carry.
	requestHeader      = "X-Requested-With"
	requestHeaderValue = "tailwatch"

	contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; " +
		"object-src 'none'; base-uri 'self'; form-action 'self'"
	strictTransportSecurity = "max-age=63072000; includeSubDomains"
)

// responseWriter records the status and byte count for logging and panic
// recovery while preserving http.Flusher and http.ResponseController access
// to the underlying writer (needed by the SSE handler).
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

// WriteHeader implements http.ResponseWriter.
func (w *responseWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter.
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush implements http.Flusher.
func (w *responseWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// reqInfo is per-request logging state shared between middlewares.
type reqInfo struct {
	login string
}

type reqInfoKey struct{}

func reqInfoFrom(ctx context.Context) *reqInfo {
	ri, _ := ctx.Value(reqInfoKey{}).(*reqInfo)
	return ri
}

// isAPIPath reports whether p is under the API prefix.
func isAPIPath(p string) bool { return strings.HasPrefix(p, "/api/") }

// recoverer converts handler panics into a 500 response (when nothing has
// been written yet) and logs them with a stack trace. http.ErrAbortHandler
// is re-raised so net/http can abort the connection quietly.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw, ok := w.(*responseWriter)
		if !ok {
			rw = &responseWriter{ResponseWriter: w}
			w = rw
		}
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(p)
			}
			s.log.Error("httpapi: handler panic", "method", r.Method, "path", r.URL.Path, "panic", p, "stack", string(debug.Stack()))
			if rw.wrote {
				return
			}
			if isAPIPath(r.URL.Path) {
				writeError(rw, http.StatusInternalServerError, codeInternal, "internal error")
				return
			}
			http.Error(rw, "internal error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// logging records one line per request: method, path (never the query
// string), status, duration, bytes, remote IP and the resolved login.
func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{ResponseWriter: w}
		ri := &reqInfo{}
		start := s.now()
		r = r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, ri))
		defer func() {
			// A panic is logged as a 500 here and re-raised so the outer
			// recoverer can write the response and the stack trace.
			p := recover()
			if p != nil {
				rw.status = http.StatusInternalServerError
			} else if !rw.wrote {
				rw.status = http.StatusOK
			}
			ip, _ := remoteIP(r.RemoteAddr)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"durMs", s.now().Sub(start).Milliseconds(),
				"bytes", rw.bytes,
				"remote", ip.String(),
			}
			if ri.login != "" {
				attrs = append(attrs, "login", ri.login)
			}
			level := slog.LevelDebug
			switch {
			case rw.status >= 500:
				level = slog.LevelWarn
			case rw.status >= 400:
				level = slog.LevelInfo
			case r.Method != http.MethodGet && r.Method != http.MethodHead && isAPIPath(r.URL.Path):
				level = slog.LevelInfo
			}
			s.log.Log(r.Context(), level, "httpapi: request", attrs...)
			if p != nil {
				panic(p)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// securityHeaders sets the response headers documented in docs/API.md on
// every response, plus HSTS when TLS is configured and Cache-Control:
// no-store under /api/.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Del("Server")
		if s.tls {
			h.Set("Strict-Transport-Security", strictTransportSecurity)
		}
		if isAPIPath(r.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// bodyLimit caps request bodies at maxBodyBytes.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// browserHardening rejects cross-site requests: Sec-Fetch-Site: cross-site,
// an Origin whose host differs from the request Host, and any non-GET/HEAD
// request without "X-Requested-With: tailwatch". CORS preflights are never
// answered, so browsers cannot make credentialed cross-origin calls.
func browserHardening(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeError(w, http.StatusForbidden, codeForbidden, "cross-site requests are not allowed")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r) {
			writeError(w, http.StatusForbidden, codeForbidden, "origin does not match the hub")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			!strings.EqualFold(r.Header.Get(requestHeader), requestHeaderValue) {
			writeError(w, http.StatusForbidden, codeForbidden, "missing "+requestHeader+": "+requestHeaderValue+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originMatchesHost reports whether the Origin header names the same host
// (and port) as the request. Default ports are normalized so
// "https://hub" matches Host "hub:443".
func originMatchesHost(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	reqScheme := "http"
	if r.TLS != nil {
		reqScheme = "https"
	}
	return normalizeHost(u.Host, u.Scheme) == normalizeHost(r.Host, reqScheme)
}

// normalizeHost lower-cases host and strips the scheme's default port.
func normalizeHost(host, scheme string) string {
	host = strings.ToLower(host)
	def := ":80"
	if scheme == "https" {
		def = ":443"
	}
	return strings.TrimSuffix(host, def)
}

// authenticate resolves the caller's identity from the remote address and
// stores it in the request context. Failures yield 401, a known identity
// without a role 403.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.auth.Authenticate(r.Context(), r.RemoteAddr)
		if err != nil || id == nil {
			switch {
			case errors.Is(err, ErrForbidden):
				s.log.Info("httpapi: identity has no role", "remote", r.RemoteAddr, "err", err)
				writeError(w, http.StatusForbidden, codeForbidden, "your tailnet identity has no access to this hub")
			default:
				if r.Context().Err() == nil {
					s.log.Info("httpapi: authentication failed", "remote", r.RemoteAddr, "err", err)
				}
				writeError(w, http.StatusUnauthorized, codeUnauthorized, "request could not be attributed to a tailnet identity")
			}
			return
		}
		if ri := reqInfoFrom(r.Context()); ri != nil {
			ri.login = id.Login
		}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), id)))
	})
}

// rateLimit enforces the per-identity token bucket.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "request could not be attributed to a tailnet identity")
			return
		}
		key := id.Login + "|" + string(id.NodeID) + "|" + id.NodeIP
		if !s.limiter.allow(key) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, codeRateLimited, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin rejects non-admin identities with 403.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok || id.Role != model.RoleAdmin {
			writeError(w, http.StatusForbidden, codeForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// timeout bounds a handler with http.TimeoutHandler. The timeout body is
// the JSON error envelope so clients can parse it.
func (s *Server) timeout(next http.Handler) http.Handler {
	return http.TimeoutHandler(next, s.handlerTimeout, timeoutBody)
}

// timeoutBody is the body written by the timeout handler.
const timeoutBody = `{"error":{"code":"internal","message":"request timed out"}}`
