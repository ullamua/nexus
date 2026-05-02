package engine

import (
        "encoding/json"
        "io/fs"
        "net/http"
        "os"

        "github.com/go-chi/chi/v5"
        chimw "github.com/go-chi/chi/v5/middleware"
        "github.com/nexus/core/connectors"
        "github.com/nexus/core/diagnostics"
        "github.com/nexus/core/intelligence"
        "github.com/prometheus/client_golang/prometheus/promhttp"
        "github.com/rs/zerolog"
)

// Config holds all runtime configuration for the router.
type Config struct {
        NexusKey     string
        AdminKey     string
        MaxBodyBytes int64
        Dashboard    bool
        Trace        bool
        GlobalRPS    float64
        GlobalBurst  int
        ConnectorsDir string // used by /admin/reload
}

// BuildRouter constructs the chi router with all routes wired up.
func BuildRouter(
        cfg Config,
        registry *connectors.Registry,
        runner *connectors.Runner,
        dispatcher *Dispatcher,
        pipelineExec *PipelineExecutor,
        probe *diagnostics.Probe,
        tracer *diagnostics.Tracer,
        connStatuses map[string]diagnostics.ConnectorStatus,
        vault *connectors.Vault,
        met *diagnostics.Metrics,
        log zerolog.Logger,
        resolver *intelligence.Resolver,
        dashFS ...fs.FS,
) http.Handler {
        r := chi.NewRouter()

        r.Use(chimw.Recoverer)
        r.Use(requestIDMiddleware)
        r.Use(loggingMiddleware(log))
        r.Use(corsMiddleware)
        if cfg.MaxBodyBytes > 0 {
                r.Use(maxBodyMiddleware(cfg.MaxBodyBytes))
        }
        if cfg.GlobalRPS > 0 {
                rl := newRateLimiter(cfg.GlobalRPS, cfg.GlobalBurst)
                r.Use(rateLimitMiddleware(rl))
        }

        gw := &Gateway{
                dispatcher: dispatcher,
                pipeline:   pipelineExec,
                metrics:    met,
                adminKey:   cfg.AdminKey,
                log:        log,
        }

        // Public endpoints — no auth required.
        r.Get("/health", healthHandler(connStatuses))
        r.Get("/ready", readyHandler(registry))
        r.Get("/version", versionHandler())
        r.Get("/metrics", promhttp.HandlerFor(met.Registry, promhttp.HandlerOpts{}).ServeHTTP)

        // Authenticated endpoints.
        r.Group(func(r chi.Router) {
                r.Use(authMiddleware(cfg.NexusKey))
                r.Post("/call", gw.callHandler)
                r.Post("/pipeline", gw.pipelineHandler)
                r.Post("/pipeline/dry-run", dryRunHandler(registry))
                r.Post("/resolve", resolveHandler(resolver, registry))
                r.Get("/registry", registryHandler(registry))
                r.Get("/registry/{connector}", registryConnectorHandler(registry))
        })

        // Admin-only endpoints.
        r.Group(func(r chi.Router) {
                r.Use(adminAuthMiddleware(cfg.AdminKey))
                r.Get("/diagnostics/traces", tracesHandler(tracer))
                r.Post("/vault/set", vaultSetHandler(vault))
                r.Post("/admin/reload", reloadHandler(registry, cfg.ConnectorsDir, log))
        })

        if cfg.Dashboard && len(dashFS) > 0 && dashFS[0] != nil {
                sub, err := fs.Sub(dashFS[0], "dashboard")
                if err == nil {
                        fileServer := http.FileServer(http.FS(sub))
                        r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
                                http.Redirect(w, r, "/dashboard/", http.StatusMovedPermanently)
                        })
                        r.Handle("/dashboard/*", http.StripPrefix("/dashboard/", fileServer))
                }
        }

        return r
}

// ── Route handlers ─────────────────────────────────────────────────────────

func healthHandler(statuses map[string]diagnostics.ConnectorStatus) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                connHealth := make(map[string]string, len(statuses))
                for name, s := range statuses {
                        connHealth[name] = s.Status
                }
                writeJSON(w, http.StatusOK, map[string]interface{}{
                        "ok":         true,
                        "connectors": connHealth,
                })
        }
}

