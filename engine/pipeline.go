package engine

import (
        "context"
        "fmt"
        "strings"
        "sync"
        "time"

        "github.com/nexus/core/connectors"
        "github.com/nexus/core/definitions"
        "github.com/nexus/core/diagnostics"
        "github.com/rs/zerolog"
        "github.com/rs/xid"
)

// PipelineExecutor runs DAG pipelines of connector actions.
type PipelineExecutor struct {
        registry *connectors.Registry
        runner   *connectors.Runner
        metrics  *diagnostics.Metrics
        log      zerolog.Logger
}

// NewPipelineExecutor creates a PipelineExecutor.
func NewPipelineExecutor(registry *connectors.Registry, runner *connectors.Runner, metrics *diagnostics.Metrics, log zerolog.Logger) *PipelineExecutor {
        return &PipelineExecutor{registry: registry, runner: runner, metrics: metrics, log: log}
}

type stepState struct {
        result definitions.StepResult
        data   map[string]interface{}
}

// Execute runs the pipeline and returns a PipelineEnvelope.
func (pe *PipelineExecutor) Execute(ctx context.Context, req definitions.PipelineRequest) definitions.PipelineEnvelope {
        pipelineID := "pipe_" + xid.New().String()
        start := time.Now()
        requestID := requestIDFromContext(ctx)

        order, err := topoSort(req.Pipeline)
        if err != nil {
                return definitions.PipelineEnvelope{
                        OK:         false,
                        PipelineID: pipelineID,
                        Steps:      map[string]definitions.StepResult{},
                        Error:      &definitions.ErrorDetail{Code: "INVALID_PIPELINE", Message: err.Error()},
                        Meta: definitions.PipelineMeta{
                                RequestID: requestID,
                                Timestamp: time.Now().UTC().Format(time.RFC3339),
                        },
                }
        }

        results := make(map[string]*stepState, len(req.Pipeline))
        stepsMu := sync.Mutex{}
        parallelCount := 0

        // Group steps by topological level so independent steps run in parallel.
        levels := buildLevels(order, req.Pipeline)

        for _, level := range levels {
                if len(level) > 1 {
                        parallelCount += len(level)
                }
                var wg sync.WaitGroup
                for _, stepID := range level {
                        step := findStep(req.Pipeline, stepID)
                        if step == nil {
                                continue
                        }

                        // Check if any dependency failed → cancel this step.
                        stepsMu.Lock()
                        cancelled := false
                        for _, dep := range step.DependsOn {
                                if s, ok := results[dep]; ok && !s.result.OK {
                                        cancelled = true
                                        break
                                }
                        }
                        stepsMu.Unlock()

                        if cancelled {
                                stepsMu.Lock()
                                results[step.ID] = &stepState{
                                        result: definitions.StepResult{Status: "cancelled", OK: false,
                                                Error: &definitions.ErrorDetail{Code: "DEPENDENCY_FAILED", Message: "upstream step failed"}},
                                }
                                stepsMu.Unlock()
                                continue
                        }

                        wg.Add(1)
                        go func(s definitions.PipelineStep) {
                                defer wg.Done()
                                state := pe.runStep(ctx, s, req.Input, results, &stepsMu)
                                stepsMu.Lock()
                                results[s.ID] = state
                                stepsMu.Unlock()
                                pe.metrics.RecordPipelineStep(s.Connector, s.Action, state.result.Status)
                        }(*step)
                }
                wg.Wait()
        }

        // Build final steps map and determine last step result.
        finalSteps := make(map[string]definitions.StepResult, len(results))
        var lastResult interface{}
        allOK := true
        for id, state := range results {
                finalSteps[id] = state.result
                if !state.result.OK {
                        allOK = false
                }
                lastResult = state.data
        }
        // Return last step's data as the pipeline result.
        if len(order) > 0 {
                last := order[len(order)-1]
                if s, ok := results[last]; ok {
                        lastResult = s.data
                }
        }

        return definitions.PipelineEnvelope{
                OK:         allOK,
                PipelineID: pipelineID,
                Steps:      finalSteps,
                Result:     lastResult,
                Meta: definitions.PipelineMeta{
                        RequestID:      requestID,
                        TotalLatencyMS: time.Since(start).Milliseconds(),
                        ParallelSteps:  parallelCount,
                        Timestamp:      time.Now().UTC().Format(time.RFC3339),
                },
        }
}

