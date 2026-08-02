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
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
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

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="description" content="A fast, private-by-default temporary file host for trusted tools and agents.">
  <link rel="icon" href="/favicon.png" type="image/png">
  <title>host — files in, links out</title>
  <style>
    :root{color-scheme:dark;--bg:#08090b;--panel:#101115;--panel-2:#15171c;--line:#292c34;--line-soft:#1c1e24;--text:#f7f7f8;--muted:#989ba5;--faint:#666a75;--accent:#c4f042;--violet:#9b87f5;--mono:ui-monospace,SFMono-Regular,Consolas,"Liberation Mono",monospace}
    *{box-sizing:border-box}
    html{background:var(--bg);scroll-behavior:smooth}
    body{margin:0;min-height:100vh;overflow-x:hidden;background:radial-gradient(circle at 78% 18%,rgba(155,135,245,.13),transparent 28rem),radial-gradient(circle at 15% 70%,rgba(196,240,66,.07),transparent 24rem),var(--bg);color:var(--text);font:16px/1.6 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    body:before{position:fixed;inset:0;z-index:-1;content:"";background-image:linear-gradient(rgba(255,255,255,.025) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.025) 1px,transparent 1px);background-size:56px 56px;mask-image:linear-gradient(to bottom,black,transparent 85%)}
    a{color:inherit}a:focus-visible{outline:2px solid var(--accent);outline-offset:4px}
    .shell{width:min(1120px,calc(100% - 40px));margin:auto}
    header{display:flex;align-items:center;justify-content:space-between;height:88px;border-bottom:1px solid var(--line-soft)}
    .brand{display:flex;align-items:center;gap:11px;font:650 14px/1 var(--mono);letter-spacing:-.02em;text-decoration:none}.logo{width:34px;height:34px}.brand span{color:var(--muted)}
    .status{display:flex;align-items:center;gap:9px;color:var(--muted);font:12px/1 var(--mono);text-decoration:none}.status-dot{width:7px;height:7px;border-radius:99px;background:var(--accent);box-shadow:0 0 16px rgba(196,240,66,.8)}
    main{padding:clamp(72px,10vw,130px) 0 56px}
    .hero{display:grid;grid-template-columns:minmax(0,1.08fr) minmax(360px,.92fr);align-items:center;gap:clamp(48px,8vw,108px)}
    .kicker{display:flex;align-items:center;gap:10px;margin:0 0 24px;color:var(--accent);font:600 12px/1 var(--mono);letter-spacing:.12em;text-transform:uppercase}.kicker:before{width:28px;height:1px;background:currentColor;content:""}
    h1{max-width:700px;margin:0;font-size:clamp(56px,8vw,96px);font-weight:610;line-height:.92;letter-spacing:-.072em}h1 em{color:var(--muted);font-style:normal}
    .lead{max-width:590px;margin:30px 0 0;color:var(--muted);font-size:clamp(17px,2vw,20px);line-height:1.65}.lead strong{color:var(--text);font-weight:520}
    .signals{display:flex;flex-wrap:wrap;gap:9px;margin-top:30px}.signal{padding:7px 10px;border:1px solid var(--line);border-radius:99px;color:#b8bbc4;background:rgba(16,17,21,.65);font:11px/1 var(--mono)}
    .terminal{position:relative;border:1px solid #30333d;border-radius:18px;background:rgba(16,17,21,.94);box-shadow:0 30px 100px rgba(0,0,0,.45),0 0 0 1px rgba(255,255,255,.02) inset;transform:rotate(1deg)}
    .terminal:before{position:absolute;inset:-1px;z-index:-1;border-radius:18px;background:linear-gradient(135deg,rgba(196,240,66,.35),transparent 32%,transparent 70%,rgba(155,135,245,.35));content:"";filter:blur(16px);opacity:.35}
    .terminal-bar{display:flex;align-items:center;justify-content:space-between;padding:15px 17px;border-bottom:1px solid var(--line)}.traffic{display:flex;gap:6px}.traffic i{display:block;width:7px;height:7px;border-radius:50%;background:#3a3d46}.traffic i:first-child{background:var(--accent)}.terminal-label{color:var(--faint);font:10px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase}
    pre{min-height:236px;margin:0;overflow:auto;padding:26px 24px;color:#d8dae0;font:13px/1.85 var(--mono);white-space:pre-wrap;word-break:break-word}.prompt{color:var(--accent)}.flag{color:var(--violet)}.dim{color:#707480}.value{color:#f4f4f5}
    .result{display:flex;align-items:center;gap:11px;margin:0 15px 15px;padding:13px 14px;border:1px solid var(--line);border-radius:10px;background:#0b0c0f;color:#cfd1d7;font:11px/1.4 var(--mono);overflow:hidden}.result-arrow{color:var(--accent)}.result-url{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .features{display:grid;grid-template-columns:repeat(3,1fr);margin-top:clamp(84px,12vw,138px);border-top:1px solid var(--line);border-bottom:1px solid var(--line)}
    .feature{position:relative;min-height:220px;padding:32px}.feature+ .feature{border-left:1px solid var(--line)}.number{display:block;margin-bottom:54px;color:var(--faint);font:11px/1 var(--mono)}.feature h2{margin:0 0 9px;font-size:17px;font-weight:560;letter-spacing:-.02em}.feature p{max-width:260px;margin:0;color:var(--muted);font-size:14px;line-height:1.55}.feature:after{position:absolute;top:34px;right:32px;width:8px;height:8px;border:1px solid var(--faint);content:"";transform:rotate(45deg)}
    footer{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:32px 0 44px;color:var(--faint);font:11px/1.5 var(--mono)}.warning{color:#8d9099}
    @media(max-width:820px){.hero{grid-template-columns:1fr}.terminal{max-width:620px;transform:none}.features{grid-template-columns:1fr}.feature{min-height:auto}.feature+ .feature{border-top:1px solid var(--line);border-left:0}.number{margin-bottom:30px}}
    @media(max-width:520px){.shell{width:min(100% - 28px,1120px)}header{height:72px}.brand span{display:none}main{padding-top:58px}h1{font-size:clamp(50px,17vw,72px)}.signals{gap:7px}.terminal{border-radius:14px}pre{min-height:220px;padding:22px 18px;font-size:12px}.features{margin-top:78px}.feature{padding:28px 20px}.feature:after{right:22px}footer{align-items:flex-start;flex-direction:column}}
    @media(prefers-reduced-motion:no-preference){.terminal{animation:arrive .7s cubic-bezier(.2,.8,.2,1) both}@keyframes arrive{from{opacity:0;transform:translateY(18px) rotate(1deg)}to{opacity:1;transform:translateY(0) rotate(1deg)}}}
  </style>
</head>
<body>
  <div class="shell">
    <header>
      <a class="brand" href="/" aria-label="host.sharath.page home"><img class="logo" src="/logo.png" alt=""><b>host</b><span>/ sharath.page</span></a>
      <a class="status" href="/healthz"><span class="status-dot"></span>systems nominal</a>
    </header>
    <main>
      <section class="hero" aria-labelledby="hero-title">
        <div>
          <p class="kicker">Agent-native file relay</p>
          <h1 id="hero-title">Upload once.<br><em>Share anywhere.</em></h1>
          <p class="lead">A fast temporary home for <strong>recordings, reports, logs, and artifacts.</strong> Authenticated on the way in. Effortless on the way out.</p>
          <div class="signals" aria-label="Service properties">
            <span class="signal">JWT protected</span><span class="signal">streaming I/O</span><span class="signal">auto-expiring</span><span class="signal">range requests</span>
          </div>
        </div>
        <div class="terminal" aria-label="Upload example">
          <div class="terminal-bar"><span class="traffic"><i></i><i></i><i></i></span><span class="terminal-label">~/artifacts</span></div>
          <pre><code><span class="prompt">❯</span> curl <span class="flag">-T</span> report.html \
  <span class="flag">-H</span> <span class="value">"Authorization: Bearer $HOST_TOKEN"</span> \
  <span class="value">"https://host.sharath.page/upload/report.html?ttl=3d&amp;format=text"</span></code></pre>
          <div class="result"><span class="result-arrow">→</span><span class="result-url">https://host.sharath.page/f/a8K2q/report.html</span></div>
        </div>
      </section>
      <section class="features" aria-label="Features">
        <article class="feature"><span class="number">01 / STREAM</span><h2>No waiting room.</h2><p>Large files move directly to disk instead of collecting in application memory.</p></article>
        <article class="feature"><span class="number">02 / EXPIRE</span><h2>Gone on schedule.</h2><p>Every link gets a lifetime. The janitor removes it automatically when time is up.</p></article>
        <article class="feature"><span class="number">03 / PREVIEW</span><h2>Open, don't download.</h2><p>Videos seek, reports render, and browser-friendly files display right where they land.</p></article>
      </section>
    </main>
    <footer><span>host.sharath.page · private infrastructure</span><span class="warning">Public by link — never upload secrets</span></footer>
  </div>
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
