package engine

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nexus/core/definitions"
)

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeSuccess writes a successful single-action response.
func writeSuccess(w http.ResponseWriter, requestID, connector, action string, data interface{}, latencyMS int64, cached bool, version string) {
	writeJSON(w, http.StatusOK, definitions.ResponseEnvelope{
		OK:        true,
		Connector: connector,
		Action:    action,
		Data:      data,
		Meta: definitions.ResponseMeta{
			RequestID:        requestID,
			LatencyMS:        latencyMS,
			Cached:           cached,
			ConnectorVersion: version,
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// writeError writes a structured error response.
func writeError(w http.ResponseWriter, status int, requestID, code, message, step string, upstreamStatus int, latencyMS int64) {
	writeJSON(w, status, definitions.ResponseEnvelope{
		OK: false,
		Error: &definitions.ErrorDetail{
			Code:           code,
			Message:        message,
			Step:           step,
			UpstreamStatus: upstreamStatus,
		},
		Meta: definitions.ResponseMeta{
			RequestID: requestID,
			LatencyMS: latencyMS,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// writePipelineSuccess writes a successful pipeline response.
func writePipelineSuccess(w http.ResponseWriter, env definitions.PipelineEnvelope) {
	writeJSON(w, http.StatusOK, env)
}

// writePipelineError writes a pipeline-level error response.
func writePipelineError(w http.ResponseWriter, status int, requestID, pipelineID, code, message string, latencyMS int64) {
	writeJSON(w, status, definitions.PipelineEnvelope{
		OK:         false,
		PipelineID: pipelineID,
		Steps:      map[string]definitions.StepResult{},
		Error: &definitions.ErrorDetail{
			Code:    code,
			Message: message,
		},
		Meta: definitions.PipelineMeta{
			RequestID:      requestID,
			TotalLatencyMS: latencyMS,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		},
	})
}
