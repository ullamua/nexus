//go:build live

// Live end-to-end tests against the real hosted APIs.
// Run with: go test -tags live -v -timeout 120s ./tests/ -run TestLive
//
// These tests require outbound internet access and will make real HTTP calls
// to the two live API endpoints:
//   - https://your-animetsu-api-url
//   - https://your-animekai-api-url

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// ─── helpers ────────────────────────────────────────────────────────────────

func buildLiveRouter(t *testing.T) http.Handler {
	t.Helper()
	log := zerolog.Nop()
	vaultPath := t.TempDir() + "/vault.enc"
	vault, err := connectors.NewVault("live-test-key-32-characters-pad!!", vaultPath)
	require.NoError(t, err)

	registry := connectors.NewRegistry(log)
	require.NoError(t, registry.LoadDir("../connectors.d"), "loading connector YAMLs")

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
		MaxBodyBytes: 2 * 1024 * 1024,
		Dashboard:    false,
	}
	return engine.BuildRouter(cfg, registry, runner, dispatcher, pipelineExec, probe, tracer, statuses, vault, met, log, resolver)
}

func buildLiveDispatcher(t *testing.T) *engine.Dispatcher {
	t.Helper()
	log := zerolog.Nop()
	vaultPath := t.TempDir() + "/vault.enc"
	vault, err := connectors.NewVault("live-test-key-32-characters-pad!!", vaultPath)
	require.NoError(t, err)

	registry := connectors.NewRegistry(log)
	require.NoError(t, registry.LoadDir("../connectors.d"))

	mapper := intelligence.NewMapper()
	transformer := connectors.NewTransformer(mapper, log)
	runner := connectors.NewRunner(vault, transformer)
	met := diagnostics.NewMetrics()

	lruStore, err := cache.NewLRUStore(256)
	require.NoError(t, err)

	return engine.NewDispatcher(registry, runner, lruStore, met)
}

func buildLivePipeline(t *testing.T) (*engine.PipelineExecutor, *connectors.Registry) {
	t.Helper()
	log := zerolog.Nop()
	vaultPath := t.TempDir() + "/vault.enc"
	vault, err := connectors.NewVault("live-test-key-32-characters-pad!!", vaultPath)
	require.NoError(t, err)

	registry := connectors.NewRegistry(log)
	require.NoError(t, registry.LoadDir("../connectors.d"))

	mapper := intelligence.NewMapper()
	transformer := connectors.NewTransformer(mapper, log)
	runner := connectors.NewRunner(vault, transformer)
	met := diagnostics.NewMetrics()

	return engine.NewPipelineExecutor(registry, runner, met, log), registry
}

func callViaRouter(t *testing.T, router http.Handler, connector, action string, params map[string]interface{}) definitions.ResponseEnvelope {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"connector": connector,
		"action":    action,
		"params":    params,
	})
	req := httptest.NewRequest(http.MethodPost, "/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var env definitions.ResponseEnvelope
	require.NoError(t, json.NewDecoder(w.Body).Decode(&env),
		"response body should be valid JSON (HTTP %d)", w.Code)
	return env
}

// ─── Animetsu live tests ─────────────────────────────────────────────────────

func TestLiveAnimetsuHealth(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "health",
		Params:    map[string]interface{}{},
	})
	require.NoError(t, err, "animetsu /healthz should respond without error")
	assert.NotEmpty(t, result.Data, "health response should not be empty")
	t.Logf("animetsu health: %+v  latency=%dms", result.Data, result.LatencyMS)
}

func TestLiveAnimetsuHome(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "home",
		Params:    map[string]interface{}{},
	})
	require.NoError(t, err, "animetsu home should succeed")
	assert.NotEmpty(t, result.Data, "home response should contain season data")
	_, hasSeasonal := result.Data["seasonal"]
	_, hasTrending := result.Data["trending"]
	assert.True(t, hasSeasonal || hasTrending, "home should include seasonal or trending key")
	t.Logf("animetsu home keys=%v  latency=%dms", keysOf(result.Data), result.LatencyMS)
}

