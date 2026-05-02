package connectors

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "os"
        "regexp"
        "strings"
        "time"

        "github.com/nexus/core/definitions"
)

var templateRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Runner executes a single HTTP connector action.
type Runner struct {
        vault       *Vault
        transformer *Transformer
}

// NewRunner creates a Runner with the given vault and transformer.
func NewRunner(vault *Vault, transformer *Transformer) *Runner {
        return &Runner{vault: vault, transformer: transformer}
}

// RunResult holds the output of a connector execution.
type RunResult struct {
        Data      map[string]interface{}
        LatencyMS int64
        Cached    bool
}

// Execute performs the HTTP call for the given connector action.
func (r *Runner) Execute(ctx context.Context, def *definitions.ConnectorDef, actionName string, params map[string]interface{}) (*RunResult, error) {
        action, ok := def.Actions[actionName]
        if !ok {
                return nil, fmt.Errorf("runner: action %q not found in connector %s", actionName, def.Name)
        }

        // Validate required input fields before making any network call.
        if err := validateInputSchema(action, params); err != nil {
                return nil, err
        }

        timeout := time.Duration(def.TimeoutMS) * time.Millisecond
        if timeout == 0 {
                timeout = 10 * time.Second
        }
        ctx, cancel := context.WithTimeout(ctx, timeout)
        defer cancel()

        reqURL, err := r.buildURL(def, action, params)
        if err != nil {
                return nil, fmt.Errorf("runner: build URL: %w", err)
        }

        body, err := r.buildBody(action, params)
        if err != nil {
                return nil, fmt.Errorf("runner: build body: %w", err)
        }

        req, err := http.NewRequestWithContext(ctx, action.Method, reqURL, body)
        if err != nil {
                return nil, fmt.Errorf("runner: build request: %w", err)
        }

        r.injectAuth(req, def)
        r.injectHeaders(req, def)

        client := &http.Client{Timeout: timeout}
        start := time.Now()
        resp, err := client.Do(req)
        elapsed := time.Since(start).Milliseconds()
        if err != nil {
                return nil, fmt.Errorf("runner: http call to %s: %w", def.Name, err)
        }
        defer resp.Body.Close()

        if resp.StatusCode >= 400 {
                return nil, &ConnectorError{
                        Code:           "UPSTREAM_ERROR",
                        Message:        fmt.Sprintf("%s returned status %d", def.Name, resp.StatusCode),
                        UpstreamStatus: resp.StatusCode,
                }
        }

        raw, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, fmt.Errorf("runner: read response: %w", err)
        }

        // Decode into a generic value first — upstream may return an array or an object.
        var upstreamRaw interface{}
        if err := json.Unmarshal(raw, &upstreamRaw); err != nil {
                return nil, fmt.Errorf("runner: decode response from %s: %w", def.Name, err)
        }

        // Normalise to map[string]interface{} — wrapping arrays under "results".
        upstream := normaliseToMap(upstreamRaw)

        // Unwrap response_root if configured (e.g. "data" or "data.results").
        if action.ResponseRoot != "" {
                upstream = unwrapRoot(upstream, action.ResponseRoot)
        }

        transformed := r.transformer.Apply(upstream, action, def.KeepNulls)
        return &RunResult{Data: transformed, LatencyMS: elapsed}, nil
}

// normaliseToMap converts any JSON value into a map[string]interface{}.
// Arrays are wrapped as {"results": [...]}.
// Scalars are wrapped as {"value": scalar}.
func normaliseToMap(v interface{}) map[string]interface{} {
        switch val := v.(type) {
        case map[string]interface{}:
                return val
        case []interface{}:
                return map[string]interface{}{"results": val}
        default:
                return map[string]interface{}{"value": val}
        }
}

// unwrapRoot navigates dot-separated keys and returns the value as a map.
// If the target value is an array it is wrapped under "results".
// If any key is missing or the path is wrong, the original map is returned.
func unwrapRoot(m map[string]interface{}, root string) map[string]interface{} {
        parts := strings.SplitN(root, ".", 2)
        val, ok := m[parts[0]]
        if !ok {
                return m
        }
        if len(parts) == 2 {
                // Recurse one level deeper.
                if nested, ok := val.(map[string]interface{}); ok {
                        return unwrapRoot(nested, parts[1])
                }
                return m
        }
        return normaliseToMap(val)
}

