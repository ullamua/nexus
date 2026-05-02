package connectors

import (
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "sync"

        "github.com/nexus/core/definitions"
        "github.com/rs/zerolog"
        "gopkg.in/yaml.v3"
)

// Registry holds all loaded connector definitions indexed by name.
type Registry struct {
        mu         sync.RWMutex
        connectors map[string]*definitions.ConnectorDef
        log        zerolog.Logger
}

// NewRegistry creates an empty registry.
func NewRegistry(log zerolog.Logger) *Registry {
        return &Registry{
                connectors: make(map[string]*definitions.ConnectorDef),
                log:        log,
        }
}

// LoadDir reads all YAML files from dir and registers them.
func (r *Registry) LoadDir(dir string) error {
        entries, err := os.ReadDir(dir)
        if err != nil {
                return fmt.Errorf("registry: read dir %s: %w", dir, err)
        }

        loaded := 0
        for _, e := range entries {
                if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
                        continue
                }
                if strings.HasPrefix(e.Name(), "_") {
                        continue
                }
                path := filepath.Join(dir, e.Name())
                if err := r.loadFile(path); err != nil {
                        r.log.Warn().Err(err).Str("file", path).Msg("skipping connector file")
                        continue
                }
                loaded++
        }

        r.log.Info().Int("count", loaded).Str("dir", dir).Msg("connector registry loaded")
        return nil
}

func (r *Registry) loadFile(path string) error {
        data, err := os.ReadFile(path)
        if err != nil {
                return fmt.Errorf("registry: load connector file %s: %w", path, err)
        }

        var def definitions.ConnectorDef
        if err := yaml.Unmarshal(data, &def); err != nil {
                return fmt.Errorf("registry: parse %s: %w", path, err)
        }
        if def.Name == "" {
                return fmt.Errorf("registry: connector in %s has no name", path)
        }

        r.mu.Lock()
        r.connectors[def.Name] = &def
        r.mu.Unlock()

        r.log.Debug().Str("connector", def.Name).Str("version", def.Version).Msg("connector registered")
        return nil
}

// Get returns a connector definition by name.
func (r *Registry) Get(name string) (*definitions.ConnectorDef, bool) {
        r.mu.RLock()
        defer r.mu.RUnlock()
        c, ok := r.connectors[name]
        return c, ok
}

// All returns a copy of all registered connectors.
func (r *Registry) All() map[string]*definitions.ConnectorDef {
        r.mu.RLock()
        defer r.mu.RUnlock()
        out := make(map[string]*definitions.ConnectorDef, len(r.connectors))
        for k, v := range r.connectors {
                out[k] = v
        }
        return out
}

// Names returns connector names in the registry.
func (r *Registry) Names() []string {
        r.mu.RLock()
        defer r.mu.RUnlock()
        names := make([]string, 0, len(r.connectors))
        for k := range r.connectors {
                names = append(names, k)
        }
        return names
}

// Reload clears all registered connectors and reloads from dir.
// It is safe to call while the server is running.
func (r *Registry) Reload(dir string) error {
        r.mu.Lock()
        r.connectors = make(map[string]*definitions.ConnectorDef)
        r.mu.Unlock()
        return r.LoadDir(dir)
}

// RegisterForTest adds a connector definition directly — for use in tests only.
func (r *Registry) RegisterForTest(def *definitions.ConnectorDef) {
        r.mu.Lock()
        defer r.mu.Unlock()
        r.connectors[def.Name] = def
}
