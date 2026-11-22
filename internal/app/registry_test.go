package app

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistryIncludesGoAndProcessCollectors(t *testing.T) {
	reg := newRegistry()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	assert.Contains(t, names, "go_gc_duration_seconds")
	// The process collector is a no-op outside linux and windows.
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		assert.Contains(t, names, "process_start_time_seconds")
	}
}
