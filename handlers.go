package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Server struct {
	cfg   Config
	store *Store
}

type ctxKey int

const ctxSub ctxKey = iota

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /favicon.png", s.handleFavicon)
	mux.HandleFunc("GET /logo.png", s.handleLogo)
	mux.HandleFunc("GET /tokens.css", s.handleTokensCSS)
	mux.HandleFunc("GET /home.css", s.handleHomeCSS)
	mux.HandleFunc("GET /home.js", s.handleHomeJS)
	mux.HandleFunc("GET /", s.handleRoot)
	mux.HandleFunc("POST /api/tokens", s.handleMintToken)
	mux.HandleFunc("PUT /upload", s.requireAuth(s.handleUploadRaw))
	mux.HandleFunc("PUT /upload/{filename}", s.requireAuth(s.handleUploadRaw))
	mux.HandleFunc("POST /upload", s.requireAuth(s.handleUploadMultipart))
	mux.HandleFunc("GET /f/{id}", s.handleServe)
	mux.HandleFunc("GET /f/{id}/{filename...}", s.handleServe)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; script-src 'self'; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, homePage)
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "User-agent: *\nAllow: /$\nDisallow: /f/\nDisallow: /upload\nDisallow: /api/\nDisallow: /healthz\n")
}

func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	serveBrandPNG(w, faviconPNG)
}

func (s *Server) handleLogo(w http.ResponseWriter, _ *http.Request) {
	serveBrandPNG(w, logoPNG)
}

func (s *Server) handleTokensCSS(w http.ResponseWriter, _ *http.Request) {
	serveStatic(w, "text/css; charset=utf-8", tokensCSS)
}

func (s *Server) handleHomeCSS(w http.ResponseWriter, _ *http.Request) {
	serveStatic(w, "text/css; charset=utf-8", homeCSS)
}

func (s *Server) handleHomeJS(w http.ResponseWriter, _ *http.Request) {
	serveStatic(w, "text/javascript; charset=utf-8", homeJS)
}