func (r *Runner) buildURL(def *definitions.ConnectorDef, action definitions.Action, params map[string]interface{}) (string, error) {
        path := resolveTemplate(action.Path, params, nil)
        base := strings.TrimRight(def.BaseURL, "/")
        full := base + path

        if len(action.QueryParams) > 0 {
                q := url.Values{}
                for k, tmpl := range action.QueryParams {
                        resolved := resolveTemplate(tmpl, params, nil)
                        // Skip unresolved template placeholders — they were optional.
                        if strings.HasPrefix(resolved, "{{") && strings.HasSuffix(resolved, "}}") {
                                continue
                        }
                        q.Set(k, resolved)
                }
                if encoded := q.Encode(); encoded != "" {
                        full += "?" + encoded
                }
        }
        return full, nil
}

func (r *Runner) buildBody(action definitions.Action, params map[string]interface{}) (io.Reader, error) {
        if action.Method == http.MethodGet || action.Method == http.MethodHead {
                return nil, nil
        }
        if len(action.Body) > 0 {
                m := make(map[string]interface{})
                for k, tmpl := range action.Body {
                        m[k] = resolveTemplate(tmpl, params, nil)
                }
                b, err := json.Marshal(m)
                if err != nil {
                        return nil, err
                }
                return bytes.NewReader(b), nil
        }
        if len(params) > 0 {
                b, err := json.Marshal(params)
                if err != nil {
                        return nil, err
                }
                return bytes.NewReader(b), nil
        }
        return nil, nil
}

func (r *Runner) injectAuth(req *http.Request, def *definitions.ConnectorDef) {
        if def.Auth.Type == "none" || def.Auth.Env == "" {
                return
        }
        cred, ok := r.vault.Get(def.Name, def.Auth.Env)
        if !ok {
                cred = os.Getenv(def.Auth.Env)
        }
        if cred == "" {
                return
        }
        switch def.Auth.Type {
        case "bearer":
                req.Header.Set("Authorization", "Bearer "+cred)
        case "api_key":
                header := def.Auth.Header
                if header == "" {
                        header = "X-API-Key"
                }
                req.Header.Set(header, cred)
        case "basic":
                req.SetBasicAuth(def.Name, cred)
        }
}

func (r *Runner) injectHeaders(req *http.Request, def *definitions.ConnectorDef) {
        for k, v := range def.Headers {
                req.Header.Set(k, v)
        }
        if req.Header.Get("Content-Type") == "" && req.Body != nil {
                req.Header.Set("Content-Type", "application/json")
        }
        // Always send a descriptive User-Agent.
        if req.Header.Get("User-Agent") == "" {
                req.Header.Set("User-Agent", "Nexus/1.0 (+https://github.com/nexus/core)")
        }
}

// resolveTemplate replaces {{params.x}}, {{input.x}}, {{stepN.data.x}} in a template string.
func resolveTemplate(tmpl string, params map[string]interface{}, ctx map[string]interface{}) string {
        return templateRe.ReplaceAllStringFunc(tmpl, func(match string) string {
                inner := strings.Trim(match, "{}")
                inner = strings.TrimSpace(inner)
                parts := strings.SplitN(inner, ".", 2)
                if len(parts) < 2 {
                        return match
                }
                switch parts[0] {
                case "params":
                        if v, ok := params[parts[1]]; ok {
                                return fmt.Sprintf("%v", v)
                        }
                default:
                        // Step result or input from pipeline context.
                        if ctx != nil {
                                if v, ok := ctx[inner]; ok {
                                        return fmt.Sprintf("%v", v)
                                }
                        }
                }
                return match
        })
}

// validateInputSchema returns an error if any required params are missing.
func validateInputSchema(action definitions.Action, params map[string]interface{}) error {
        for field, schema := range action.InputSchema {
                if !schema.Required {
                        continue
                }
                v, exists := params[field]
                if !exists || v == nil {
                        return &ConnectorError{
                                Code:    "MISSING_REQUIRED_PARAM",
                                Message: fmt.Sprintf("required parameter %q is missing", field),
                        }
                }
        }
        return nil
}

// ConnectorError wraps an upstream HTTP error with a structured code.
type ConnectorError struct {
        Code           string
        Message        string
        UpstreamStatus int
}

func (e *ConnectorError) Error() string { return e.Message }
