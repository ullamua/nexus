package diagnostics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/nexus/core/definitions"
	"github.com/rs/zerolog"
)

// ConnectorStatus describes the liveness of a registered connector.
type ConnectorStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // healthy | degraded | unreachable | skipped
	Message string `json:"message,omitempty"`
}

// Probe runs startup health checks for all registered connectors.
type Probe struct {
	log zerolog.Logger
}

// NewProbe creates a Probe with the given logger.
func NewProbe(log zerolog.Logger) *Probe {
	return &Probe{log: log}
}

// CheckAll runs health checks concurrently and returns statuses.
func (p *Probe) CheckAll(connectors map[string]*definitions.ConnectorDef) map[string]ConnectorStatus {
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make(map[string]ConnectorStatus, len(connectors))

	for name, def := range connectors {
		wg.Add(1)
		go func(n string, d *definitions.ConnectorDef) {
			defer wg.Done()
			status := p.check(n, d)
			mu.Lock()
			results[n] = status
			mu.Unlock()
		}(name, def)
	}
	wg.Wait()

	p.logSummary(results)
	return results
}

func (p *Probe) check(name string, def *definitions.ConnectorDef) ConnectorStatus {
	if def.Protocol == "grpc" {
		return ConnectorStatus{Name: name, Status: "skipped", Message: "gRPC health check not implemented"}
	}
	if def.BaseURL == "" {
		return ConnectorStatus{Name: name, Status: "skipped", Message: "no base_url configured"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, def.BaseURL, nil)
	if err != nil {
		return ConnectorStatus{Name: name, Status: "unreachable", Message: err.Error()}
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectorStatus{Name: name, Status: "degraded", Message: fmt.Sprintf("probe failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return ConnectorStatus{Name: name, Status: "degraded", Message: fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}
	return ConnectorStatus{Name: name, Status: "healthy"}
}

func (p *Probe) logSummary(results map[string]ConnectorStatus) {
	for _, s := range results {
		event := p.log.Info()
		if s.Status != "healthy" {
			event = p.log.Warn()
		}
		event.Str("connector", s.Name).Str("status", s.Status).Str("message", s.Message).Msg("connector probe")
	}
}
