package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeysAcceptsMultipleCallers(t *testing.T) {
	ring, err := ParseKeys("billing:k1, orders:k2")
	require.NoError(t, err)

	name, known := ring.Caller("k2")
	assert.True(t, known)
	assert.Equal(t, "orders", name)
}

func TestParseKeysKeepsColonsInsideKey(t *testing.T) {
	ring, err := ParseKeys("billing:a:b")
	require.NoError(t, err)

	name, known := ring.Caller("a:b")
	assert.True(t, known)
	assert.Equal(t, "billing", name)
}

func TestParseKeysRejects(t *testing.T) {
	tests := []struct {
		name, raw, wantInErr string
	}{
		{"empty", "", "empty"},
		{"no colon", "billing", "entry 1"},
		{"empty name", ":k", "entry 1"},
		{"empty key", "billing:", "billing"},
		{"duplicate name", "a:k,a:j", `"a"`},
		{"duplicate key", "a:k,b:k", `"b"`},
		{"empty entry", "a:k,,b:j", "entry 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseKeys(tt.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantInErr)
		})
	}
}

func TestParseKeysErrorDoesNotRevealKey(t *testing.T) {
	_, err := ParseKeys("a:secret,a:other")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")

	_, err = ParseKeys("a:secret,b:secret")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
}

func TestCallerRejectsUnknownKey(t *testing.T) {
	ring, err := ParseKeys("a:k")
	require.NoError(t, err)

	name, known := ring.Caller("x")
	assert.False(t, known)
	assert.Empty(t, name)
}
