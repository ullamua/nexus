package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/nexus/core/cache"
	"github.com/nexus/core/connectors"
	"github.com/nexus/core/definitions"
	"github.com/nexus/core/diagnostics"
)

// Dispatcher routes a CallRequest to the correct connector runner.
type Dispatcher struct {
	registry *connectors.Registry
	runner   *connectors.Runner
	cache    cache.Store
	metrics  *diagnostics.Metrics
}

// NewDispatcher creates a Dispatcher.
func NewDispatcher(registry *connectors.Registry, runner *connectors.Runner, c cache.Store, metrics *diagnostics.Metrics) *Dispatcher {
	return &Dispatcher{registry: registry, runner: runner, cache: c, metrics: metrics}
}

// DispatchResult holds the outcome of a dispatched call.
type DispatchResult struct {
	Data      map[string]interface{}
	LatencyMS int64
	Cached    bool
}

// Dispatch looks up the connector and executes the action.
func (d *Dispatcher) Dispatch(ctx context.Context, req definitions.CallRequest) (*DispatchResult, error) {
	def, ok := d.registry.Get(req.Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q not registered", req.Connector)
	}

	action, ok := def.Actions[req.Action]
	if !ok {
		return nil, &connectors.ConnectorError{
			Code:    "ACTION_NOT_FOUND",
			Message: fmt.Sprintf("action %q not found in connector %s", req.Action, req.Connector),
		}
	}

	// Determine cache settings: request options override connector action settings.
	useCache := req.Options.Cache || action.Cache
	cacheTTL := time.Duration(req.Options.CacheTTLSeconds) * time.Second
	if cacheTTL == 0 && action.CacheTTL > 0 {
		cacheTTL = time.Duration(action.CacheTTL) * time.Second
	}
	if cacheTTL == 0 {
		cacheTTL = 30 * time.Second
	}

	cacheKey := buildCacheKey(req)

	if useCache && d.cache != nil {
		if cached, ok := d.cache.Get(cacheKey); ok {
			d.metrics.RecordCacheHit(true)
			if data, ok := cached.(map[string]interface{}); ok {
				return &DispatchResult{Data: data, Cached: true}, nil
			}
		}
		d.metrics.RecordCacheHit(false)
	}

	result, err := d.runner.Execute(ctx, def, req.Action, req.Params)
	if err != nil {
		return nil, err
	}

	if useCache && d.cache != nil {
		d.cache.Set(cacheKey, result.Data, cacheTTL)
	}

	return &DispatchResult{Data: result.Data, LatencyMS: result.LatencyMS, Cached: false}, nil
}

func buildCacheKey(req definitions.CallRequest) string {
	return fmt.Sprintf("nexus:%s:%s:%v", req.Connector, req.Action, req.Params)
}
