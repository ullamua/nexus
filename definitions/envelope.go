package definitions

// ResponseEnvelope is the unified response shape for single connector calls.
type ResponseEnvelope struct {
	OK               bool                   `json:"ok"`
	Connector        string                 `json:"connector,omitempty"`
	Action           string                 `json:"action,omitempty"`
	Data             interface{}            `json:"data,omitempty"`
	Error            *ErrorDetail           `json:"error,omitempty"`
	Meta             ResponseMeta           `json:"meta"`
}

// PipelineEnvelope is the unified response shape for pipeline execution.
type PipelineEnvelope struct {
	OK         bool                      `json:"ok"`
	PipelineID string                    `json:"pipeline_id"`
	Steps      map[string]StepResult     `json:"steps"`
	Result     interface{}               `json:"result,omitempty"`
	Error      *ErrorDetail              `json:"error,omitempty"`
	Meta       PipelineMeta              `json:"meta"`
}

// StepResult holds the outcome of a single pipeline step.
type StepResult struct {
	OK        bool         `json:"ok"`
	Data      interface{}  `json:"data,omitempty"`
	Error     *ErrorDetail `json:"error,omitempty"`
	LatencyMS int64        `json:"latency_ms"`
	Status    string       `json:"status"` // completed | failed | cancelled | skipped
}

// ErrorDetail describes a structured error.
type ErrorDetail struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Step           string `json:"step,omitempty"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
}

// ResponseMeta contains request metadata included in every response.
type ResponseMeta struct {
	RequestID        string `json:"request_id"`
	LatencyMS        int64  `json:"latency_ms"`
	Cached           bool   `json:"cached"`
	ConnectorVersion string `json:"connector_version,omitempty"`
	Timestamp        string `json:"timestamp"`
}

// PipelineMeta contains metadata for pipeline responses.
type PipelineMeta struct {
	RequestID      string `json:"request_id"`
	TotalLatencyMS int64  `json:"total_latency_ms"`
	ParallelSteps  int    `json:"parallel_steps"`
	Timestamp      string `json:"timestamp"`
}