func TestLiveAnimetsuTrending(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "trending",
		Params:    map[string]interface{}{},
	})
	require.NoError(t, err)
	arr, ok := result.Data["results"].([]interface{})
	require.True(t, ok, "trending should return a list wrapped under 'results'")
	assert.GreaterOrEqual(t, len(arr), 1, "trending list should not be empty")
	first := arr[0].(map[string]interface{})
	assert.NotEmpty(t, first["title"], "first trending item should have a title")
	t.Logf("animetsu trending: %d items, first=%q  latency=%dms", len(arr), first["title"], result.LatencyMS)
}

func TestLiveAnimetsuSearch(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "search",
		Params:    map[string]interface{}{"q": "naruto"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Data, "search should return data")
	t.Logf("animetsu search(naruto) keys=%v  latency=%dms", keysOf(result.Data), result.LatencyMS)
}

func TestLiveAnimetsuAnimeDetail(t *testing.T) {
	const narutoID = "6989b89f29cf95f4eb03b4ef"
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "anime_detail",
		Params:    map[string]interface{}{"id": narutoID},
	})
	require.NoError(t, err, "anime_detail for Naruto should succeed")
	assert.NotEmpty(t, result.Data)
	t.Logf("animetsu anime_detail keys=%v  latency=%dms", keysOf(result.Data), result.LatencyMS)
}

func TestLiveAnimetsuEpisodes(t *testing.T) {
	const narutoID = "6989b89f29cf95f4eb03b4ef"
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "episodes",
		Params:    map[string]interface{}{"id": narutoID},
	})
	require.NoError(t, err)
	arr, ok := result.Data["results"].([]interface{})
	require.True(t, ok, "episodes should be a list")
	assert.GreaterOrEqual(t, len(arr), 200, "Naruto has 220 episodes")
	first := arr[0].(map[string]interface{})
	assert.NotNil(t, first["ep_num"], "episode should have ep_num")
	t.Logf("animetsu episodes: %d eps, first=%v  latency=%dms", len(arr), first["ep_num"], result.LatencyMS)
}

func TestLiveAnimetsuRecent(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "recent",
		Params:    map[string]interface{}{"page": "1"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Data)
	t.Logf("animetsu recent keys=%v  latency=%dms", keysOf(result.Data), result.LatencyMS)
}

func TestLiveAnimetsuSchedule(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animetsu",
		Action:    "schedule",
		Params:    map[string]interface{}{},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Data)
	t.Logf("animetsu schedule keys=%v  latency=%dms", keysOf(result.Data), result.LatencyMS)
}

// ─── AnimeKai live tests ─────────────────────────────────────────────────────

func TestLiveAnimekaiTrending(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animekai",
		Action:    "trending",
		Params:    map[string]interface{}{"page": "1"},
	})
	require.NoError(t, err, "animekai trending should succeed")
	arr, ok := result.Data["results"].([]interface{})
	require.True(t, ok, "animekai trending should return a list")
	assert.GreaterOrEqual(t, len(arr), 1)
	first := arr[0].(map[string]interface{})
	assert.NotEmpty(t, first["title"], "trending item should have a title")
	t.Logf("animekai trending: %d items, first=%q  latency=%dms", len(arr), first["title"], result.LatencyMS)
}

func TestLiveAnimekaiLatest(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animekai",
		Action:    "latest",
		Params:    map[string]interface{}{"page": "1"},
	})
	require.NoError(t, err)
	arr, ok := result.Data["results"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(arr), 1)
	t.Logf("animekai latest: %d items  latency=%dms", len(arr), result.LatencyMS)
}

func TestLiveAnimekaiSearch(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animekai",
		Action:    "search",
		Params:    map[string]interface{}{"keyword": "one piece", "page": "1"},
	})
	require.NoError(t, err)
	arr, ok := result.Data["results"].([]interface{})
	require.True(t, ok, "search should return a results list")
	assert.GreaterOrEqual(t, len(arr), 1)
	first := arr[0].(map[string]interface{})
	assert.NotEmpty(t, first["title"])
	t.Logf("animekai search(one piece): %d results, first=%q  latency=%dms", len(arr), first["title"], result.LatencyMS)
}

func TestLiveAnimekaiFilters(t *testing.T) {
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animekai",
		Action:    "filters",
		Params:    map[string]interface{}{},
	})
	require.NoError(t, err)
	_, hasTypes := result.Data["types"]
	_, hasGenres := result.Data["genres"]
	assert.True(t, hasTypes || hasGenres, "filters should include types or genres")
	t.Logf("animekai filters keys=%v  latency=%dms", keysOf(result.Data), result.LatencyMS)
}

