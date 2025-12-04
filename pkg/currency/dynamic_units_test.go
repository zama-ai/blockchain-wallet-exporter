package currency

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnsureUnitPair(t *testing.T) {
	registry := NewDefaultRegistry()

	err := EnsureUnitPair(registry, "zamawei", "zama", 18)
	assert.NoError(t, err)

	base, err := registry.Get("zamawei")
	assert.NoError(t, err)
	display, err := registry.Get("zama")
	assert.NoError(t, err)

	assert.InDelta(t, 1e-18, base.ConversionTo["zama"], 1e-30)
	assert.InDelta(t, 1e18, display.ConversionTo["zamawei"], 1e6)

	// Calling again should be idempotent
	assert.NoError(t, EnsureUnitPair(registry, "zamawei", "zama", 18))
}
