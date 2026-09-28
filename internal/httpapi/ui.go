package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	indexFile = "index.html"
	// maxStaticFile bounds a static file read into memory for hashing.
	maxStaticFile  = 32 << 20
	cacheImmutable = "public, max-age=31536000, immutable"
	cacheNoCache   = "no-cache"
)

// contentTypes maps the extensions a Vite build emits to media types; the
// mime package is the fallback.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".map":         "application/json; charset=utf-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".ico":         "image/x-icon",
	".webp":        "image/webp",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".txt":         "text/plain; charset=utf-8",
	".webmanifest": "application/manifest+json",
}

// uiHandler serves the embedded single-page UI: hashed assets under
// /assets/ with an immutable cache policy, other files with no-cache, and
// index.html for every unknown path (SPA fallback). Without a build it
// serves a static "UI not built" page.
type uiHandler struct {
	fsys  fs.FS
	built bool
}

// newUIHandler inspects fsys for index.html.
func newUIHandler(fsys fs.FS) *uiHandler {
	h := &uiHandler{fsys: fsys}
	if fsys != nil {
		if fi, err := fs.Stat(fsys, indexFile); err == nil && !fi.IsDir() {
			h.built = true
		}
	}
	return h
}

// ServeHTTP implements http.Handler.
func (h *uiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.Contains(r.URL.Path, "..") || strings.ContainsRune(r.URL.Path, 0) {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	if !h.built {
		h.serveNotBuilt(w, r)
		return
	}
	clean := path.Clean("/" + r.URL.Path)
	name := strings.TrimPrefix(clean, "/")
	switch {
	case strings.HasPrefix(clean, "/assets/"):
		if !h.serveFile(w, r, name, cacheImmutable, false) {
			http.Error(w, "not found", http.StatusNotFound)
		}
	case name != "" && name != indexFile && h.serveFile(w, r, name, cacheNoCache, true):
	default:
		if !h.serveFile(w, r, indexFile, cacheNoCache, true) {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

// serveFile writes the named file when it exists and is a regular file.
// withETag hashes the content so browsers can revalidate no-cache files.
func (h *uiHandler) serveFile(w http.ResponseWriter, r *http.Request, name, cacheControl string, withETag bool) bool {
	if !fs.ValidPath(name) {
		return false
	}
	f, err := h.fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		return false
	}
	ct := contentTypes[strings.ToLower(path.Ext(name))]
	if ct == "" {
		ct = mime.TypeByExtension(path.Ext(name))
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	hdr := w.Header()
	hdr.Set("Content-Type", ct)
	hdr.Set("Cache-Control", cacheControl)

	if !withETag {
		if rs, ok := f.(io.ReadSeeker); ok {
			http.ServeContent(w, r, "", modTime(fi), rs)
			return true
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxStaticFile+1))
	if err != nil || len(data) > maxStaticFile {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return true
	}
	if withETag {
		sum := sha256.Sum256(data)
		hdr.Set("ETag", `"`+hex.EncodeToString(sum[:8])+`"`)
	}
	http.ServeContent(w, r, "", modTime(fi), bytes.NewReader(data))
	return true
}

// modTime returns the file's modification time, or zero for embedded files
// (which report the zero time) so no misleading Last-Modified is sent.
func modTime(fi fs.FileInfo) time.Time {
	if t := fi.ModTime(); t.Unix() > 0 {
		return t
	}
	return time.Time{}
}

// serveNotBuilt writes the placeholder page shown when web/dist is empty.
func (h *uiHandler) serveNotBuilt(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Cache-Control", cacheNoCache)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, notBuiltPage)
}

// notBuiltPage is self-contained (inline CSS only) so it satisfies the
// hub's Content Security Policy.
const notBuiltPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tailwatch</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #0f172a; color: #e2e8f0; font: 16px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
  main { max-width: 36rem; padding: 2rem; }
  h1 { font-size: 1.5rem; margin: 0 0 .5rem; }
  p { margin: .5rem 0; color: #cbd5e1; }
  code, pre { font: 14px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; background: #1e293b; border-radius: .375rem; }
  code { padding: .1rem .35rem; }
  pre { padding: .75rem 1rem; overflow-x: auto; }
  a { color: #7dd3fc; }
</style>
</head>
<body>
<main>
  <h1>Tailwatch is running, but the UI is not built</h1>
  <p>The hub binary was compiled without the web interface (<code>web/dist</code> is empty). The API is available under <code>/api/v1/</code>.</p>
  <p>Build the UI and rebuild the hub:</p>
  <pre>make web
make build</pre>
  <p>or, without make: <code>cd web &amp;&amp; npm ci &amp;&amp; npm run build</code>, then <code>go build ./cmd/tailwatch</code>.</p>
</main>
</body>
</html>
`
