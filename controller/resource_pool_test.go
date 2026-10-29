package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsMaskedSecret(t *testing.T) {
	assert.True(t, isMaskedSecret("****abcd"))
	assert.True(t, isMaskedSecret("****"))
	assert.False(t, isMaskedSecret(""))
	assert.False(t, isMaskedSecret("plain"))
	assert.False(t, isMaskedSecret("abc"))
	// A short secret is never mistaken for a mask.
	assert.False(t, isMaskedSecret("***"))
}

func TestIsValidResourcePoolProvider(t *testing.T) {
	for _, provider := range []string{"claude", "gpt", "gemini", "grok"} {
		assert.True(t, isValidResourcePoolProvider(provider), provider)
	}
	for _, provider := range []string{"", "openai", "Claude", "anthropic"} {
		assert.False(t, isValidResourcePoolProvider(provider), provider)
	}
}