func TestLiveAnimekaiDetails(t *testing.T) {
	const onePieceID = "watch/one-piece-dk6r"
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animekai",
		Action:    "details",
		Params:    map[string]interface{}{"id": onePieceID},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Data)
	assert.NotEmpty(t, result.Data["title"], "details should include title")
	t.Logf("animekai details(One Piece) title=%q  latency=%dms", result.Data["title"], result.LatencyMS)
}

func TestLiveAnimekaiEpisodes(t *testing.T) {
	const onePieceID = "watch/one-piece-dk6r"
	d := buildLiveDispatcher(t)
	result, err := d.Dispatch(context.Background(), definitions.CallRequest{
		Connector: "animekai",
		Action:    "episodes",
		Params:    map[string]interface{}{"id": onePieceID},
	})
	require.NoError(t, err)
	arr, ok := result.Data["results"].([]interface{})
	require.True(t, ok, "episodes should return a list")
	assert.GreaterOrEqual(t, len(arr), 100, "One Piece has 1000+ episodes")
	first := arr[0].(map[string]interface{})
	assert.NotEmpty(t, first["token"], "episode should have a token for server lookup")
	t.Logf("animekai episodes(One Piece): %d eps, latest token=%q  latency=%dms",
		len(arr), first["token"], result.LatencyMS)
}

// ─── Cross-API pipeline tests ────────────────────────────────────────────────

func TestLivePipeline_AnimetsuSearchThenDetail(t *testing.T) {
	exec, _ := buildLivePipeline(t)

	req := definitions.PipelineRequest{
		Input: map[string]interface{}{"query": "attack on titan"},
		Pipeline: []definitions.PipelineStep{
			{
				ID:        "search",
				Connector: "animetsu",
				Action:    "search",
				Params:    map[string]interface{}{"q": "{{input.query}}"},
			},
			{
				ID:        "detail",
				Connector: "animetsu",
				Action:    "anime_detail",
				Params:    map[string]interface{}{"id": "6989b89f29cf95f4eb03b4ef"},
				DependsOn: []string{"search"},
			},
		},
	}

	env := exec.Execute(context.Background(), req)
	assert.True(t, env.OK, "pipeline should succeed; error=%v", env.Error)
	assert.Equal(t, "completed", env.Steps["search"].Status)
	assert.Equal(t, "completed", env.Steps["detail"].Status)
	t.Logf("pipeline search→detail  total_latency=%dms", env.Meta.TotalLatencyMS)
}

func TestLivePipeline_AnimekaiSearchThenDetails(t *testing.T) {
	exec, _ := buildLivePipeline(t)

	req := definitions.PipelineRequest{
		Input: map[string]interface{}{"keyword": "naruto"},
		Pipeline: []definitions.PipelineStep{
			{
				ID:        "search",
				Connector: "animekai",
				Action:    "search",
				Params:    map[string]interface{}{"keyword": "{{input.keyword}}", "page": "1"},
			},
			{
				ID:        "details",
				Connector: "animekai",
				Action:    "details",
				Params:    map[string]interface{}{"id": "watch/naruto-9r5k"},
				DependsOn: []string{"search"},
			},
		},
	}

	env := exec.Execute(context.Background(), req)
	assert.True(t, env.OK, "pipeline should succeed; error=%v", env.Error)
	assert.Equal(t, "completed", env.Steps["search"].Status)
	assert.Equal(t, "completed", env.Steps["details"].Status)
	t.Logf("pipeline search→details  total_latency=%dms", env.Meta.TotalLatencyMS)
}

