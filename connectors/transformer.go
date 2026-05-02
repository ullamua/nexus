package connectors

import (
	"github.com/nexus/core/definitions"
	"github.com/nexus/core/intelligence"
	"github.com/rs/zerolog"
)

// Transformer applies output_map renaming and semantic auto-mapping to upstream responses.
type Transformer struct {
	mapper *intelligence.Mapper
	log    zerolog.Logger
}

// NewTransformer creates a Transformer backed by the given semantic mapper.
func NewTransformer(mapper *intelligence.Mapper, log zerolog.Logger) *Transformer {
	return &Transformer{mapper: mapper, log: log}
}

// Apply renames upstream response fields according to the connector action's output_map,
// then auto-maps any remaining unknown fields using semantic similarity.
func (t *Transformer) Apply(data map[string]interface{}, action definitions.Action, keepNulls bool) map[string]interface{} {
	out := make(map[string]interface{}, len(data))

	for upstreamKey, val := range data {
		if !keepNulls && isNullOrEmpty(val) {
			continue
		}

		// Check explicit output_map first.
		if normalized, ok := action.OutputMap[upstreamKey]; ok {
			out[normalized] = val
			continue
		}

		// Fall back to semantic auto-mapping.
		mapped, score := t.mapper.Map(upstreamKey)
		if mapped != upstreamKey {
			t.log.Debug().
				Str("original", upstreamKey).
				Str("mapped", mapped).
				Float64("score", score).
				Msg("semantic auto-map applied")
		}
		out[mapped] = val
	}

	return out
}

// ApplyNested recursively transforms nested maps.
func (t *Transformer) ApplyNested(data interface{}, action definitions.Action, keepNulls bool) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		return t.Apply(v, action, keepNulls)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = t.ApplyNested(item, action, keepNulls)
		}
		return out
	default:
		return data
	}
}

func isNullOrEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case []interface{}:
		return len(val) == 0
	case map[string]interface{}:
		return len(val) == 0
	}
	return false
}
