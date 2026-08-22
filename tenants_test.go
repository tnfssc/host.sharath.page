package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidTenantName(t *testing.T) {
	for _, ok := range []string{"a", "acme", "acme-corp", "a1-b2", strings.Repeat("x", tenantNameMax)} {
		if !validTenantName(ok) {
			t.Errorf("validTenantName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "-acme", "acme-", "Acme", "acme_corp", "../etc", "a/b", strings.Repeat("x", tenantNameMax+1)} {
		if validTenantName(bad) {
			t.Errorf("validTenantName(%q) = true", bad)
		}
	}
}

func TestTenantRegistryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), registryName)
	reg, err := LoadTenantRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create("acme", time.Hour, 24*time.Hour, 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create("acme", 0, 0, 0); err != errTenantExists {
		t.Errorf("duplicate create: err=%v, want errTenantExists", err)
	}
	if _, err := reg.Create("Bad Name!", 0, 0, 0); err != errTenantName {
		t.Errorf("bad name: err=%v, want errTenantName", err)
	}

	reloaded, err := LoadTenantRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("acme")
	if !ok {
		t.Fatal("tenant missing after reload")
	}
	if got.DefaultTTL != "1h0m0s" || got.MaxTTL != "24h0m0s" || got.MaxUploadBytes != 1024 {
		t.Errorf("limits not persisted: %+v", got)
	}
	if len(got.JWTSecret) < 32 || len(got.AdminToken) < 32 {
		t.Error("secrets not generated")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("registry perms = %o, want 600", perm)
	}

	if err := reloaded.Delete("acme"); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Delete("acme"); err != errTenantNotFound {
		t.Errorf("second delete: err=%v, want errTenantNotFound", err)
	}
}

func TestEffectiveLimits(t *testing.T) {
	cfg := Config{DefaultTTL: 72 * time.Hour, MaxTTL: 7 * 24 * time.Hour, MaxUploadBytes: 5 << 30}

	def, max, maxBytes := (*Tenant)(nil).effectiveLimits(cfg)
	if def != cfg.DefaultTTL || max != cfg.MaxTTL || maxBytes != cfg.MaxUploadBytes {
		t.Error("nil tenant should inherit global limits")
	}

	tenant := &Tenant{DefaultTTL: "1h", MaxTTL: "2h", MaxUploadBytes: 10}
	def, max, maxBytes = tenant.effectiveLimits(cfg)
	if def != time.Hour || max != 2*time.Hour || maxBytes != 10 {
		t.Errorf("tenant limits not applied: %v %v %v", def, max, maxBytes)
	}

	bad := &Tenant{DefaultTTL: "bogus", MaxTTL: "bogus"}
	def, max, _ = bad.effectiveLimits(cfg)
	if def != cfg.DefaultTTL || max != cfg.MaxTTL {
		t.Error("invalid tenant durations should fall back to global limits")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := LoadTenantRegistry(filepath.Join(dir, registryName))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		cfg: Config{
			AdminToken:     "root-admin-token-1234567890",
			JWTSecret:      []byte("legacy-jwt-secret-1234567890"),
			DefaultTTL:     72 * time.Hour,
			MaxTTL:         7 * 24 * time.Hour,
			MaxUploadBytes: 1 << 20,
		},
		store:   store,
		tenants: reg,
	}
}

