// Command analytics is a tiny, cookieless, self-hosted analytics server.
//
// Configuration is done entirely through environment variables (see README.md).
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed dashboard
var dashboardFS embed.FS

// a.js is the drop-in <script> client, built from ../client (pnpm build copies it here).
//
//go:embed public/a.js
var scriptJS []byte

type config struct {
	addr      string
	dbPath    string
	user      string
	pass      string
	sites     map[string]bool // nil = accept any site key
	retention int             // days, 0 = keep forever
	r2        *r2Config
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadConfig() config {
	c := config{
		addr:   env("ADDR", ":8080"),
		dbPath: env("DB_PATH", "/data/analytics.db"),
		user:   env("DASHBOARD_USER", "admin"),
		pass:   os.Getenv("DASHBOARD_PASSWORD"),
	}
	if s := os.Getenv("SITES"); s != "" {
		c.sites = map[string]bool{}
		for _, k := range strings.Split(s, ",") {
			if k = strings.TrimSpace(k); k != "" {
				c.sites[k] = true
			}
		}
	}
	c.retention = atoiDef(os.Getenv("RETENTION_DAYS"), 0)
	c.r2 = loadR2()
	return c
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		r, err := http.Get("http://127.0.0.1" + env("ADDR", ":8080") + "/health")
		if err != nil || r.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg := loadConfig()
	if cfg.pass == "" {
		log.Println("WARNING: DASHBOARD_PASSWORD not set, dashboard is disabled")
	}
	st, err := openStore(cfg.dbPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go st.writer(ctx)
	go maintenance(ctx, st, cfg)

	dash, _ := fs.Sub(dashboardFS, "dashboard")
	mux := http.NewServeMux()
	mux.Handle("/e", &ingest{st: st, sites: cfg.sites})
	mux.HandleFunc("/a.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(scriptJS)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("/api/stats", auth(cfg, http.HandlerFunc(st.statsHandler)))
	mux.Handle("/api/sites", auth(cfg, http.HandlerFunc(st.sitesHandler)))
	mux.Handle("/", auth(cfg, http.FileServerFS(dash)))

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		srv.Shutdown(sctx)
	}()
	log.Printf("listening on %s", cfg.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	st.close()
}

func auth(cfg config, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if cfg.pass == "" || !ok || !secureEq(u, cfg.user) || !secureEq(p, cfg.pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="analytics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// maintenance runs daily: retention cleanup and optional R2 backup.
func maintenance(ctx context.Context, st *store, cfg config) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	last := ""
	for {
		if day := time.Now().UTC().Format("2006-01-02"); day != last && time.Now().UTC().Hour() >= 3 {
			last = day
			if cfg.retention > 0 {
				if n, err := st.purge(time.Now().AddDate(0, 0, -cfg.retention)); err != nil {
					log.Println("purge:", err)
				} else if n > 0 {
					log.Printf("purged %d events", n)
				}
			}
			if cfg.r2 != nil {
				if err := backup(ctx, st, cfg.r2, day); err != nil {
					log.Println("backup:", err)
				} else {
					log.Println("backup uploaded")
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
