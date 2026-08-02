// Command host is a small self-hosted temporary file host.
//
// Uploads are authenticated with HS256 JWTs minted via the admin token.
// Files are served publicly under unguessable /f/<id>/<filename> links and
// deleted automatically once their TTL expires.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	if len(cfg.JWTSecret) < 16 {
		log.Fatal("JWT_SECRET must be set (>= 16 chars)")
	}
	if len(cfg.AdminToken) < 16 {
		log.Fatal("ADMIN_TOKEN must be set (>= 16 chars)")
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

	srv := &Server{cfg: cfg, store: store}
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

	log.Printf("host: listening on %s, data in %s, default TTL %s, max upload %d bytes",
		cfg.Addr, cfg.DataDir, cfg.DefaultTTL, cfg.MaxUploadBytes)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http: %v", err)
	}
}

// mintCmd prints a signed JWT:  host mint --name laptop --days 365
func mintCmd(args []string) {
	fs := flag.NewFlagSet("mint", flag.ExitOnError)
	name := fs.String("name", "default", "subject/name for the token")
	days := fs.Int("days", 365, "validity in days")
	_ = fs.Parse(args)

	secret := []byte(os.Getenv("JWT_SECRET"))
	if len(secret) < 16 {
		log.Fatal("JWT_SECRET must be set (>= 16 chars)")
	}
	tok, exp, err := mintJWT(secret, *name, time.Duration(*days)*24*time.Hour)
	if err != nil {
		log.Fatalf("mint: %v", err)
	}
	fmt.Println(tok)
	fmt.Fprintf(os.Stderr, "subject=%q expires=%s\n", *name, exp.Format(time.RFC3339))
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
