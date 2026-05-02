package engine

import (
        "context"
        "net/http"
        "sync"
        "time"

        "github.com/rs/xid"
        "github.com/rs/zerolog"
)

type contextKeyType string

const contextKeyRequestID contextKeyType = "request_id"

func requestIDFromContext(ctx context.Context) string {
        v, _ := ctx.Value(contextKeyRequestID).(string)
        return v
}

func contextWithRequestID(ctx context.Context, id string) context.Context {
        return context.WithValue(ctx, contextKeyRequestID, id)
}

// requestIDMiddleware injects a unique request ID into every request.
func requestIDMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                id := xid.New().String()
                w.Header().Set("X-Request-ID", id)
                r = r.WithContext(contextWithRequestID(r.Context(), id))
                next.ServeHTTP(w, r)
        })
}

// loggingMiddleware logs method, path, status, and latency for every request.
func loggingMiddleware(log zerolog.Logger) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        start := time.Now()
                        rw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
                        next.ServeHTTP(rw, r)
                        log.Info().
                                Str("method", r.Method).
                                Str("path", r.URL.Path).
                                Int("status", rw.code).
                                Int64("latency_ms", time.Since(start).Milliseconds()).
                                Str("request_id", requestIDFromContext(r.Context())).
                                Msg("request")
                })
        }
}

// corsMiddleware adds permissive CORS headers.
func corsMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Access-Control-Allow-Origin", "*")
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Nexus-Key")
                if r.Method == http.MethodOptions {
                        w.WriteHeader(http.StatusNoContent)
                        return
                }
                next.ServeHTTP(w, r)
        })
}

// maxBodyMiddleware rejects request bodies larger than limit bytes.
func maxBodyMiddleware(limitBytes int64) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        r.Body = http.MaxBytesReader(w, r.Body, limitBytes)
                        next.ServeHTTP(w, r)
                })
        }
}

// rateLimiter is a per-key token bucket.
type rateLimiter struct {
        mu      sync.Mutex
        buckets map[string]*tokenBucket
        rps     float64
        burst   float64
}

type tokenBucket struct {
        tokens   float64
        lastTime time.Time
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
        return &rateLimiter{
                buckets: make(map[string]*tokenBucket),
                rps:     rps,
                burst:   float64(burst),
        }
}

func (rl *rateLimiter) allow(key string) bool {
        rl.mu.Lock()
        defer rl.mu.Unlock()
        b, ok := rl.buckets[key]
        if !ok {
                b = &tokenBucket{tokens: rl.burst, lastTime: time.Now()}
                rl.buckets[key] = b
        }
        now := time.Now()
        b.tokens += now.Sub(b.lastTime).Seconds() * rl.rps
        if b.tokens > rl.burst {
                b.tokens = rl.burst
        }
        b.lastTime = now
        if b.tokens < 1 {
                return false
        }
        b.tokens--
        return true
}

// rateLimitMiddleware applies a global token-bucket rate limit keyed by remote IP.
func rateLimitMiddleware(rl *rateLimiter) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        if !rl.allow(r.RemoteAddr) {
                                requestID := requestIDFromContext(r.Context())
                                writeError(w, http.StatusTooManyRequests, requestID, "RATE_LIMITED", "global rate limit exceeded — slow down", "", 0, 0)
                                return
                        }
                        next.ServeHTTP(w, r)
                })
        }
}

// statusWriter captures the HTTP status code.
type statusWriter struct {
        http.ResponseWriter
        code int
}

func (sw *statusWriter) WriteHeader(code int) {
        sw.code = code
        sw.ResponseWriter.WriteHeader(code)
}
