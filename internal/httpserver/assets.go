package httpserver

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves a compiled browser bundle.
//
// Two cache policies, and the split is what makes deploys safe (§8.2):
//
//   - Content-hashed assets are immutable. The filename changes when the bytes
//     change, so they can be cached for a year with no revalidation.
//   - The HTML entry point is never cached beyond a few seconds. It is the file
//     that names the current asset hashes, so a stale copy points at assets
//     that no longer exist.
//
// Unknown paths fall through to index.html because the router lives in the
// browser. Paths under /api and /ws never reach here — they are matched first.
type SPAHandler struct {
	fs       fs.FS
	index    []byte
	basePath string
	// devReload disables all caching so a rebuilt bundle is picked up without
	// a hard refresh.
	devReload bool
}

// NewSPAHandler reads index.html once at startup. A missing or unreadable
// index is a build error, and failing loudly here beats serving a blank page.
func NewSPAHandler(assets fs.FS, basePath string, dev bool) (*SPAHandler, error) {
	file, err := assets.Open("index.html")
	if err != nil {
		return nil, errors.New("assets: index.html is missing — run `make web` before building the binary")
	}
	defer file.Close()

	index, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return &SPAHandler{fs: assets, index: index, basePath: basePath, devReload: dev}, nil
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), h.basePath)
	name = strings.TrimPrefix(name, "/")

	if name == "" || name == "index.html" {
		h.serveIndex(w, r)
		return
	}

	file, err := h.fs.Open(name)
	if err != nil {
		// Not a file on disk, so it is a client-side route. Serve the shell and
		// let the browser router decide — including for genuinely unknown
		// paths, which the SPA renders as its own not-found screen.
		h.serveIndex(w, r)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		h.serveIndex(w, r)
		return
	}

	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		h.serveIndex(w, r)
		return
	}

	h.setAssetCacheHeaders(w, name)
	http.ServeContent(w, r, name, stat.ModTime(), seeker)
}

func (h *SPAHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if h.devReload {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		// Short, revalidated. The entry point must be allowed to change on
		// every deploy without anyone clearing a cache.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	_, _ = w.Write(h.index)
}

func (h *SPAHandler) setAssetCacheHeaders(w http.ResponseWriter, name string) {
	if h.devReload {
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	if isContentHashed(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}

	// Not hashed — the widget loader, favicons, the robots file. These are
	// stable URLs whose contents do change, so they get a short window.
	w.Header().Set("Cache-Control", "public, max-age=300")
}

// StaticHandler serves a directory of files with no HTML fallback.
//
// The widget uses this rather than SPAHandler because it is not an application
// with a shell — it is a loader script and a module the loader imports. A
// missing path there is a genuine 404, not a client-side route, and answering
// it with an HTML page would hand a JavaScript `import()` a document to parse.
type StaticHandler struct {
	fs        fs.FS
	basePath  string
	devReload bool
}

func NewStaticHandler(assets fs.FS, basePath string, dev bool) *StaticHandler {
	return &StaticHandler{fs: assets, basePath: basePath, devReload: dev}
}

func (h *StaticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), h.basePath)
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		http.NotFound(w, r)
		return
	}

	file, err := h.fs.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}

	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case h.devReload:
		w.Header().Set("Cache-Control", "no-store")
	case isContentHashed(name):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		// The loader and the app entry keep stable URLs so an install snippet
		// never has to change. Five minutes is short enough to roll out a fix
		// and long enough that a busy site is not re-fetching it constantly.
		w.Header().Set("Cache-Control", "public, max-age=300")
	}

	http.ServeContent(w, r, name, stat.ModTime(), seeker)
}

// isContentHashed recognises the `name-HASH.ext` pattern the bundler emits.
// Matching on shape rather than on a manifest keeps this handler independent
// of the bundler's output format beyond that one convention.
func isContentHashed(name string) bool {
	base := path.Base(name)
	ext := path.Ext(base)
	if ext == "" {
		return false
	}

	stem := strings.TrimSuffix(base, ext)
	dash := strings.LastIndex(stem, "-")
	if dash < 0 {
		return false
	}

	hash := stem[dash+1:]
	if len(hash) < 8 {
		return false
	}

	for _, char := range hash {
		isHashChar := (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-'
		if !isHashChar {
			return false
		}
	}
	return true
}
