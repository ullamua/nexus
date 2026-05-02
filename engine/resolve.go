package engine

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/nexus/core/connectors"
	"github.com/nexus/core/intelligence"
)

type resolveRequest struct {
	Intent string `json:"intent"`
}

// ResolveResult is the response shape for POST /resolve.
type ResolveResult struct {
	OK        bool    `json:"ok"`
	Connector string  `json:"connector,omitempty"`
	Action    string  `json:"action,omitempty"`
	Score     float64 `json:"score"`
	Confident bool    `json:"confident"`
	Message   string  `json:"message,omitempty"`
}

// resolveHandler uses semantic similarity to map a natural-language intent
// onto the best-matching registered connector + action pair.
func resolveHandler(resolver *intelligence.Resolver, registry *connectors.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFromContext(r.Context())

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, requestID, "READ_ERROR", "failed to read request body", "", 0, 0)
			return
		}

		var req resolveRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, requestID, "INVALID_JSON", "failed to parse request body", "", 0, 0)
			return
		}

		if req.Intent == "" {
			writeError(w, http.StatusBadRequest, requestID, "MISSING_INTENT", "field 'intent' is required", "", 0, 0)
			return
		}

		connector, action, score := resolver.Resolve(req.Intent, registry.All())

		msg := ""
		switch {
		case connector == "":
			msg = "no connectors registered — load connector YAML files and restart"
		case score < 0.4:
			msg = "low confidence — intent may be too vague or no connector describes this action well"
		}

		writeJSON(w, http.StatusOK, ResolveResult{
			OK:        connector != "",
			Connector: connector,
			Action:    action,
			Score:     score,
			Confident: score >= 0.6,
			Message:   msg,
		})
	}
}