func serveStatic(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func serveBrandPNG(w http.ResponseWriter, image []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(image)
}

//go:embed public/favicon.png
var faviconPNG []byte

//go:embed public/logo.png
var logoPNG []byte

//go:embed tokens.css
var tokensCSS []byte

//go:embed public/home.css
var homeCSS []byte

//go:embed public/home.js
var homeJS []byte

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
  <meta name="description" content="A small self-hosted temporary file host for agent artifacts, recordings, logs, and reports.">
  <link rel="icon" href="/favicon.png" type="image/png">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500&amp;family=Plus+Jakarta+Sans:wght@400;500;600;700&amp;display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/home.css">
  <title>host — files in, links out</title>
</head>
<body>
  <header class="nav">
    <div class="nav__inner shell">
      <a class="brand" href="/" aria-label="host.sharath.page home">
        <img class="brand__logo" src="/logo.png" width="32" height="32" alt="">
        <span>host</span><span class="brand__suffix">/ sharath.page</span>
      </a>
      <button class="search-trigger" id="command-trigger" type="button" aria-haspopup="dialog" aria-controls="command-palette">
        <span class="search-trigger__icon" aria-hidden="true"></span>
        <span class="search-trigger__label">Poke around</span>
        <kbd>⌘ K</kbd>
      </button>
      <a class="github-link" href="https://github.com/tnfssc/host.sharath.page">GitHub ↗</a>
    </div>
  </header>

  <main>
    <section class="hero shell" aria-labelledby="hero-title">
      <div class="hero__copy">
        <p class="eyebrow">Tiny personal file relay</p>
        <h1 id="hero-title">Files in. Links out. Nice.</h1>
        <p class="hero__lede">A little self-hosted stopover for recordings, reports, logs, and anything else that should only visit for a while.</p>
        <div class="hero__actions">
          <a class="action" href="https://github.com/tnfssc/host.sharath.page">View source ↗</a>
          <a class="text-link" href="https://github.com/tnfssc/host.sharath.page#quick-start">Read the setup →</a>
        </div>
      </div>

      <figure class="command" aria-labelledby="upload-caption">
        <span class="upload-mark" aria-hidden="true"></span>
        <figcaption class="command__meta" id="upload-caption"><span>Upload</span><span class="command__status">a link pops out</span></figcaption>
        <pre><code><span class="command__prompt">$</span> <span class="command__verb">curl</span> <span class="command__flag">-fsS -T</span> recording.mp4 \
  <span class="command__flag">-H</span> "Authorization: Bearer $HOST_TOKEN" \
  "$HOST_URL/upload/recording.mp4?ttl=3d&amp;format=text"

<span class="command__result">→</span> <span class="command__url">https://host.sharath.page/f/a8K2q/recording.mp4</span></code></pre>
      </figure>
    </section>

    <section class="workflow" aria-labelledby="workflow-title">
      <div class="shell">
        <header class="workflow__head">
          <div>
            <p class="eyebrow">The whole workflow</p>
            <h2 id="workflow-title">Nothing between the file and its link.</h2>
          </div>
        </header>
        <ol class="steps">
          <li class="step">
            <span class="step__number">01</span>
            <div><h3>Stream it.</h3><p>The request body moves directly to disk instead of collecting in application memory.</p></div>
          </li>
          <li class="step">
            <span class="step__number">02</span>
            <div><h3>Open it.</h3><p>Range requests keep videos seekable, while browser-friendly files render inline.</p></div>
          </li>
          <li class="step">
            <span class="step__number">03</span>
            <div><h3>Let it expire.</h3><p>Every upload has a lifetime. The janitor removes the file and its metadata on schedule.</p></div>
          </li>
        </ol>
      </div>
    </section>

    <section class="specs shell" aria-labelledby="specs-title">
      <header class="section-head">
        <h2 id="specs-title">Known limits. No mystery layer.</h2>
        <p>The defaults are visible in the repository and configurable through environment variables.</p>
      </header>
      <dl class="spec-list">
        <div class="spec-row"><dt>Default lifetime</dt><dd><span data-count="72">72</span> hours</dd><p>Override per upload with a Go duration or day string.</p></div>
        <div class="spec-row"><dt>Maximum lifetime</dt><dd><span data-count="7">7</span> days</dd><p>Longer values clamp to the configured maximum.</p></div>
        <div class="spec-row"><dt>Default upload limit</dt><dd><span data-count="5">5</span> GiB</dd><p>Uploads stream to disk; reverse proxies may impose smaller limits.</p></div>
        <div class="spec-row"><dt>Public identifier</dt><dd><span data-count="5">5</span> characters</dd><p>Downloads are public to anyone holding the unguessable link.</p></div>
        <div class="spec-row"><dt>Runtime modules</dt><dd><span data-count="0">0</span></dd><p>The server uses the Go standard library and ships in a distroless image.</p></div>
      </dl>
    </section>

    <section class="repo shell" aria-labelledby="repo-title">
      <div>
        <h2 id="repo-title">Curious? Lift the lid.</h2>
        <p>Setup, endpoints, configuration, deployment notes, and the security model all live with the code. No mysterious machinery.</p>
      </div>
      <a class="text-link" href="https://github.com/tnfssc/host.sharath.page">Check GitHub ↗</a>
    </section>
  </main>

  <footer class="footer" aria-label="Footer">
    <div class="footer__track" aria-hidden="true">
      <span>FILES IN · LINKS OUT · GONE ON SCHEDULE ·</span>
      <span>FILES IN · LINKS OUT · GONE ON SCHEDULE ·</span>
      <span>FILES IN · LINKS OUT · GONE ON SCHEDULE ·</span>
      <span>FILES IN · LINKS OUT · GONE ON SCHEDULE ·</span>
    </div>
    <div class="footer__meta shell">
      <span>host.sharath.page · personal infrastructure · MIT</span>
      <a href="/healthz">Service status</a>
    </div>
  </footer>

  <dialog class="palette" id="command-palette" aria-labelledby="command-label">
    <div class="palette__header">
      <label>
        <span class="palette__label" id="command-label">Go to</span>
        <input class="palette__input" id="command-input" type="search" autocomplete="off" placeholder="README, security, status…">
      </label>
      <button class="palette__close" id="command-close" type="button" aria-label="Close command palette">Esc</button>
    </div>
    <div class="palette__results">
      <p class="palette__group">Project</p>
      <a class="palette__item is-active" href="https://github.com/tnfssc/host.sharath.page"><span>Repository</span><span>GitHub ↗</span></a>
      <a class="palette__item" href="https://github.com/tnfssc/host.sharath.page#quick-start"><span>Quick start</span><span>README ↗</span></a>
      <a class="palette__item" href="https://github.com/tnfssc/host.sharath.page#security-model"><span>Security model</span><span>README ↗</span></a>
      <a class="palette__item" href="https://github.com/tnfssc/host.sharath.page/blob/main/LICENSE"><span>MIT license</span><span>GitHub ↗</span></a>
      <p class="palette__group">Service</p>
      <a class="palette__item" href="/healthz"><span>Service status</span><span>Local</span></a>
      <p class="palette__empty" id="command-empty" hidden>No matching destination.</p>
    </div>
  </dialog>
  <script src="/home.js" defer></script>
</body>
</html>`

// requireAuth enforces a valid HS256 JWT bearer token and records its subject.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="host"`)
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		sub, err := verifyJWT(s.cfg.JWTSecret, token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxSub, sub)))
	}
}