func readyHandler(registry *connectors.Registry) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                writeJSON(w, http.StatusOK, map[string]interface{}{
                        "ok":              true,
                        "connector_count": len(registry.Names()),
                })
        }
}

func versionHandler() http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                writeJSON(w, http.StatusOK, map[string]interface{}{
                        "ok":      true,
                        "name":    "nexus",
                        "version": "1.0.0",
                })
        }
}

func registryHandler(registry *connectors.Registry) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                all := registry.All()
                out := make([]map[string]interface{}, 0, len(all))
                for name, def := range all {
                        actions := make([]string, 0, len(def.Actions))
                        for a := range def.Actions {
                                actions = append(actions, a)
                        }
                        out = append(out, map[string]interface{}{
                                "name":        name,
                                "version":     def.Version,
                                "description": def.Description,
                                "protocol":    def.Protocol,
                                "actions":     actions,
                        })
                }
                writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "connectors": out})
        }
}

func registryConnectorHandler(registry *connectors.Registry) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                name := chi.URLParam(r, "connector")
                def, ok := registry.Get(name)
                if !ok {
                        writeJSON(w, http.StatusNotFound, map[string]interface{}{
                                "ok":    false,
                                "error": map[string]string{"code": "NOT_FOUND", "message": "connector not found"},
                        })
                        return
                }

                // Build actions map explicitly with lowercase keys so the dashboard
                // receives consistent field names regardless of struct tag resolution.
                actions := make(map[string]map[string]interface{}, len(def.Actions))
                for aName, a := range def.Actions {
                        schema := make(map[string]map[string]interface{}, len(a.InputSchema))
                        for field, fs := range a.InputSchema {
                                schema[field] = map[string]interface{}{
                                        "type":     fs.Type,
                                        "required": fs.Required,
                                }
                        }
                        actions[aName] = map[string]interface{}{
                                "method":      a.Method,
                                "path":        a.Path,
                                "description": a.Description,
                                "input_schema": schema,
                                "cache":       a.Cache,
                        }
                }

                writeJSON(w, http.StatusOK, map[string]interface{}{
                        "ok": true,
                        "connector": map[string]interface{}{
                                "name":        def.Name,
                                "version":     def.Version,
                                "description": def.Description,
                                "protocol":    def.Protocol,
                                "actions":     actions,
                        },
                })
        }
}

func tracesHandler(tracer *diagnostics.Tracer) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                traces := tracer.Recent(100)
                writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "traces": traces})
        }
}

func vaultSetHandler(vault *connectors.Vault) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                requestID := requestIDFromContext(r.Context())
                var req struct {
                        Connector string `json:"connector"`
                        Key       string `json:"key"`
                        Value     string `json:"value"`
                }
                if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                        writeError(w, http.StatusBadRequest, requestID, "INVALID_JSON", "failed to parse body", "", 0, 0)
                        return
                }
                if req.Connector == "" || req.Key == "" || req.Value == "" {
                        writeError(w, http.StatusBadRequest, requestID, "MISSING_FIELDS", "connector, key, and value are required", "", 0, 0)
                        return
                }
                if err := vault.Set(req.Connector, req.Key, req.Value); err != nil {
                        writeError(w, http.StatusInternalServerError, requestID, "VAULT_ERROR", err.Error(), "", 0, 0)
                        return
                }
                writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
        }
}

// reloadHandler hot-reloads all connector YAML files from disk without restarting.
func reloadHandler(registry *connectors.Registry, dir string, log zerolog.Logger) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                if dir == "" {
                        dir = "./connectors.d"
                }
                if err := registry.Reload(dir); err != nil {
                        requestID := requestIDFromContext(r.Context())
                        writeError(w, http.StatusInternalServerError, requestID, "RELOAD_ERROR", err.Error(), "", 0, 0)
                        return
                }
                names := registry.Names()
                log.Info().Int("count", len(names)).Msg("connectors reloaded via admin API")
                writeJSON(w, http.StatusOK, map[string]interface{}{
                        "ok":              true,
                        "connector_count": len(names),
                        "connectors":      names,
                })
        }
}

func envOrDefault(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}
