package definitions

// ConnectorDef is the parsed form of a connector YAML file.
type ConnectorDef struct {
	Name        string             `yaml:"name"              json:"name"`
	Version     string             `yaml:"version"           json:"version"`
	Description string             `yaml:"description"       json:"description"`
	Protocol    string             `yaml:"protocol"          json:"protocol"`
	BaseURL     string             `yaml:"base_url"          json:"base_url"`
	Auth        AuthDef            `yaml:"auth"              json:"auth"`
	RateLimit   RateLimitDef       `yaml:"rate_limit"        json:"rate_limit"`
	TimeoutMS   int                `yaml:"timeout_ms"        json:"timeout_ms"`
	Headers     map[string]string  `yaml:"headers"           json:"headers"`
	Actions     map[string]Action  `yaml:"actions"           json:"actions"`
	KeepNulls   bool               `yaml:"keep_nulls"        json:"keep_nulls"`
}

// AuthDef describes authentication configuration for a connector.
type AuthDef struct {
	Type   string `yaml:"type"   json:"type"`
	Env    string `yaml:"env"    json:"env"`
	Header string `yaml:"header" json:"header"`
	Prefix string `yaml:"prefix" json:"prefix"`
}

// RateLimitDef configures the token bucket for a connector.
type RateLimitDef struct {
	RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int     `yaml:"burst"               json:"burst"`
}

// Action describes a single callable action within a connector.
type Action struct {
	Method       string                 `yaml:"method"             json:"method"`
	Path         string                 `yaml:"path"               json:"path"`
	Description  string                 `yaml:"description"        json:"description"`
	InputSchema  map[string]FieldSchema `yaml:"input_schema"       json:"input_schema"`
	OutputMap    map[string]string      `yaml:"output_map"         json:"output_map"`
	QueryParams  map[string]string      `yaml:"query_params"       json:"query_params"`
	Body         map[string]string      `yaml:"body"               json:"body"`
	ResponseRoot string                 `yaml:"response_root"      json:"response_root"`
	Cache        bool                   `yaml:"cache"              json:"cache"`
	CacheTTL     int                    `yaml:"cache_ttl_seconds"  json:"cache_ttl_seconds"`
	GRPCService  string                 `yaml:"grpc_service"       json:"grpc_service"`
	GRPCMethod   string                 `yaml:"grpc_method"        json:"grpc_method"`
}

// FieldSchema describes a single input field.
type FieldSchema struct {
	Type     string `yaml:"type"     json:"type"`
	Required bool   `yaml:"required" json:"required"`
}
