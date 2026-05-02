package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexus/core/cache"
	"github.com/nexus/core/connectors"
	"github.com/nexus/core/definitions"
	"github.com/nexus/core/diagnostics"
	"github.com/nexus/core/engine"
	"github.com/nexus/core/intelligence"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestServer(response map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
}

func buildPipelineExec(t *testing.T, connectorDefs map[string]*definitions.ConnectorDef) *engine.PipelineExecutor {
	t.Helper()
	log := zerolog.Nop()
	path := t.TempDir() + "/vault.enc"
	vault, err := connectors.NewVault("test-key-32-characters-padding!!", path)
	require.NoError(t, err)
	mapper := intelligence.NewMapper()
	transformer := connectors.NewTransformer(mapper, log)
	runner := connectors.NewRunner(vault, transformer)
	met := diagnostics.NewMetrics()
	registry := connectors.NewRegistry(log)
	for _, def := range connectorDefs {
		registry.RegisterForTest(def)
	}
	return engine.NewPipelineExecutor(registry, runner, met, log)
}

func TestPipelineSequential(t *testing.T) {
	srv := makeTestServer(map[string]interface{}{"result": "step_output"})
	defer srv.Close()

	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: srv.URL,
		Actions: map[string]definitions.Action{
			"action": {Method: "GET", Path: "/"},
		},
	}
	exec := buildPipelineExec(t, map[string]*definitions.ConnectorDef{"mock": def})

	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "step1", Connector: "mock", Action: "action"},
			{ID: "step2", Connector: "mock", Action: "action", DependsOn: []string{"step1"}},
			{ID: "step3", Connector: "mock", Action: "action", DependsOn: []string{"step2"}},
		},
		Input: map[string]interface{}{},
	}

	env := exec.Execute(context.Background(), req)
	assert.True(t, env.OK)
	assert.Equal(t, "completed", env.Steps["step1"].Status)
	assert.Equal(t, "completed", env.Steps["step2"].Status)
	assert.Equal(t, "completed", env.Steps["step3"].Status)
}

func TestPipelineParallelSteps(t *testing.T) {
	var callCount int64
	var mu sync.Mutex
	startTimes := make([]time.Time, 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		startTimes = append(startTimes, time.Now())
		mu.Unlock()
		atomic.AddInt64(&callCount, 1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"result": "ok"})
	}))
	defer srv.Close()

	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: srv.URL,
		Actions: map[string]definitions.Action{
			"act": {Method: "GET", Path: "/"},
		},
	}
	exec := buildPipelineExec(t, map[string]*definitions.ConnectorDef{"mock": def})

	// step1 and step2 are independent; step3 depends on both.
	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "step1", Connector: "mock", Action: "act"},
			{ID: "step2", Connector: "mock", Action: "act"},
			{ID: "step3", Connector: "mock", Action: "act", DependsOn: []string{"step1", "step2"}},
		},
	}

	start := time.Now()
	env := exec.Execute(context.Background(), req)
	elapsed := time.Since(start)

	assert.True(t, env.OK)
	// Serial time would be ~150ms; parallel should be ~100ms.
	assert.Less(t, elapsed.Milliseconds(), int64(140), "parallel steps should execute concurrently")
	_ = startTimes
	_ = callCount
}

func TestPipelineTemplateResolution(t *testing.T) {
	srv := makeTestServer(map[string]interface{}{"token": "resolved_token"})
	defer srv.Close()

	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: srv.URL,
		Actions: map[string]definitions.Action{
			"encrypt": {Method: "GET", Path: "/"},
			"use":     {Method: "GET", Path: "/"},
		},
	}
	exec := buildPipelineExec(t, map[string]*definitions.ConnectorDef{"mock": def})

	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "step1", Connector: "mock", Action: "encrypt", Params: map[string]interface{}{"text": "{{input.content_id}}"}},
			{ID: "step2", Connector: "mock", Action: "use", Params: map[string]interface{}{"tok": "{{step1.data.token}}"}, DependsOn: []string{"step1"}},
		},
		Input: map[string]interface{}{"content_id": "dIG98qei6A"},
	}

	env := exec.Execute(context.Background(), req)
	assert.True(t, env.OK)
	assert.Equal(t, "completed", env.Steps["step1"].Status)
	assert.Equal(t, "completed", env.Steps["step2"].Status)
}

func TestPipelineFailurePropagation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: srv.URL,
		Actions: map[string]definitions.Action{
			"act": {Method: "GET", Path: "/"},
		},
	}
	exec := buildPipelineExec(t, map[string]*definitions.ConnectorDef{"mock": def})

	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "step1", Connector: "mock", Action: "act"},
			{ID: "step2", Connector: "mock", Action: "act", DependsOn: []string{"step1"}},
		},
	}

	env := exec.Execute(context.Background(), req)
	assert.False(t, env.Steps["step1"].OK)
	assert.Equal(t, "cancelled", env.Steps["step2"].Status)
}

func TestPipelineCircularDependency(t *testing.T) {
	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: "http://localhost",
		Actions: map[string]definitions.Action{"act": {Method: "GET", Path: "/"}},
	}
	exec := buildPipelineExec(t, map[string]*definitions.ConnectorDef{"mock": def})

	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "a", Connector: "mock", Action: "act", DependsOn: []string{"b"}},
			{ID: "b", Connector: "mock", Action: "act", DependsOn: []string{"a"}},
		},
	}

	env := exec.Execute(context.Background(), req)
	assert.False(t, env.OK)
	require.NotNil(t, env.Error)
	assert.Equal(t, "INVALID_PIPELINE", env.Error.Code)
}

func TestDispatcherCaching(t *testing.T) {
	var callCount int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "cus_abc"})
	}))
	defer srv.Close()

	log := zerolog.Nop()
	vaultPath := t.TempDir() + "/vault.enc"
	vault, err := connectors.NewVault("test-key-32-characters-padding!!", vaultPath)
	require.NoError(t, err)
	mapper := intelligence.NewMapper()
	transformer := connectors.NewTransformer(mapper, log)
	runner := connectors.NewRunner(vault, transformer)
	registry := connectors.NewRegistry(log)
	registry.RegisterForTest(&definitions.ConnectorDef{
		Name:    "api",
		BaseURL: srv.URL,
		Actions: map[string]definitions.Action{
			"get": {Method: "GET", Path: "/", Cache: true, CacheTTL: 60},
		},
	})
	lruStore, err := cache.NewLRUStore(256)
	require.NoError(t, err)
	met := diagnostics.NewMetrics()
	dispatcher := engine.NewDispatcher(registry, runner, lruStore, met)

	req := definitions.CallRequest{Connector: "api", Action: "get", Params: map[string]interface{}{}}
	_, err = dispatcher.Dispatch(context.Background(), req)
	require.NoError(t, err)
	_, err = dispatcher.Dispatch(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, int64(1), atomic.LoadInt64(&callCount), "second call should be served from cache")
}