func uploader(r *http.Request) string {
	if sub, ok := r.Context().Value(ctxSub).(string); ok {
		return sub
	}
	return ""
}

// handleMintToken mints JWTs. Protected by the shared admin token.
// POST /api/tokens  X-Admin-Token: <secret>  {"name": "laptop", "days": 365}
func (s *Server) handleMintToken(w http.ResponseWriter, r *http.Request) {
	if !constantTimeEqual(r.Header.Get("X-Admin-Token"), s.cfg.AdminToken) {
		writeErr(w, http.StatusUnauthorized, "bad admin token")
		return
	}
	var req struct {
		Name string `json:"name"`
		Days int    `json:"days"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		req.Name = "default"
	}
	if req.Days <= 0 {
		req.Days = 365
	}
	tok, exp, err := mintJWT(s.cfg.JWTSecret, req.Name, time.Duration(req.Days)*24*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not mint token")
		return
	}
	log.Printf("token minted for %q, expires %s", req.Name, exp.Format(time.RFC3339))
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      tok,
		"name":       req.Name,
		"expires_at": exp.UTC().Format(time.RFC3339),
	})
}

// handleUploadRaw accepts a raw request body (curl -T file) with the filename
// taken from the URL path.
func (s *Server) handleUploadRaw(w http.ResponseWriter, r *http.Request) {
	filename := sanitizeFilename(r.PathValue("filename"))
	s.saveUpload(w, r, filename, r.Body)
}

// handleUploadMultipart accepts multipart form uploads (curl -F file=@x).
func (s *Server) handleUploadMultipart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing form field \"file\"")
		return
	}
	defer file.Close()
	s.saveUpload(w, r, sanitizeFilename(header.Filename), file)
}

type uploadResponse struct {
	URL       string    `json:"url"`
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) saveUpload(w http.ResponseWriter, r *http.Request, filename string, body io.Reader) {
	id, dir, err := s.store.createDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not allocate storage")
		return
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	size, head, err := writeBlob(dir, body, s.cfg.MaxUploadBytes)
	if err != nil {
		cleanup()
		if errors.Is(err, errTooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		log.Printf("upload %s: %v", id, err)
		writeErr(w, http.StatusInternalServerError, "could not store file")
		return
	}

	ttl := resolveTTL(r.URL.Query().Get("ttl"), s.cfg.DefaultTTL, s.cfg.MaxTTL)
	now := time.Now()
	meta := &Meta{
		ID:          id,
		Filename:    filename,
		ContentType: contentTypeFor(filename, head),
		Size:        size,
		Uploader:    uploader(r),
		UploadedAt:  now,
		ExpiresAt:   now.Add(ttl),
	}
	if err := s.store.saveMeta(dir, meta); err != nil {
		cleanup()
		writeErr(w, http.StatusInternalServerError, "could not store metadata")
		return
	}

	fileURL := s.publicBase(r) + "/f/" + id + "/" + url.PathEscape(filename)
	log.Printf("uploaded %s (%s, %d bytes, ttl %s) by %q", id, filename, size, ttl, uploader(r))

	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, fileURL+"\n")
		return
	}
	writeJSON(w, http.StatusCreated, uploadResponse{
		URL:       fileURL,
		ID:        id,
		Filename:  filename,
		Size:      size,
		ExpiresAt: meta.ExpiresAt.UTC(),
	})
}

// handleServe serves stored files inline with Range support (video seeking).
func (s *Server) handleServe(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	meta, err := s.store.Load(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not read metadata")
		return
	}
	if meta.Expired(time.Now()) {
		s.store.Delete(id)
		writeErr(w, http.StatusGone, "file expired")
		return
	}

	f, err := os.Open(s.store.BlobPath(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Disposition", contentDispositionInline(meta.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	// ServeContent handles Range, HEAD, If-Modified-Since and Content-Length.
	http.ServeContent(w, r, meta.Filename, meta.UploadedAt, f)
}

// contentDispositionInline keeps the browser rendering the file instead of
// downloading it, while still suggesting a sensible filename for "Save as".
func contentDispositionInline(filename string) string {
	quoted := strings.NewReplacer("\\", "_", "\"", "_", "\r", "_", "\n", "_").Replace(filename)
	return `inline; filename="` + quoted + `"`
}

// publicBase builds absolute URLs from BASE_URL when configured, otherwise
// from the (possibly proxied) request.
func (s *Server) publicBase(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return strings.TrimRight(s.cfg.BaseURL, "/")
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

func (s *Server) janitor(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.store.Sweep(now)
		}
	}
}
