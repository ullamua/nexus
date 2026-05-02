package intelligence

import (
	"math"
	"strings"
	"sync"
)

const autoMapThreshold = 0.85

// Mapper performs semantic field name mapping using cosine similarity
// on character n-gram embeddings.
type Mapper struct {
	once     sync.Once
	table    map[string][]float64 // canonical field → unit vector
}

// NewMapper creates a Mapper and precomputes vectors for all canonical fields.
func NewMapper() *Mapper {
	m := &Mapper{}
	m.init()
	return m
}

func (m *Mapper) init() {
	m.once.Do(func() {
		m.table = make(map[string][]float64, len(CanonicalFields))
		for _, f := range CanonicalFields {
			m.table[f] = computeVector(f)
		}
	})
}

// Map returns the best canonical field name for an unknown field, or the
// original name if no match exceeds the similarity threshold.
func (m *Mapper) Map(field string) (string, float64) {
	m.init()
	vec := computeVector(field)
	bestName := field
	bestSim := 0.0

	for canonical, cv := range m.table {
		sim := cosineSimilarity(vec, cv)
		if sim > bestSim {
			bestSim = sim
			bestName = canonical
		}
	}

	if bestSim < autoMapThreshold {
		return field, bestSim
	}
	return bestName, bestSim
}

// computeVector builds a 128-dim unit vector from character n-grams.
// Dims 0–63: bigram hash buckets. Dims 64–127: trigram hash buckets.
func computeVector(field string) []float64 {
	norm := normalizeFieldName(field)
	vec := make([]float64, VectorDim)

	for i := 0; i < len(norm)-1; i++ {
		h := bigramHash(norm[i], norm[i+1]) % 64
		vec[h]++
	}
	for i := 0; i < len(norm)-2; i++ {
		h := trigramHash(norm[i], norm[i+1], norm[i+2])%64 + 64
		vec[h]++
	}

	return unitNormalize(vec)
}

func normalizeFieldName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func bigramHash(a, b byte) int {
	return int(a)*31 + int(b)
}

func trigramHash(a, b, c byte) int {
	return int(a)*31*31 + int(b)*31 + int(c)
}

func unitNormalize(vec []float64) []float64 {
	mag := 0.0
	for _, v := range vec {
		mag += v * v
	}
	if mag == 0 {
		return vec
	}
	mag = math.Sqrt(mag)
	out := make([]float64, len(vec))
	for i, v := range vec {
		out[i] = v / mag
	}
	return out
}

func cosineSimilarity(a, b []float64) float64 {
	dot := 0.0
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot // both vectors are already unit-normalized
}
