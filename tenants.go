package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	tenantNameMax  = 32
	registryName   = "tenants.json"
	tenantDirName  = "t"
	legacyTenant   = "" // pre-multitenancy layout: files at the data-dir root
	secretBytes    = 32
	maxRegistryLen = 1 << 20
)

// Tenant is one isolated namespace with its own signing secret, admin token,
// and upload limits. Zero-valued limits inherit the global Config defaults.
type Tenant struct {
	Name           string    `json:"name"`
	JWTSecret      string    `json:"jwt_secret"`
	AdminToken     string    `json:"admin_token"`
	DefaultTTL     string    `json:"default_ttl,omitempty"`
	MaxTTL         string    `json:"max_ttl,omitempty"`
	MaxUploadBytes int64     `json:"max_upload_bytes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

var (
	errTenantExists   = errors.New("tenant already exists")
	errTenantNotFound = errors.New("tenant not found")
	errTenantName     = errors.New("invalid tenant name: use 1-32 chars of [a-z0-9-], no leading/trailing dash")
)

// validTenantName allows lowercase alphanumerics and inner dashes, so names
// are safe as a single URL path segment and a directory name.
func validTenantName(name string) bool {
	if name == "" || len(name) > tenantNameMax {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func randomSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// TenantRegistry is the on-disk set of tenants, persisted as JSON with an
// atomic rename. It is the source of truth for per-tenant secrets and limits.
type TenantRegistry struct {
	path    string
	mu      sync.RWMutex
	tenants map[string]*Tenant
}

func LoadTenantRegistry(path string) (*TenantRegistry, error) {
	r := &TenantRegistry{path: path, tenants: map[string]*Tenant{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > maxRegistryLen {
		return nil, errors.New("tenant registry too large")
	}
	var list []*Tenant
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("tenant registry: %w", err)
	}
	for _, t := range list {
		if !validTenantName(t.Name) {
			return nil, fmt.Errorf("tenant registry: invalid tenant name %q", t.Name)
		}
		r.tenants[t.Name] = t
	}
	return r, nil
}

func (r *TenantRegistry) saveLocked() error {
	list := make([]*Tenant, 0, len(r.tenants))
	for _, t := range r.tenants {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.path), ".tenants-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), r.path)
}

// Get returns a copy of the named tenant, safe to use after the lock is released.
func (r *TenantRegistry) Get(name string) (*Tenant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tenants[name]
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

// Create registers a new tenant with freshly generated secrets. Optional
// limits may be nil/zero to inherit the global defaults.
func (r *TenantRegistry) Create(name string, defaultTTL, maxTTL time.Duration, maxUploadBytes int64) (*Tenant, error) {
	if !validTenantName(name) {
		return nil, errTenantName
	}
	if maxTTL > 0 && defaultTTL > maxTTL {
		defaultTTL = maxTTL
	}
	jwtSecret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	adminToken, err := randomSecret()
	if err != nil {
		return nil, err
	}
	t := &Tenant{
		Name:           name,
		JWTSecret:      jwtSecret,
		AdminToken:     adminToken,
		MaxUploadBytes: maxUploadBytes,
		CreatedAt:      time.Now().UTC(),
	}
	if defaultTTL > 0 {
		t.DefaultTTL = defaultTTL.String()
	}
	if maxTTL > 0 {
		t.MaxTTL = maxTTL.String()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tenants[name]; exists {
		return nil, errTenantExists
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return nil, err
	}
	r.tenants[name] = t
	if err := r.saveLocked(); err != nil {
		delete(r.tenants, name)
		return nil, err
	}
	cp := *t
	return &cp, nil
}

// Delete removes a tenant from the registry; callers remove its files.
func (r *TenantRegistry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tenants[name]; !ok {
		return errTenantNotFound
	}
	delete(r.tenants, name)
	return r.saveLocked()
}

// List returns copies of all tenants, sorted by name.
func (r *TenantRegistry) List() []*Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Tenant, 0, len(r.tenants))
	for _, t := range r.tenants {
		cp := *t
		list = append(list, &cp)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// publicTenant is a Tenant with its secrets stripped, for list responses.
type publicTenant struct {
	Name           string    `json:"name"`
	DefaultTTL     string    `json:"default_ttl,omitempty"`
	MaxTTL         string    `json:"max_ttl,omitempty"`
	MaxUploadBytes int64     `json:"max_upload_bytes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

func (t *Tenant) public() publicTenant {
	return publicTenant{
		Name:           t.Name,
		DefaultTTL:     t.DefaultTTL,
		MaxTTL:         t.MaxTTL,
		MaxUploadBytes: t.MaxUploadBytes,
		CreatedAt:      t.CreatedAt,
	}
}

// effectiveLimits resolves the tenant's limits against the global defaults.
func (t *Tenant) effectiveLimits(cfg Config) (def, max time.Duration, maxBytes int64) {
	def, max, maxBytes = cfg.DefaultTTL, cfg.MaxTTL, cfg.MaxUploadBytes
	if t == nil {
		return def, max, maxBytes
	}
	if v, err := parseDuration(t.DefaultTTL); err == nil && v > 0 {
		def = v
	}
	if v, err := parseDuration(t.MaxTTL); err == nil && v > 0 {
		max = v
	}
	if t.MaxUploadBytes > 0 {
		maxBytes = t.MaxUploadBytes
	}
	if def > max {
		def = max
	}
	return def, max, maxBytes
}

// tenantPrefix returns the URL path prefix for a tenant ("" for legacy).
func tenantPrefix(name string) string {
	if name == legacyTenant {
		return ""
	}
	return "/t/" + name
}
