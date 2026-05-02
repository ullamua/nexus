package engine

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"

        "github.com/nexus/core/connectors"
        "github.com/nexus/core/definitions"
        "github.com/nexus/core/diagnostics"
        "github.com/rs/zerolog"
)

// Gateway handles request validation, auth, and routing to dispatcher or pipeline.
type Gateway struct {
        dispatcher *Dispatcher
        pipeline   *PipelineExecutor
        metrics    *diagnostics.Metrics
        adminKey   string
        log        zerolog.Logger
}

// callHandler processes POST /call requests.
func (g *Gateway) callHandler(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        requestID := requestIDFromContext(r.Context())

        if err := validateContentType(r); err != nil {
                writeError(w, http.StatusBadRequest, requestID, "INVALID_CONTENT_TYPE", err.Error(), "", 0, 0)
                return
        }

        body, err := io.ReadAll(r.Body)
        if err != nil {
                if strings.Contains(err.Error(), "http: request body too large") {
                        writeError(w, http.StatusRequestEntityTooLarge, requestID, "BODY_TOO_LARGE", "request body exceeds limit", "", 0, 0)
                        return
                }
                writeError(w, http.StatusBadRequest, requestID, "READ_ERROR", "failed to read request body", "", 0, 0)
                return
        }

        var req definitions.CallRequest
        if err := json.Unmarshal(body, &req); err != nil {
                writeError(w, http.StatusBadRequest, requestID, "INVALID_JSON", "failed to parse request body", "", 0, 0)
                return
        }

        if req.Connector == "" {
                writeError(w, http.StatusBadRequest, requestID, "MISSING_CONNECTOR", "field 'connector' is required", "", 0, 0)
                return
        }
        if req.Action == "" {
                writeError(w, http.StatusBadRequest, requestID, "MISSING_ACTION", "field 'action' is required", "", 0, 0)
                return
        }

        result, err := g.dispatcher.Dispatch(r.Context(), req)
        elapsed := time.Since(start).Milliseconds()
        if err != nil {
                code := "CONNECTOR_ERROR"
                upstreamStatus := 0
                msg := err.Error()
                httpStatus := http.StatusInternalServerError

                if ce, ok := err.(*connectors.ConnectorError); ok {
                        code = ce.Code
                        upstreamStatus = ce.UpstreamStatus
                        if upstreamStatus == 404 {
                                httpStatus = http.StatusNotFound
                        } else if upstreamStatus >= 400 && upstreamStatus < 500 {
                                httpStatus = http.StatusBadGateway
                        }
                }
                if strings.Contains(msg, "not registered") {
                        code = "CONNECTOR_NOT_FOUND"
                        httpStatus = http.StatusNotFound
                }
                g.metrics.RecordRequest(req.Connector, req.Action, "error", elapsed)
                g.metrics.RecordConnectorError(req.Connector, code)
                writeError(w, httpStatus, requestID, code, msg, "", upstreamStatus, elapsed)
                return
        }

        def, _ := g.dispatcher.registry.Get(req.Connector)
        version := ""
        if def != nil {
                version = def.Version
        }
        g.metrics.RecordRequest(req.Connector, req.Action, "ok", elapsed)
        writeSuccess(w, requestID, req.Connector, req.Action, result.Data, elapsed, result.Cached, version)
}

// pipelineHandler processes POST /pipeline requests.
func (g *Gateway) pipelineHandler(w http.ResponseWriter, r *http.Request) {
        requestID := requestIDFromContext(r.Context())

        if err := validateContentType(r); err != nil {
                writeError(w, http.StatusBadRequest, requestID, "INVALID_CONTENT_TYPE", err.Error(), "", 0, 0)
                return
        }

        body, err := io.ReadAll(r.Body)
        if err != nil {
                writeError(w, http.StatusBadRequest, requestID, "READ_ERROR", "failed to read request body", "", 0, 0)
                return
        }

        var req definitions.PipelineRequest
        if err := json.Unmarshal(body, &req); err != nil {
                writeError(w, http.StatusBadRequest, requestID, "INVALID_JSON", "failed to parse request body", "", 0, 0)
                return
        }

        if len(req.Pipeline) == 0 {
                writeError(w, http.StatusBadRequest, requestID, "EMPTY_PIPELINE", "field 'pipeline' must contain at least one step", "", 0, 0)
                return
        }

        env := g.pipeline.Execute(r.Context(), req)
        writePipelineSuccess(w, env)
}

// authMiddleware validates the Nexus API key when one is configured.
func authMiddleware(nexusKey string) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        if nexusKey == "" {
                                next.ServeHTTP(w, r)
                                return
                        }
                        requestID := requestIDFromContext(r.Context())
                        provided := extractKey(r)
                        if provided != nexusKey {
                                writeError(w, http.StatusUnauthorized, requestID, "UNAUTHORIZED", "missing or invalid API key", "", 0, 0)
                                return
                        }
                        next.ServeHTTP(w, r)
                })
        }
}

// adminAuthMiddleware validates admin key for privileged endpoints.
func adminAuthMiddleware(adminKey string) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        if adminKey == "" {
                                next.ServeHTTP(w, r)
                                return
                        }
                        requestID := requestIDFromContext(r.Context())
                        provided := extractKey(r)
                        if provided != adminKey {
                                writeError(w, http.StatusUnauthorized, requestID, "UNAUTHORIZED", "admin key required", "", 0, 0)
                                return
                        }
                        next.ServeHTTP(w, r)
                })
        }
}

func extractKey(r *http.Request) string {
        if k := r.Header.Get("X-Nexus-Key"); k != "" {
                return k
        }
        auth := r.Header.Get("Authorization")
        if strings.HasPrefix(auth, "Bearer ") {
                return strings.TrimPrefix(auth, "Bearer ")
        }
        return ""
}

func validateContentType(r *http.Request) error {
        ct := r.Header.Get("Content-Type")
        if !strings.Contains(ct, "application/json") {
                return fmt.Errorf("Content-Type must be application/json, got %q", ct)
        }
        return nil
}
