package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHomePage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	(&Server{}).routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Files in.") || !strings.Contains(body, "curl -T") {
		t.Error("home page is missing expected content")
	}
}

func TestRobotsTxt(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	(&Server{}).routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, rule := range []string{"Allow: /$", "Disallow: /f/", "Disallow: /upload", "Disallow: /api/"} {
		if !strings.Contains(body, rule) {
			t.Errorf("robots.txt missing %q", rule)
		}
	}
}

func TestFavicon(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	rec := httptest.NewRecorder()
	(&Server{}).routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("content type = %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<svg") || !strings.Contains(body, "#a3e635") {
		t.Error("favicon is missing expected SVG content")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"video.mp4":                      "video.mp4",
		"../etc/passwd":                  "passwd",
		"..\\..\\win.ini":                "win.ini",
		"my recording (final).MP4":       "my-recording-final.mp4",
		"  spaces  .txt":                 "spaces.txt",
		".mp4":                           "file.mp4",
		"":                               "file",
		"---...---":                      "file",
		"ünïcödé 😀.mov":                  "n-c-d.mov",
		"a.tar.gz":                       "a.tar.gz",
		strings.Repeat("a", 100) + ".7z": strings.Repeat("a", 60) + ".7z",
		"name.with.many.dots.png":        "name.with.many.dots.png",
		"semi;colon&and=equals.txt":      "semi-colon-and-equals.txt",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"72h": 72 * time.Hour,
		"30m": 30 * time.Minute,
		"3d":  3 * 24 * time.Hour,
		"1d":  24 * time.Hour,
	}
	for in, want := range cases {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "xd", "abc", "10x"} {
		if _, err := parseDuration(bad); err == nil {
			t.Errorf("parseDuration(%q) expected error", bad)
		}
	}
}

func TestResolveTTL(t *testing.T) {
	def, max := 72*time.Hour, 7*24*time.Hour
	if got := resolveTTL("", def, max); got != def {
		t.Errorf("empty override: got %v want %v", got, def)
	}
	if got := resolveTTL("1h", def, max); got != time.Hour {
		t.Errorf("override: got %v want %v", got, time.Hour)
	}
	if got := resolveTTL("100d", def, max); got != max {
		t.Errorf("over max: got %v want %v", got, max)
	}
	if got := resolveTTL("bogus", def, max); got != def {
		t.Errorf("invalid override: got %v want %v", got, def)
	}
}

func TestJWTRoundTrip(t *testing.T) {
	secret := []byte("test-secret-test-secret")
	tok, _, err := mintJWT(secret, "agent-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := verifyJWT(secret, tok)
	if err != nil || sub != "agent-1" {
		t.Fatalf("roundtrip: sub=%q err=%v", sub, err)
	}

	if _, err := verifyJWT([]byte("wrong-secret-wrong-secret"), tok); err != errTokenSignature {
		t.Errorf("wrong secret: err=%v, want errTokenSignature", err)
	}

	tampered := tok[:len(tok)-2] + "xx"
	if _, err := verifyJWT(secret, tampered); err == nil {
		t.Error("tampered token verified")
	}

	expired, _, err := mintJWT(secret, "old", -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyJWT(secret, expired); err != errTokenExpired {
		t.Errorf("expired: err=%v, want errTokenExpired", err)
	}

	if _, err := verifyJWT(secret, "not.a.jwt-at-all"); err == nil {
		t.Error("garbage token verified")
	}
}

func TestContentTypeFor(t *testing.T) {
	if ct := contentTypeFor("x.mp4", nil); ct != "video/mp4" {
		t.Errorf("mp4: %q", ct)
	}
	if ct := contentTypeFor("x.html", nil); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("html: %q", ct)
	}
	if ct := contentTypeFor("x.unknownext", []byte("<html><body>hi</body></html>")); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("sniff html: %q", ct)
	}
	if ct := contentTypeFor("noext", nil); ct != "application/octet-stream" {
		t.Errorf("no ext no head: %q", ct)
	}
}

func TestValidID(t *testing.T) {
	for _, ok := range []string{"a1b2c", "zzzzz", "00000"} {
		if !validID(ok) {
			t.Errorf("validID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "abcd", "abcdef", "A1b2c", "a1.2c", "../etc", "a/b/c"} {
		if validID(bad) {
			t.Errorf("validID(%q) = true", bad)
		}
	}
}
