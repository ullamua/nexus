package main

import (
        "fmt"
        "net/http"
        "os"
        "strconv"
        "time"

        "github.com/nexus/core/cache"
        "github.com/nexus/core/connectors"
        "github.com/nexus/core/diagnostics"
        "github.com/nexus/core/engine"
        "github.com/nexus/core/intelligence"
        "github.com/nexus/core/static"
        "github.com/rs/zerolog"
        "github.com/rs/zerolog/log"
)

func main() {
        // Vault key is required — fail loudly rather than silently.
        vaultKey := os.Getenv("NEXUS_VAULT_KEY")
        if vaultKey == "" {
                fmt.Fprintln(os.Stderr, "")
                fmt.Fprintln(os.Stderr, "  ERROR: NEXUS_VAULT_KEY is not set.")
                fmt.Fprintln(os.Stderr, "  Run:  make setup   then edit .env and set the key.")
                fmt.Fprintln(os.Stderr, "  Tip:  openssl rand -hex 32")
                fmt.Fprintln(os.Stderr, "")
                os.Exit(1)
        }

        logger := buildLogger()

        // Initialize vault.
        vaultPath := envOr("NEXUS_VAULT_PATH", "vault.enc")
        vault, err := connectors.NewVault(vaultKey, vaultPath)
        if err != nil {
                logger.Fatal().Err(err).Msg("failed to initialize vault")
        }

        // Load connector registry.
        connectorsDir := envOr("NEXUS_CONNECTORS_DIR", "./connectors.d")
        registry := connectors.NewRegistry(logger)
        if err := registry.LoadDir(connectorsDir); err != nil {
                logger.Warn().Err(err).Str("dir", connectorsDir).Msg("connector directory load warning")
        }

        // Initialize intelligence layer.
        mapper := intelligence.NewMapper()
        resolver := intelligence.NewResolver(mapper)

        // Initialize transformer and runner.
        transformer := connectors.NewTransformer(mapper, logger)
        runner := connectors.NewRunner(vault, transformer)

        // Initialize cache: Redis if REDIS_URL is set, otherwise LRU.
        var cacheStore cache.Store
        if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
                redisStore, err := cache.NewRedisStore(redisURL)
                if err != nil {
                        logger.Warn().Err(err).Msg("redis unavailable, falling back to LRU cache")
                        cacheStore = buildLRU(logger)
                } else {
                        logger.Info().Str("url", redisURL).Msg("using redis cache")
                        cacheStore = redisStore
                }
        } else {
                cacheStore = buildLRU(logger)
        }

        // Initialize diagnostics.
        met := diagnostics.NewMetrics()
        tracer := diagnostics.NewTracer(1000)
        probe := diagnostics.NewProbe(logger)

        // Run connector health probes.
        connStatuses := probe.CheckAll(registry.All())

        // Build engine components.
        dispatcher := engine.NewDispatcher(registry, runner, cacheStore, met)
        pipelineExec := engine.NewPipelineExecutor(registry, runner, met, logger)

        // Build global rate limiter if configured.
        globalRPS := 0.0
        globalBurst := 100
        if v := os.Getenv("NEXUS_RATE_LIMIT_GLOBAL"); v != "" {
                if f, err := strconv.ParseFloat(v, 64); err == nil {
                        globalRPS = f
                }
        }

        maxBody := int64(1 * 1024 * 1024)
        if v := os.Getenv("NEXUS_MAX_BODY_MB"); v != "" {
                if n, err := strconv.ParseInt(v, 10, 64); err == nil {
                        maxBody = n * 1024 * 1024
                }
        }

        cfg := engine.Config{
                NexusKey:      os.Getenv("NEXUS_API_KEY"),
                AdminKey:      os.Getenv("NEXUS_ADMIN_KEY"),
                MaxBodyBytes:  maxBody,
                Dashboard:     os.Getenv("NEXUS_DASHBOARD") == "true",
                Trace:         os.Getenv("NEXUS_TRACE") == "true",
                GlobalRPS:     globalRPS,
                GlobalBurst:   globalBurst,
                ConnectorsDir: connectorsDir,
        }

        router := engine.BuildRouter(cfg, registry, runner, dispatcher, pipelineExec, probe, tracer, connStatuses, vault, met, logger, resolver, static.DashboardFS)

        port := envOr("NEXUS_PORT", "8080")
        addr := fmt.Sprintf(":%s", port)
        srv := &http.Server{
                Addr:         addr,
                Handler:      router,
                ReadTimeout:  30 * time.Second,
                WriteTimeout: 60 * time.Second,
                IdleTimeout:  120 * time.Second,
        }

        printBanner(port, connectorsDir, len(registry.Names()), cfg)
        if err := srv.ListenAndServe(); err != nil {
                logger.Fatal().Err(err).Msg("server error")
        }
}