func (pe *PipelineExecutor) runStep(ctx context.Context, step definitions.PipelineStep, input map[string]interface{}, results map[string]*stepState, mu *sync.Mutex) *stepState {
        start := time.Now()

        def, ok := pe.registry.Get(step.Connector)
        if !ok {
                return &stepState{result: definitions.StepResult{
                        OK:        false,
                        Status:    "failed",
                        LatencyMS: time.Since(start).Milliseconds(),
                        Error:     &definitions.ErrorDetail{Code: "CONNECTOR_NOT_FOUND", Message: fmt.Sprintf("connector %q not registered", step.Connector)},
                }}
        }

        // Resolve template params.
        mu.Lock()
        templateCtx := buildTemplateContext(input, results)
        mu.Unlock()
        resolvedParams := resolveParams(step.Params, templateCtx)

        runResult, err := pe.runner.Execute(ctx, def, step.Action, resolvedParams)
        elapsed := time.Since(start).Milliseconds()
        if err != nil {
                code := "CONNECTOR_ERROR"
                upstreamStatus := 0
                if ce, ok := err.(*connectors.ConnectorError); ok {
                        code = ce.Code
                        upstreamStatus = ce.UpstreamStatus
                }
                return &stepState{result: definitions.StepResult{
                        OK:        false,
                        Status:    "failed",
                        LatencyMS: elapsed,
                        Error:     &definitions.ErrorDetail{Code: code, Message: err.Error(), Step: step.ID, UpstreamStatus: upstreamStatus},
                }}
        }

        return &stepState{
                data: runResult.Data,
                result: definitions.StepResult{
                        OK:        true,
                        Data:      runResult.Data,
                        LatencyMS: runResult.LatencyMS,
                        Status:    "completed",
                },
        }
}

// topoSort returns step IDs in topological order or an error if a cycle exists.
func topoSort(steps []definitions.PipelineStep) ([]string, error) {
        inDegree := make(map[string]int, len(steps))
        adj := make(map[string][]string, len(steps))
        ids := make(map[string]bool, len(steps))

        for _, s := range steps {
                ids[s.ID] = true
                if _, ok := inDegree[s.ID]; !ok {
                        inDegree[s.ID] = 0
                }
        }
        for _, s := range steps {
                for _, dep := range s.DependsOn {
                        if !ids[dep] {
                                return nil, fmt.Errorf("pipeline: step %q depends on unknown step %q", s.ID, dep)
                        }
                        adj[dep] = append(adj[dep], s.ID)
                        inDegree[s.ID]++
                }
        }

        queue := []string{}
        for id, deg := range inDegree {
                if deg == 0 {
                        queue = append(queue, id)
                }
        }

        order := make([]string, 0, len(steps))
        for len(queue) > 0 {
                cur := queue[0]
                queue = queue[1:]
                order = append(order, cur)
                for _, next := range adj[cur] {
                        inDegree[next]--
                        if inDegree[next] == 0 {
                                queue = append(queue, next)
                        }
                }
        }

        if len(order) != len(steps) {
                return nil, fmt.Errorf("pipeline: circular dependency detected among steps")
        }
        return order, nil
}

// buildLevels groups step IDs by execution level (steps at the same level can run in parallel).
func buildLevels(order []string, steps []definitions.PipelineStep) [][]string {
        depMap := make(map[string][]string, len(steps))
        for _, s := range steps {
                depMap[s.ID] = s.DependsOn
        }

        levelOf := make(map[string]int, len(order))
        for _, id := range order {
                max := -1
                for _, dep := range depMap[id] {
                        if l := levelOf[dep]; l > max {
                                max = l
                        }
                }
                levelOf[id] = max + 1
        }

        maxLevel := 0
        for _, l := range levelOf {
                if l > maxLevel {
                        maxLevel = l
                }
        }
        levels := make([][]string, maxLevel+1)
        for id, l := range levelOf {
                levels[l] = append(levels[l], id)
        }
        return levels
}

func findStep(steps []definitions.PipelineStep, id string) *definitions.PipelineStep {
        for i := range steps {
                if steps[i].ID == id {
                        return &steps[i]
                }
        }
        return nil
}

// buildTemplateContext creates a flat key→value map for template resolution.
// Arrays and nested maps are recursively expanded so that templates like
// {{search.data.results.0.id}} resolve to the correct scalar value.
func buildTemplateContext(input map[string]interface{}, results map[string]*stepState) map[string]interface{} {
        ctx := make(map[string]interface{})
        for k, v := range input {
                flattenCtx(ctx, "input."+k, v)
        }
        for stepID, state := range results {
                for k, v := range state.data {
                        flattenCtx(ctx, stepID+".data."+k, v)
                }
        }
        return ctx
}

// flattenCtx adds prefix→v to ctx, then recursively adds indexed sub-keys for
// arrays (prefix.0, prefix.1, …) and map sub-keys (prefix.field).
func flattenCtx(ctx map[string]interface{}, prefix string, v interface{}) {
        ctx[prefix] = v
        switch val := v.(type) {
        case []interface{}:
                for i, item := range val {
                        flattenCtx(ctx, fmt.Sprintf("%s.%d", prefix, i), item)
                }
        case map[string]interface{}:
                for k, child := range val {
                        flattenCtx(ctx, prefix+"."+k, child)
                }
        }
}

// resolveParams substitutes {{...}} templates in param values.
func resolveParams(params map[string]interface{}, ctx map[string]interface{}) map[string]interface{} {
        out := make(map[string]interface{}, len(params))
        for k, v := range params {
                if s, ok := v.(string); ok {
                        out[k] = resolveParamString(s, ctx)
                } else {
                        out[k] = v
                }
        }
        return out
}

func resolveParamString(tmpl string, ctx map[string]interface{}) string {
        result := tmpl
        for key, val := range ctx {
                placeholder := "{{" + key + "}}"
                if strings.Contains(result, placeholder) {
                        result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", val))
                }
        }
        return result
}
