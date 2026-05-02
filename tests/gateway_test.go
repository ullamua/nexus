package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func buildTestRouter(t *testing.T, connectorDefs map[string]*definitions.ConnectorDef, nexusKey, adminKey string) http.Handler {
	t.Helper()
	log := zerolog.Nop()
	vaultPath := t.TempDir() + "/vault.enc"
	vault, err := connectors.NewVault("test-key-32-characters-padding!!", vaultPath)
	require.NoError(t, err)

	registry := connectors.NewRegistry(log)
	for _, def := range connectorDefs {
		registry.RegisterForTest(def)
	}

	mapper := intelligence.NewMapper()
	resolver := intelligence.NewResolver(mapper)
	transformer := connectors.NewTransformer(mapper, log)
	runner := connectors.NewRunner(vault, transformer)
	met := diagnostics.NewMetrics()

	lruStore, err := cache.NewLRUStore(256)
	require.NoError(t, err)

	dispatcher := engine.NewDispatcher(registry, runner, lruStore, met)
	pipelineExec := engine.NewPipelineExecutor(registry, runner, met, log)
	probe := diagnostics.NewProbe(log)
	tracer := diagnostics.NewTracer(100)
	statuses := map[string]diagnostics.ConnectorStatus{}

	cfg := engine.Config{
		NexusKey:     nexusKey,
		AdminKey:     adminKey,
		MaxBodyBytes: 1 * 1024 * 1024,
		Dashboard:    false,
	}

	return engine.BuildRouter(cfg, registry, runner, dispatcher, pipelineExec, probe, tracer, statuses, vault, met, log, resolver)
}

func makeCallBody(connector, action string, params map[string]interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"connector": connector,
		"action":    action,
		"params":    params,
	})
	return b
}

// ── /call tests ───────────────────────────────────────────────────────────────

