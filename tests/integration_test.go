package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexus/core/definitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationFullCallFlow(t *testing.T) {
	upstream := makeTestServer(map[string]interface{}{
		"id":    "cus_xyz",
		"email": "user@example.com",
	})
	defer upstream.Close()

	def := &definitions.ConnectorDef{
		Name:    "testapi",
		Version: "1.0.0",
		BaseURL: upstream.URL,
		Actions: map[string]definitions.Action{
			"create": {
				Method:    "POST",
				Path:      "/resource",
				OutputMap: map[string]string{"id": "resource_id"},
			},
		},
	}
	router := buildTestRouter(t, map[string]*definitions.ConnectorDef{"testapi": def}, "", "")

	body, _ := json.Marshal(map[string]interface{}{
		"connector": "testapi",
		"action":    "create",
		"params":    map[string]interface{}{"name": "test"},
	})
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var env definitions.ResponseEnvelope
	require.NoError(t, json.NewDecoder(w.Body).Decode(&env))
	assert.True(t, env.OK)
	assert.Equal(t, "testapi", env.Connector)
	assert.Equal(t, "create", env.Action)
	assert.NotEmpty(t, env.Meta.RequestID)
}

func TestIntegrationPipelineThreeSteps(t *testing.T) {
	srv := makeTestServer(map[string]interface{}{"token": "enc_abc", "data": "dec_xyz"})
	defer srv.Close()

	def := &definitions.ConnectorDef{
		Name:    "pipeline_api",
		BaseURL: srv.URL,
		Actions: map[string]definitions.Action{
			"encrypt": {Method: "GET", Path: "/"},
			"fetch":   {Method: "GET", Path: "/"},
			"decrypt": {Method: "GET", Path: "/"},
		},
	}
	exec := buildPipelineExec(t, map[string]*definitions.ConnectorDef{"pipeline_api": def})

	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "step1", Connector: "pipeline_api", Action: "encrypt", Params: map[string]interface{}{"text": "{{input.id}}"}},
			{ID: "step2", Connector: "pipeline_api", Action: "fetch", Params: map[string]interface{}{"enc": "{{step1.data.token}}"}, DependsOn: []string{"step1"}},
			{ID: "step3", Connector: "pipeline_api", Action: "decrypt", Params: map[string]interface{}{"data": "{{step2.data.data}}"}, DependsOn: []string{"step2"}},
		},
		Input: map[string]interface{}{"id": "content_001"},
	}

	env := exec.Execute(context.Background(), req)
	assert.Equal(t, "completed", env.Steps["step1"].Status)
	assert.Equal(t, "completed", env.Steps["step2"].Status)
	assert.Equal(t, "completed", env.Steps["step3"].Status)
}

func TestIntegrationHealthEndpoint(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["ok"])
}

func TestIntegrationReadyEndpoint(t *testing.T) {
	router := buildTestRouter(t, nil, "", "")
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["ok"])
}
