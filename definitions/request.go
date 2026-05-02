package definitions

// CallRequest is the unified inbound request shape for a single connector action.
type CallRequest struct {
	Connector string                 `json:"connector"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	Options   CallOptions            `json:"options"`
}

// CallOptions carries per-request overrides.
type CallOptions struct {
	Cache          bool `json:"cache"`
	CacheTTLSeconds int  `json:"cache_ttl_seconds"`
	TimeoutMS      int  `json:"timeout_ms"`
}

// PipelineRequest is the unified inbound request shape for a DAG pipeline.
type PipelineRequest struct {
	Pipeline []PipelineStep         `json:"pipeline"`
	Input    map[string]interface{} `json:"input"`
}

// PipelineStep describes a single step in a pipeline.
type PipelineStep struct {
	ID        string                 `json:"id"`
	Connector string                 `json:"connector"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	DependsOn []string               `json:"depends_on"`
}