func printBanner(port, connectorsDir string, connCount int, cfg engine.Config) {
        authStatus := "none (open)"
        if cfg.NexusKey != "" {
                authStatus = "X-Nexus-Key / Bearer"
        }
        adminStatus := "none"
        if cfg.AdminKey != "" {
                adminStatus = "X-Nexus-Key / Bearer"
        }
        rateLimit := "unlimited"
        if cfg.GlobalRPS > 0 {
                rateLimit = fmt.Sprintf("%.0f req/s", cfg.GlobalRPS)
        }

        fmt.Printf("\n")
        fmt.Printf("  ███╗   ██╗███████╗██╗  ██╗██╗   ██╗███████╗\n")
        fmt.Printf("  ████╗  ██║██╔════╝╚██╗██╔╝██║   ██║██╔════╝\n")
        fmt.Printf("  ██╔██╗ ██║█████╗   ╚███╔╝ ██║   ██║███████╗\n")
        fmt.Printf("  ██║╚██╗██║██╔══╝   ██╔██╗ ██║   ██║╚════██║\n")
        fmt.Printf("  ██║ ╚████║███████╗██╔╝ ██╗╚██████╔╝███████║\n")
        fmt.Printf("  ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝ ╚═════╝ ╚══════╝\n")
        fmt.Printf("\n")
        fmt.Printf("  Universal API Protocol Engine  v1.0.0\n")
        fmt.Printf("  ─────────────────────────────────────────────\n")
        fmt.Printf("  Listening    http://localhost:%s\n", port)
        fmt.Printf("  Connectors   %d loaded from %s\n", connCount, connectorsDir)
        fmt.Printf("  Auth         %s\n", authStatus)
        fmt.Printf("  Admin auth   %s\n", adminStatus)
        fmt.Printf("  Rate limit   %s\n", rateLimit)
        if cfg.Dashboard {
                fmt.Printf("  Dashboard    http://localhost:%s/dashboard/\n", port)
        } else {
                fmt.Printf("  Dashboard    disabled  (set NEXUS_DASHBOARD=true)\n")
        }
        fmt.Printf("\n")
        fmt.Printf("  Endpoints:\n")
        fmt.Printf("    GET  /health             connector health\n")
        fmt.Printf("    GET  /ready              readiness probe\n")
        fmt.Printf("    GET  /version            version info\n")
        fmt.Printf("    GET  /metrics            prometheus metrics\n")
        fmt.Printf("    GET  /registry           list all connectors\n")
        fmt.Printf("    GET  /registry/{name}    connector schema\n")
        fmt.Printf("    POST /call               call a connector action\n")
        fmt.Printf("    POST /pipeline           execute a DAG pipeline\n")
        fmt.Printf("    POST /pipeline/dry-run   validate pipeline (no calls made)\n")
        fmt.Printf("    POST /resolve            map intent → connector + action\n")
        fmt.Printf("    GET  /diagnostics/traces request trace history  [admin]\n")
        fmt.Printf("    POST /vault/set          store encrypted credential [admin]\n")
        fmt.Printf("    POST /admin/reload       hot-reload connectors from disk [admin]\n")
        fmt.Printf("\n")
}

func buildLogger() zerolog.Logger {
        level := zerolog.InfoLevel
        switch os.Getenv("NEXUS_LOG_LEVEL") {
        case "debug":
                level = zerolog.DebugLevel
        case "warn":
                level = zerolog.WarnLevel
        case "error":
                level = zerolog.ErrorLevel
        }
        return log.Level(level).With().Timestamp().Logger()
}

func buildLRU(log zerolog.Logger) cache.Store {
        lruStore, err := cache.NewLRUStore(4096)
        if err != nil {
                log.Fatal().Err(err).Msg("failed to initialize LRU cache")
        }
        log.Info().Msg("using in-process LRU cache")
        return lruStore
}

func envOr(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}
