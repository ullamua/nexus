package tests

import (
	"testing"

	"github.com/nexus/core/connectors"
	"github.com/nexus/core/definitions"
	"github.com/nexus/core/intelligence"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func newTestTransformer() *connectors.Transformer {
	mapper := intelligence.NewMapper()
	return connectors.NewTransformer(mapper, zerolog.Nop())
}

func TestTransformerOutputMapRenamesFields(t *testing.T) {
	tr := newTestTransformer()
	action := definitions.Action{
		OutputMap: map[string]string{
			"id":    "customer_id",
			"email": "email",
		},
	}
	data := map[string]interface{}{
		"id":    "cus_123",
		"email": "user@example.com",
	}

	result := tr.Apply(data, action, true)

	assert.Equal(t, "cus_123", result["customer_id"])
	assert.Equal(t, "user@example.com", result["email"])
	_, hasOldID := result["id"]
	assert.False(t, hasOldID, "original 'id' key should be renamed")
}

func TestTransformerSemanticAutoMapAboveThreshold(t *testing.T) {
	tr := newTestTransformer()
	action := definitions.Action{OutputMap: map[string]string{}}

	// "userid" normalizes identically to "user_id" — guaranteed similarity 1.0.
	data := map[string]interface{}{
		"userid": "u_abc",
	}
	result := tr.Apply(data, action, true)

	// Should be auto-mapped to "user_id" (exact normalized form match).
	_, hasUserID := result["user_id"]
	assert.True(t, hasUserID, "userid should auto-map to user_id via semantic similarity")
}

func TestTransformerSemanticAutoMapBelowThreshold(t *testing.T) {
	tr := newTestTransformer()
	action := definitions.Action{OutputMap: map[string]string{}}

	// A completely random field name with no similarity to known fields.
	data := map[string]interface{}{
		"zxqwjkl_random_98765": "value",
	}
	result := tr.Apply(data, action, true)

	// Field should remain unchanged.
	_, hasOriginal := result["zxqwjkl_random_98765"]
	assert.True(t, hasOriginal, "unrecognized field should keep its original name")
}

func TestTransformerNullStripping(t *testing.T) {
	tr := newTestTransformer()
	action := definitions.Action{OutputMap: map[string]string{}}

	data := map[string]interface{}{
		"name":    "Jane",
		"phone":   nil,
		"address": "",
	}

	result := tr.Apply(data, action, false) // keepNulls = false

	assert.Equal(t, "Jane", result["name"])
	_, hasPhone := result["phone"]
	assert.False(t, hasPhone, "nil field should be stripped")
	_, hasAddr := result["address"]
	assert.False(t, hasAddr, "empty string field should be stripped")
}

func TestTransformerNullRetained(t *testing.T) {
	tr := newTestTransformer()
	action := definitions.Action{OutputMap: map[string]string{}}

	data := map[string]interface{}{
		"name":  "Jane",
		"phone": nil,
	}

	result := tr.Apply(data, action, true) // keepNulls = true

	assert.Equal(t, "Jane", result["name"])
	_, hasPhone := result["phone"]
	assert.True(t, hasPhone, "nil field should be retained when keepNulls=true")
}