func createTenant(t *testing.T, srv *Server, name string) *Tenant {
	t.Helper()
	tenant, err := srv.tenants.Create(name, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return tenant
}

func uploadRaw(t *testing.T, srv *Server, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

func TestTenantUploadAndServe(t *testing.T) {
	srv := newTestServer(t)
	tenant := createTenant(t, srv, "acme")
	tok, _, err := mintJWT([]byte(tenant.JWTSecret), "ci", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	rec := uploadRaw(t, srv, "/t/acme/upload/report.txt", tok, "hello tenant")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.URL, "/t/acme/f/") {
		t.Errorf("url %q missing tenant prefix", resp.URL)
	}

	req := httptest.NewRequest(http.MethodGet, resp.URL[strings.Index(resp.URL, "/t/"):], nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello tenant" {
		t.Errorf("serve: status=%d body=%q", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(srv.store.dir, tenantDirName, "acme", resp.ID, blobName)); err != nil {
		t.Errorf("blob not stored under tenant dir: %v", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	srv := newTestServer(t)
	a := createTenant(t, srv, "acme")
	b := createTenant(t, srv, "globex")
	tokA, _, _ := mintJWT([]byte(a.JWTSecret), "ci", time.Hour)
	tokB, _, _ := mintJWT([]byte(b.JWTSecret), "ci", time.Hour)

	rec := uploadRaw(t, srv, "/t/acme/upload/a.txt", tokA, "a-file")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d", rec.Code)
	}
	var resp uploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	aPath := resp.URL[strings.Index(resp.URL, "/t/"):]

	// A globex token must not work on acme routes.
	if rec := uploadRaw(t, srv, "/t/acme/upload/evil.txt", tokB, "x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("cross-tenant upload: status = %d, want 401", rec.Code)
	}
	// Legacy token must not work on tenant routes.
	legacyTok, _, _ := mintJWT(srv.cfg.JWTSecret, "old", time.Hour)
	if rec := uploadRaw(t, srv, "/t/acme/upload/evil.txt", legacyTok, "x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("legacy token on tenant route: status = %d, want 401", rec.Code)
	}
	// Tenant token must not work on legacy routes.
	if rec := uploadRaw(t, srv, "/upload/evil.txt", tokA, "x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("tenant token on legacy route: status = %d, want 401", rec.Code)
	}
	// acme's file must not be visible under globex's namespace.
	globexPath := strings.Replace(aPath, "/t/acme/", "/t/globex/", 1)
	req := httptest.NewRequest(http.MethodGet, globexPath, nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-tenant serve: status = %d, want 404", rec.Code)
	}
	// Unknown tenant 404s everywhere.
	for _, path := range []string{"/t/nope/f/a1b2c/x.txt"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
	if rec := uploadRaw(t, srv, "/t/nope/upload/x.txt", tokA, "x"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown tenant upload: status = %d, want 404", rec.Code)
	}
}

func TestTenantAdminAPI(t *testing.T) {
	srv := newTestServer(t)
	mux := srv.routes()

	// Unauthorized without root admin token.
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", strings.NewReader(`{"name":"acme"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no admin token: status = %d, want 401", rec.Code)
	}

	// Create.
	req = httptest.NewRequest(http.MethodPost, "/api/tenants", strings.NewReader(`{"name":"acme","max_ttl":"1d"}`))
	req.Header.Set("X-Admin-Token", srv.cfg.AdminToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body %s", rec.Code, rec.Body)
	}
	var created struct {
		Tenant     publicTenant `json:"tenant"`
		JWTSecret  string       `json:"jwt_secret"`
		AdminToken string       `json:"admin_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.JWTSecret == "" || created.AdminToken == "" {
		t.Error("create response must include secrets")
	}

	// Duplicate create conflicts.
	req = httptest.NewRequest(http.MethodPost, "/api/tenants", strings.NewReader(`{"name":"acme"}`))
	req.Header.Set("X-Admin-Token", srv.cfg.AdminToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate: status = %d, want 409", rec.Code)
	}

	// List must not leak secrets.
	req = httptest.NewRequest(http.MethodGet, "/api/tenants", nil)
	req.Header.Set("X-Admin-Token", srv.cfg.AdminToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), created.JWTSecret) || strings.Contains(rec.Body.String(), "jwt_secret") {
		t.Errorf("list response leaks secrets: %s", rec.Body)
	}

	// Tenant admin token can mint its own tokens.
	req = httptest.NewRequest(http.MethodPost, "/t/acme/api/tokens", strings.NewReader(`{"name":"ci","days":1}`))
	req.Header.Set("X-Admin-Token", created.AdminToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tenant mint: status = %d, body %s", rec.Code, rec.Body)
	}
	// ...but not another tenant's.
	createTenant(t, srv, "globex")
	req = httptest.NewRequest(http.MethodPost, "/t/globex/api/tokens", strings.NewReader(`{"name":"ci"}`))
	req.Header.Set("X-Admin-Token", created.AdminToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("cross-tenant mint: status = %d, want 401", rec.Code)
	}

	// Delete removes the tenant and its files.
	tok, _, _ := mintJWT([]byte(created.JWTSecret), "ci", time.Hour)
	if rec := uploadRaw(t, srv, "/t/acme/upload/x.txt", tok, "data"); rec.Code != http.StatusCreated {
		t.Fatalf("upload before delete: status = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/tenants/acme", nil)
	req.Header.Set("X-Admin-Token", srv.cfg.AdminToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(srv.store.dir, tenantDirName, "acme")); !os.IsNotExist(err) {
		t.Error("tenant files not removed")
	}
	if rec := uploadRaw(t, srv, "/t/acme/upload/x.txt", tok, "data"); rec.Code != http.StatusNotFound {
		t.Errorf("upload after delete: status = %d, want 404", rec.Code)
	}
}

func TestTenantUploadLimits(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.tenants.Create("tiny", 0, time.Hour, 8); err != nil {
		t.Fatal(err)
	}
	tenant, _ := srv.tenants.Get("tiny")
	tok, _, _ := mintJWT([]byte(tenant.JWTSecret), "ci", time.Hour)

	// Over the tenant's 8-byte limit, under the global 1 MiB limit.
	if rec := uploadRaw(t, srv, "/t/tiny/upload/big.txt", tok, strings.Repeat("x", 100)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize upload: status = %d, want 413", rec.Code)
	}

	// TTL clamps to the tenant's max, not the global max.
	rec := uploadRaw(t, srv, "/t/tiny/upload/ok.txt?ttl=30d", tok, "hi")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: status = %d", rec.Code)
	}
	var resp uploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if ttl := time.Until(resp.ExpiresAt); ttl > time.Hour+time.Minute {
		t.Errorf("ttl not clamped to tenant max: %v", ttl)
	}
}

func TestLegacyTenantDisabledWithoutSecret(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.JWTSecret = nil
	rec := uploadRaw(t, srv, "/upload/x.txt", "whatever", "x")
	if rec.Code != http.StatusNotFound {
		t.Errorf("legacy upload without JWT_SECRET: status = %d, want 404", rec.Code)
	}
}
