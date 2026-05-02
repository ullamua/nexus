package diagnostics

import (
	"context"
	"sync"
	"time"

	"github.com/rs/xid"
)

type contextKey string

const traceKey contextKey = "nexus-trace"

// Trace holds timing information for a single request.
type Trace struct {
	RequestID  string                 `json:"request_id"`
	StartedAt  time.Time              `json:"started_at"`
	TotalMS    int64                  `json:"total_ms"`
	Connector  string                 `json:"connector,omitempty"`
	Action     string                 `json:"action,omitempty"`
	Steps      map[string]*StepTrace  `json:"steps,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// StepTrace holds timing for a single pipeline step.
type StepTrace struct {
	StartedAt  time.Time `json:"started_at"`
	LatencyMS  int64     `json:"latency_ms"`
	Status     string    `json:"status"`
}

// Tracer stores the last N request traces in a circular buffer.
type Tracer struct {
	mu     sync.Mutex
	buf    []*Trace
	head   int
	size   int
	cap    int
}

// NewTracer creates a Tracer with the given buffer capacity.
func NewTracer(capacity int) *Tracer {
	return &Tracer{
		buf: make([]*Trace, capacity),
		cap: capacity,
	}
}

// NewTrace creates a new Trace and attaches it to the context.
func NewTrace(ctx context.Context, requestID string) (context.Context, *Trace) {
	if requestID == "" {
		requestID = xid.New().String()
	}
	t := &Trace{
		RequestID: requestID,
		StartedAt: time.Now(),
		Steps:     make(map[string]*StepTrace),
	}
	return context.WithValue(ctx, traceKey, t), t
}

// FromContext retrieves the Trace from context.
func FromContext(ctx context.Context) (*Trace, bool) {
	t, ok := ctx.Value(traceKey).(*Trace)
	return t, ok
}

// Finish records the total elapsed time for a trace.
func (t *Trace) Finish() {
	t.TotalMS = time.Since(t.StartedAt).Milliseconds()
}

// StartStep begins timing a named pipeline step.
func (t *Trace) StartStep(id string) {
	t.Steps[id] = &StepTrace{StartedAt: time.Now()}
}

// FinishStep records the end of a pipeline step.
func (t *Trace) FinishStep(id, status string) {
	if s, ok := t.Steps[id]; ok {
		s.LatencyMS = time.Since(s.StartedAt).Milliseconds()
		s.Status = status
	}
}

// Record stores a completed trace in the circular buffer.
func (tr *Tracer) Record(t *Trace) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.buf[tr.head] = t
	tr.head = (tr.head + 1) % tr.cap
	if tr.size < tr.cap {
		tr.size++
	}
}

// Recent returns the most recent n traces (up to buffer size).
func (tr *Tracer) Recent(n int) []*Trace {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if n > tr.size {
		n = tr.size
	}
	out := make([]*Trace, 0, n)
	for i := 0; i < n; i++ {
		idx := (tr.head - 1 - i + tr.cap) % tr.cap
		if tr.buf[idx] != nil {
			out = append(out, tr.buf[idx])
		}
	}
	return out
}