func TestLivePipeline_ParallelBothAPIs(t *testing.T) {
	exec, _ := buildLivePipeline(t)

	req := definitions.PipelineRequest{
		Pipeline: []definitions.PipelineStep{
			{ID: "animetsu_trending", Connector: "animetsu", Action: "trending", Params: map[string]interface{}{}},
			{ID: "animekai_trending", Connector: "animekai", Action: "trending", Params: map[string]interface{}{"page": "1"}},
			{
				ID:        "done",
				Connector: "animetsu",
				Action:    "health",
				Params:    map[string]interface{}{},
				DependsOn: []string{"animetsu_trending", "animekai_trending"},
			},
		},
	}

	env := exec.Execute(context.Background(), req)
	assert.True(t, env.OK, "parallel pipeline should succeed; error=%v", env.Error)
	assert.Equal(t, "completed", env.Steps["animetsu_trending"].Status)
	assert.Equal(t, "completed", env.Steps["animekai_trending"].Status)
	assert.Equal(t, "completed", env.Steps["done"].Status)
	assert.GreaterOrEqual(t, env.Meta.ParallelSteps, 2, "should have counted parallel steps")
	t.Logf("parallel pipeline latency=%dms", env.Meta.TotalLatencyMS)
}

func TestLivePipeline_ThreeStepAnimekai(t *testing.T) {
	exec, _ := buildLivePipeline(t)

	const knownEpisodeToken = "dd379-zzpxXh1mhcy5KH"
	req := definitions.PipelineRequest{
		Input: map[string]interface{}{
			"anime_id":      "watch/naruto-shippuuden-mv9v",
			"episode_token": knownEpisodeToken,
		},
		Pipeline: []definitions.PipelineStep{
			{
				ID:        "search",
				Connector: "animekai",
				Action:    "search",
				Params:    map[string]interface{}{"keyword": "naruto shippuden", "page": "1"},
			},
			{
				ID:        "episodes",
				Connector: "animekai",
				Action:    "episodes",
				Params:    map[string]interface{}{"id": "{{input.anime_id}}"},
				DependsOn: []string{"search"},
			},
			{
				ID:        "servers",
				Connector: "animekai",
				Action:    "servers",
				Params:    map[string]interface{}{"episode_token": "{{input.episode_token}}"},
				DependsOn: []string{"episodes"},
			},
		},
	}

	env := exec.Execute(context.Background(), req)
	assert.Equal(t, "completed", env.Steps["search"].Status)
	assert.Equal(t, "completed", env.Steps["episodes"].Status)
	assert.NotEqual(t, "cancelled", env.Steps["servers"].Status, "servers step should not be cancelled")
	t.Logf("3-step pipeline: search=%s episodes=%s servers=%s  total=%dms",
		env.Steps["search"].Status,
		env.Steps["episodes"].Status,
		env.Steps["servers"].Status,
		env.Meta.TotalLatencyMS)
}

func TestLiveRouterCallAnimetsuTrending(t *testing.T) {
	router := buildLiveRouter(t)
	env := callViaRouter(t, router, "animetsu", "trending", map[string]interface{}{})
	assert.True(t, env.OK, "router call should succeed; error=%v", env.Error)
	assert.Equal(t, "animetsu", env.Connector)
	assert.Equal(t, "trending", env.Action)
	assert.NotEmpty(t, env.Meta.RequestID)
	assert.NotEmpty(t, env.Meta.ConnectorVersion)
	assert.NotZero(t, env.Meta.LatencyMS)
	t.Logf("router animetsu/trending: latency=%dms requestID=%s", env.Meta.LatencyMS, env.Meta.RequestID)
}

func TestLiveRouterCallAnimekaiSearch(t *testing.T) {
	router := buildLiveRouter(t)
	env := callViaRouter(t, router, "animekai", "search", map[string]interface{}{"keyword": "dragon ball", "page": "1"})
	assert.True(t, env.OK, "router call should succeed; error=%v", env.Error)
	assert.Equal(t, "animekai", env.Connector)
	t.Logf("router animekai/search: latency=%dms", env.Meta.LatencyMS)
}

// ─── Live dry-run test ───────────────────────────────────────────────────────

func TestLiveDryRunAnimetsuPipeline(t *testing.T) {
	router := buildLiveRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"input": map[string]interface{}{"query": "naruto"},
		"pipeline": []map[string]interface{}{
			{"id": "search", "connector": "animetsu", "action": "search", "params": map[string]interface{}{"q": "{{input.query}}"}},
			{"id": "detail", "connector": "animetsu", "action": "anime_detail", "params": map[string]interface{}{"id": "{{input.query}}"}, "depends_on": []string{"search"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/pipeline/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result engine.DryRunResult
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.True(t, result.OK, "live connectors should pass dry-run: %v", result.Issues)
	t.Logf("live dry-run: %s", result.Summary)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