func TestGatewayValidRequestRoutes(t *testing.T) {
	upstream := makeTestServer(map[string]interface{}{"id": "cus_123"})
	defer upstream.Close()

	def := &definitions.ConnectorDef{
		Name:    "stripe",
		BaseURL: upstream.URL,
		Actions: map[string]definitions.Action{
			"create_customer": {Method: "POST", Path: "/customers"},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"stripe": def}, "", "")

	body := makeCallBody("stripe", "create_customer", map[string]interface{}{"email": "test@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var env definitions.ResponseEnvelope
	require.NoError(t, json.NewDecoder(w.Body).Decode(&env))
	assert.True(t, env.OK)
}

func TestGatewayMissingConnectorReturns400(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	body := []byte(`{"action":"foo","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGatewayOversizedBodyReturns413(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	bigBody := strings.Repeat("x", 2*1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/call", strings.NewReader(bigBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestGatewayMissingAuthKeyReturns401(t *testing.T) {
	router := buildTestRouter(t, nil, "secret-nexus-key", "")
	body := makeCallBody("stripe", "create_customer", nil)
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGatewayUnknownConnectorReturns404(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	body := makeCallBody("nonexistent", "action", nil)
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGatewayMissingRequiredParam(t *testing.T) {
	upstream := makeTestServer(map[string]interface{}{"id": "u_1"})
	defer upstream.Close()

	def := &definitions.ConnectorDef{
		Name:    "myapi",
		BaseURL: upstream.URL,
		Actions: map[string]definitions.Action{
			"lookup": {
				Method: "GET",
				Path:   "/users",
				InputSchema: map[string]definitions.FieldSchema{
					"user_id": {Type: "string", Required: true},
				},
			},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"myapi": def}, "", "")

	// Call without the required user_id param.
	body := makeCallBody("myapi", "lookup", map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var env definitions.ResponseEnvelope
	require.NoError(t, json.NewDecoder(w.Body).Decode(&env))
	assert.False(t, env.OK)
	require.NotNil(t, env.Error)
	assert.Equal(t, "MISSING_REQUIRED_PARAM", env.Error.Code)
}

// ── /pipeline/dry-run tests ───────────────────────────────────────────────────

func makeDryRunBody(steps []map[string]interface{}, input map[string]interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"pipeline": steps,
		"input":    input,
	})
	return b
}

func TestDryRunValidPipeline(t *testing.T) {
	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: "http://localhost",
		Actions: map[string]definitions.Action{
			"search":  {Method: "GET", Path: "/"},
			"details": {Method: "GET", Path: "/"},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"mock": def}, "", "")

	body := makeDryRunBody([]map[string]interface{}{
		{"id": "step1", "connector": "mock", "action": "search", "params": map[string]interface{}{"q": "{{input.query}}"}},
		{"id": "step2", "connector": "mock", "action": "details", "depends_on": []string{"step1"}},
	}, map[string]interface{}{"query": "naruto"})

	req := httptest.NewRequest(http.MethodPost, "/pipeline/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.DryRunResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result.OK, "valid pipeline should pass dry-run: %v", result.Issues)
	assert.Equal(t, 2, result.StepCount)
	assert.Equal(t, []string{"step1", "step2"}, result.ExecutionOrder)
	assert.Empty(t, result.Issues)
}

func TestDryRunUnknownConnector(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")

	body := makeDryRunBody([]map[string]interface{}{
		{"id": "step1", "connector": "ghost", "action": "foo"},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/pipeline/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.DryRunResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.False(t, result.OK)
	require.NotEmpty(t, result.Issues)
	assert.Equal(t, "UNKNOWN_CONNECTOR", result.Issues[0].Code)
}

func TestDryRunUnknownAction(t *testing.T) {
	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: "http://localhost",
		Actions: map[string]definitions.Action{
			"search": {Method: "GET", Path: "/"},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"mock": def}, "", "")

	body := makeDryRunBody([]map[string]interface{}{
		{"id": "step1", "connector": "mock", "action": "nonexistent"},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/pipeline/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.DryRunResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.False(t, result.OK)
	require.NotEmpty(t, result.Issues)
	assert.Equal(t, "UNKNOWN_ACTION", result.Issues[0].Code)
}

func TestDryRunCyclicDependency(t *testing.T) {
	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: "http://localhost",
		Actions: map[string]definitions.Action{
			"act": {Method: "GET", Path: "/"},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"mock": def}, "", "")

	body := makeDryRunBody([]map[string]interface{}{
		{"id": "a", "connector": "mock", "action": "act", "depends_on": []string{"b"}},
		{"id": "b", "connector": "mock", "action": "act", "depends_on": []string{"a"}},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/pipeline/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.DryRunResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.False(t, result.OK)
	found := false
	for _, issue := range result.Issues {
		if issue.Code == "CYCLE_DETECTED" {
			found = true
		}
	}
	assert.True(t, found, "expected CYCLE_DETECTED issue")
}

func TestDryRunForwardTemplateReference(t *testing.T) {
	def := &definitions.ConnectorDef{
		Name:    "mock",
		BaseURL: "http://localhost",
		Actions: map[string]definitions.Action{
			"a": {Method: "GET", Path: "/"},
			"b": {Method: "GET", Path: "/"},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"mock": def}, "", "")

	// step1 tries to use step2's output — but step2 hasn't run yet.
	body := makeDryRunBody([]map[string]interface{}{
		{"id": "step1", "connector": "mock", "action": "a", "params": map[string]interface{}{"x": "{{step2.data.token}}"}},
		{"id": "step2", "connector": "mock", "action": "b"},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/pipeline/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.DryRunResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.False(t, result.OK)
	found := false
	for _, issue := range result.Issues {
		if issue.Code == "FORWARD_REFERENCE" {
			found = true
		}
	}
	assert.True(t, found, "expected FORWARD_REFERENCE issue, got: %v", result.Issues)
}

// ── /resolve tests ────────────────────────────────────────────────────────────

func TestResolveEndpointMatchesDescription(t *testing.T) {
	def := &definitions.ConnectorDef{
		Name:    "anime",
		BaseURL: "http://localhost",
		Actions: map[string]definitions.Action{
			"search": {Method: "GET", Path: "/", Description: "search for anime by keyword"},
			"detail": {Method: "GET", Path: "/", Description: "get anime episode list"},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"anime": def}, "", "")

	body, _ := json.Marshal(map[string]interface{}{"intent": "search anime"})
	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.ResolveResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result.OK)
	assert.Equal(t, "anime", result.Connector)
	assert.NotEmpty(t, result.Action)
	assert.Greater(t, result.Score, 0.0)
}

func TestResolveMissingIntentReturns400(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	body := []byte(`{"intent":""}`)
	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── /health and /version tests ────────────────────────────────────────────────

func TestHealthEndpointReturnsOK(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

func TestVersionEndpointReturnsOK(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "nexus", body["name"])
}
