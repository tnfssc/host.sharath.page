package main

import (
	"context"
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
	mux.HandleFunc("GET /favicon.svg", s.handleFavicon)
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
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, homePage)
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "User-agent: *\nAllow: /$\nDisallow: /f/\nDisallow: /upload\nDisallow: /api/\nDisallow: /healthz\n")
}

func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, faviconSVG)
}

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<rect width="64" height="64" rx="15" fill="#121419"/>
<path d="M18 16v32M46 16v32M18 32h28" fill="none" stroke="#a3e635" stroke-width="9" stroke-linecap="round"/>
</svg>`

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="description" content="A fast, private-by-default temporary file host for trusted tools and agents.">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <title>host — temporary file sharing</title>
  <style>
    :root{color-scheme:dark;--bg:#0b0c0f;--panel:#121419;--line:#252832;--text:#f4f4f5;--muted:#9ca3af;--accent:#a3e635;--code:#181b21}
    *{box-sizing:border-box}body{margin:0;min-height:100vh;background:radial-gradient(circle at 75% 10%,#182312 0,transparent 28rem),var(--bg);color:var(--text);font:16px/1.6 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    main{width:min(920px,calc(100% - 40px));margin:auto;padding:clamp(64px,12vh,128px) 0 56px}.eyebrow{display:flex;align-items:center;gap:10px;color:var(--accent);font:600 13px/1 ui-monospace,SFMono-Regular,Consolas,monospace;letter-spacing:.08em;text-transform:uppercase}.dot{width:8px;height:8px;border-radius:50%;background:var(--accent);box-shadow:0 0 18px var(--accent)}
    h1{max-width:760px;margin:28px 0 18px;font-size:clamp(44px,8vw,86px);line-height:.98;letter-spacing:-.06em}.lead{max-width:630px;margin:0;color:var(--muted);font-size:clamp(18px,2.3vw,22px)}
    .grid{display:grid;grid-template-columns:1.4fr 1fr;gap:16px;margin-top:56px}.card{padding:24px;border:1px solid var(--line);border-radius:16px;background:color-mix(in srgb,var(--panel) 88%,transparent)}h2{margin:0 0 14px;font-size:14px;letter-spacing:.06em;text-transform:uppercase;color:var(--muted)}pre{overflow:auto;margin:0;padding:18px;border-radius:10px;background:var(--code);color:#d4d4d8;font:14px/1.7 ui-monospace,SFMono-Regular,Consolas,monospace}code strong{color:var(--accent);font-weight:500}.facts{display:grid;gap:16px}.fact{padding-bottom:16px;border-bottom:1px solid var(--line)}.fact:last-child{padding:0;border:0}.fact b{display:block;font-size:15px}.fact span{color:var(--muted);font-size:14px}
    footer{display:flex;justify-content:space-between;gap:24px;margin-top:42px;color:#71717a;font-size:13px}footer a{color:inherit;text-underline-offset:3px}@media(max-width:700px){.grid{grid-template-columns:1fr}footer{display:block}footer span{display:block;margin-top:6px}}
  </style>
</head>
<body>
  <main>
    <div class="eyebrow"><span class="dot"></span>Online · invite only</div>
    <h1>Files in.<br>Links out.</h1>
    <p class="lead">Temporary storage for recordings, reports, logs, and other artifacts. Uploads are authenticated. Shared links expire automatically.</p>
    <section class="grid" aria-label="Usage">
      <div class="card">
        <h2>Upload with curl</h2>
        <pre><code>curl -T report.html \
  -H <strong>"Authorization: Bearer $HOST_TOKEN"</strong> \
  "https://host.sharath.page/upload/report.html?ttl=3d"</code></pre>
      </div>
      <div class="card facts">
        <div class="fact"><b>Streaming uploads</b><span>Files go directly to storage.</span></div>
        <div class="fact"><b>Expiring links</b><span>Choose a TTL up to seven days.</span></div>
        <div class="fact"><b>Native previews</b><span>Video seeking and inline reports.</span></div>
      </div>
    </section>
    <footer><span>host.sharath.page</span><span>Public by link · Do not upload secrets</span></footer>
  </main>
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
