package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/nexus/core/connectors"
	"github.com/nexus/core/definitions"
)

// DryRunIssue describes a single validation problem found in a pipeline.
type DryRunIssue struct {
	Step    string `json:"step,omitempty"`
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DryRunResult is the response shape for POST /pipeline/dry-run.
type DryRunResult struct {
	OK             bool          `json:"ok"`
	Issues         []DryRunIssue `json:"issues"`
	StepCount      int           `json:"step_count"`
	ExecutionOrder []string      `json:"execution_order"`
	Summary        string        `json:"summary"`
}

var dryRunTemplateRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// dryRunHandler validates a pipeline request without executing any connector calls.
// It checks: step ID uniqueness, connector/action existence, depends_on references,
// DAG acyclicity, and template variable resolvability.
func dryRunHandler(registry *connectors.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFromContext(r.Context())

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

		var issues []DryRunIssue
		stepIDs := make(map[string]bool, len(req.Pipeline))

		// Pass 1: check duplicate IDs and missing required fields.
		for _, step := range req.Pipeline {
			if step.ID == "" {
				issues = append(issues, DryRunIssue{
					Code:    "MISSING_STEP_ID",
					Message: "a step is missing its 'id' field",
				})
				continue
			}
			if stepIDs[step.ID] {
				issues = append(issues, DryRunIssue{
					Step:    step.ID,
					Code:    "DUPLICATE_STEP_ID",
					Message: fmt.Sprintf("step id %q appears more than once", step.ID),
				})
			}
			stepIDs[step.ID] = true
		}

		// Pass 2: validate connector + action references and depends_on.
		for _, step := range req.Pipeline {
			if step.ID == "" {
				continue
			}
			if step.Connector == "" {
				issues = append(issues, DryRunIssue{
					Step:    step.ID,
					Code:    "MISSING_CONNECTOR",
					Message: "step is missing 'connector' field",
				})
				continue
			}
			def, ok := registry.Get(step.Connector)
			if !ok {
				issues = append(issues, DryRunIssue{
					Step:    step.ID,
					Code:    "UNKNOWN_CONNECTOR",
					Message: fmt.Sprintf("connector %q is not registered", step.Connector),
				})
				continue
			}
			if step.Action == "" {
				issues = append(issues, DryRunIssue{
					Step:    step.ID,
					Code:    "MISSING_ACTION",
					Message: "step is missing 'action' field",
				})
				continue
			}
			if _, ok := def.Actions[step.Action]; !ok {
				issues = append(issues, DryRunIssue{
					Step:    step.ID,
					Code:    "UNKNOWN_ACTION",
					Message: fmt.Sprintf("action %q not found in connector %q (available: %s)", step.Action, step.Connector, joinActionNames(def)),
				})
			}
			for _, dep := range step.DependsOn {
				if !stepIDs[dep] {
					issues = append(issues, DryRunIssue{
						Step:    step.ID,
						Code:    "UNKNOWN_DEPENDENCY",
						Message: fmt.Sprintf("depends_on %q refers to an unknown step id", dep),
					})
				}
			}
		}

		// Pass 3: topological sort — detect cycles.
		order, topoErr := topoSort(req.Pipeline)
		if topoErr != nil {
			issues = append(issues, DryRunIssue{
				Code:    "CYCLE_DETECTED",
				Message: topoErr.Error(),
			})
		}

		// Pass 4: validate template variable references in params.
		if topoErr == nil && len(issues) == 0 {
			// Build a set of steps that have been "executed" by the time we reach each step.
			executedBefore := make(map[string]map[string]bool, len(order))
			seen := make(map[string]bool)
			for _, stepID := range order {
				executedBefore[stepID] = copySet(seen)
				seen[stepID] = true
			}

			for _, stepID := range order {
				step := findStep(req.Pipeline, stepID)
				if step == nil {
					continue
				}
				before := executedBefore[stepID]

				for field, val := range step.Params {
					s, ok := val.(string)
					if !ok {
						continue
					}
					for _, m := range dryRunTemplateRe.FindAllStringSubmatch(s, -1) {
						ref := strings.TrimSpace(m[1])
						parts := strings.SplitN(ref, ".", 2)
						prefix := parts[0]

						switch prefix {
						case "params":
							// {{params.x}} — always valid, resolved from call-time params.
						case "input":
							// {{input.x}} — valid if input key exists.
							if len(parts) > 1 && req.Input != nil {
								if _, exists := req.Input[parts[1]]; !exists {
									issues = append(issues, DryRunIssue{
										Step:    stepID,
										Field:   field,
										Code:    "MISSING_INPUT_KEY",
										Message: fmt.Sprintf("{{%s}}: key %q is not declared in pipeline 'input'", ref, parts[1]),
									})
								}
							}
						default:
							if !stepIDs[prefix] {
								issues = append(issues, DryRunIssue{
									Step:    stepID,
									Field:   field,
									Code:    "UNKNOWN_TEMPLATE_NAMESPACE",
									Message: fmt.Sprintf("{{%s}}: %q is not a known step id or 'input'", ref, prefix),
								})
							} else if !before[prefix] {
								issues = append(issues, DryRunIssue{
									Step:    stepID,
									Field:   field,
									Code:    "FORWARD_REFERENCE",
									Message: fmt.Sprintf("{{%s}}: step %q has not executed yet at this point in the DAG", ref, prefix),
								})
							}
						}
					}
				}
			}
		}

		ok := len(issues) == 0
		summary := fmt.Sprintf("%d step(s) validated — no issues found", len(req.Pipeline))
		if !ok {
			summary = fmt.Sprintf("%d step(s) validated — %d issue(s) found", len(req.Pipeline), len(issues))
		}
		if issues == nil {
			issues = []DryRunIssue{}
		}

		writeJSON(w, http.StatusOK, DryRunResult{
			OK:             ok,
			Issues:         issues,
			StepCount:      len(req.Pipeline),
			ExecutionOrder: order,
			Summary:        summary,
		})
	}
}

func copySet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func joinActionNames(def *definitions.ConnectorDef) string {
	names := make([]string, 0, len(def.Actions))
	for k := range def.Actions {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
