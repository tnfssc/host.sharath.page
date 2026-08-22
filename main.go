// Command hoard is a small self-hosted temporary file host.
//
// Uploads are authenticated with HS256 JWTs minted via the admin token.
// Files are served publicly under unguessable /f/<id>/<filename> links and
// deleted automatically once their TTL expires.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type Config struct {
	Addr           string
	DataDir        string
	BaseURL        string
	AdminToken     string
	JWTSecret      []byte
	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	MaxUploadBytes int64
	SweepInterval  time.Duration
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := parseDuration(v); err == nil && d > 0 {
			return d
		}
		log.Printf("warning: ignoring invalid %s=%q", key, v)
	}
	return def
}

func envInt(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
		log.Printf("warning: ignoring invalid %s=%q", key, v)
	}
	return def
}

func loadConfig() Config {
	cfg := Config{
		Addr:           envStr("ADDR", ":8080"),
		DataDir:        envStr("DATA_DIR", "./data"),
		BaseURL:        envStr("BASE_URL", ""),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		JWTSecret:      []byte(os.Getenv("JWT_SECRET")),
		DefaultTTL:     envDur("DEFAULT_TTL", 72*time.Hour),
		MaxTTL:         envDur("MAX_TTL", 7*24*time.Hour),
		MaxUploadBytes: envInt("MAX_UPLOAD_BYTES", 5<<30),
		SweepInterval:  envDur("SWEEP_INTERVAL", time.Minute),
	}
	if len(cfg.AdminToken) < 16 {
		log.Fatal("ADMIN_TOKEN must be set (>= 16 chars)")
	}
	if len(cfg.JWTSecret) > 0 && len(cfg.JWTSecret) < 16 {
		log.Fatal("JWT_SECRET must be >= 16 chars when set")
	}
	if cfg.DefaultTTL > cfg.MaxTTL {
		cfg.DefaultTTL = cfg.MaxTTL
	}
	return cfg
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mint":
			mintCmd(os.Args[2:])
			return
		case "tenant":
			tenantCmd(os.Args[2:])
			return
		case "healthcheck":
			healthcheckCmd()
			return
		}
	}

	cfg := loadConfig()
	store, err := NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	tenants, err := LoadTenantRegistry(registryPath(cfg.DataDir))
	if err != nil {
		log.Fatalf("tenants: %v", err)
	}

	srv := &Server{cfg: cfg, store: store, tenants: tenants}
	srv.store.Sweep(time.Now()) // clear leftovers from a previous run

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go srv.janitor(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No ReadTimeout/WriteTimeout: uploads and downloads can be long-lived.
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("hoard: listening on %s, data in %s, default TTL %s, max upload %d bytes",
		cfg.Addr, cfg.DataDir, cfg.DefaultTTL, cfg.MaxUploadBytes)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http: %v", err)
	}
}

// registryPath locates the tenant registry inside the data dir.
func registryPath(dataDir string) string { return filepath.Join(dataDir, registryName) }

// mintCmd prints a signed JWT:  hoard mint [--tenant acme] --name laptop --days 365
// Without --tenant it signs with JWT_SECRET (the legacy tenant); with
// --tenant it uses that tenant's secret from the registry in DATA_DIR.
func mintCmd(args []string) {
	fs := flag.NewFlagSet("mint", flag.ExitOnError)
	name := fs.String("name", "default", "subject/name for the token")
	days := fs.Int("days", 365, "validity in days")
	tenant := fs.String("tenant", "", "tenant to mint for (default: legacy JWT_SECRET tenant)")
	_ = fs.Parse(args)

	var secret []byte
	if *tenant == "" {
		secret = []byte(os.Getenv("JWT_SECRET"))
		if len(secret) < 16 {
			log.Fatal("JWT_SECRET must be set (>= 16 chars)")
		}
	} else {
		reg, err := LoadTenantRegistry(registryPath(envStr("DATA_DIR", "./data")))
		if err != nil {
			log.Fatalf("tenants: %v", err)
		}
		t, ok := reg.Get(*tenant)
		if !ok {
			log.Fatalf("unknown tenant %q", *tenant)
		}
		secret = []byte(t.JWTSecret)
	}
	tok, exp, err := mintJWT(secret, *name, time.Duration(*days)*24*time.Hour)
	if err != nil {
		log.Fatalf("mint: %v", err)
	}
	fmt.Println(tok)
	fmt.Fprintf(os.Stderr, "subject=%q tenant=%q expires=%s\n", *name, *tenant, exp.Format(time.RFC3339))
}

// tenantCmd manages tenants: hoard tenant <create|list|delete> ...
func tenantCmd(args []string) {
	if len(args) == 0 {
		log.Fatal("usage: hoard tenant <create|list|delete> ...")
	}
	dataDir := envStr("DATA_DIR", "./data")
	reg, err := LoadTenantRegistry(registryPath(dataDir))
	if err != nil {
		log.Fatalf("tenants: %v", err)
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("tenant create", flag.ExitOnError)
		defaultTTL := fs.String("default-ttl", "", "default file lifetime (e.g. 72h, 3d); inherits global default")
		maxTTL := fs.String("max-ttl", "", "maximum accepted TTL; inherits global default")
		maxUpload := fs.Int64("max-upload-bytes", 0, "upload size limit; inherits global default")
		_ = fs.Parse(args[1:])
		if fs.NArg() != 1 {
			log.Fatal("usage: hoard tenant create [--default-ttl 72h] [--max-ttl 7d] [--max-upload-bytes N] <name>")
		}
		var def, max time.Duration
		if *defaultTTL != "" {
			if def, err = parseDuration(*defaultTTL); err != nil || def <= 0 {
				log.Fatalf("invalid --default-ttl %q", *defaultTTL)
			}
		}
		if *maxTTL != "" {
			if max, err = parseDuration(*maxTTL); err != nil || max <= 0 {
				log.Fatalf("invalid --max-ttl %q", *maxTTL)
			}
		}
		t, err := reg.Create(fs.Arg(0), def, max, *maxUpload)
		if err != nil {
			log.Fatalf("create: %v", err)
		}
		out, _ := json.MarshalIndent(map[string]any{
			"tenant":      t.public(),
			"jwt_secret":  t.JWTSecret,
			"admin_token": t.AdminToken,
			"base_path":   tenantPrefix(t.Name),
		}, "", "  ")
		fmt.Println(string(out))
		fmt.Fprintln(os.Stderr, "store these secrets; they cannot be retrieved later")

	case "list":
		list := reg.List()
		out := make([]publicTenant, 0, len(list))
		for _, t := range list {
			out = append(out, t.public())
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))

	case "delete":
		if len(args) != 2 {
			log.Fatal("usage: hoard tenant delete <name>")
		}
		if err := reg.Delete(args[1]); err != nil {
			log.Fatalf("delete: %v", err)
		}
		store, err := NewStore(dataDir)
		if err != nil {
			log.Fatalf("store: %v", err)
		}
		store.DeleteTenant(args[1])
		fmt.Fprintf(os.Stderr, "tenant %q deleted (files removed)\n", args[1])

	default:
		log.Fatalf("unknown tenant subcommand %q", args[0])
	}
}

// healthcheckCmd lets the distroless image (no shell/curl) run compose healthchecks.
func healthcheckCmd() {
	addr := envStr("ADDR", ":8080")
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
