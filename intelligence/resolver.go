package intelligence

import (
	"github.com/nexus/core/definitions"
)

// Resolver picks the best connector and action for a given intent string.
type Resolver struct {
	mapper *Mapper
}

// NewResolver creates a Resolver backed by a Mapper.
func NewResolver(mapper *Mapper) *Resolver {
	return &Resolver{mapper: mapper}
}

// Resolve returns the best-matching connector name and action for an intent,
// searching across all registered connectors and their action descriptions.
func (r *Resolver) Resolve(intent string, registry map[string]*definitions.ConnectorDef) (connector, action string, score float64) {
	intentVec := computeVector(intent)

	for cName, def := range registry {
		for aName, act := range def.Actions {
			descVec := computeVector(act.Description)
			sim := cosineSimilarity(intentVec, descVec)
			if sim > score {
				score = sim
				connector = cName
				action = aName
			}
		}
	}
	return
}
