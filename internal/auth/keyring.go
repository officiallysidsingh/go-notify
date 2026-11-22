// Package auth authenticates gRPC callers by API key.
package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
)

// MetadataKey is the gRPC metadata header carrying the caller's API key.
const MetadataKey = "x-api-key"

type entry struct {
	name, key string
}

// KeyRing maps API keys to caller names.
type KeyRing struct {
	entries []entry
}

// ParseKeys parses "name:key,name:key". The key is everything after the first
// colon. Errors identify the entry by name or position and never include a key.
func ParseKeys(raw string) (KeyRing, error) {
	if strings.TrimSpace(raw) == "" {
		return KeyRing{}, errors.New("auth.apiKeys: empty")
	}

	var ring KeyRing
	names := map[string]bool{}
	keys := map[string]bool{}
	for i, part := range strings.Split(raw, ",") {
		pos := i + 1
		part = strings.TrimSpace(part)
		rawName, key, ok := strings.Cut(part, ":")
		if !ok {
			return KeyRing{}, fmt.Errorf("auth.apiKeys: entry %d has no %q", pos, ":")
		}
		name := strings.TrimSpace(rawName)
		if name == "" {
			return KeyRing{}, fmt.Errorf("auth.apiKeys: entry %d has empty name", pos)
		}
		if key == "" {
			return KeyRing{}, fmt.Errorf("auth.apiKeys: entry %d (%q) has empty key", pos, name)
		}
		if names[name] {
			return KeyRing{}, fmt.Errorf("auth.apiKeys: duplicate name %q", name)
		}
		if keys[key] {
			return KeyRing{}, fmt.Errorf("auth.apiKeys: entry %q uses a duplicate key", name)
		}
		names[name] = true
		keys[key] = true
		ring.entries = append(ring.entries, entry{name: name, key: key})
	}
	return ring, nil
}

// Caller returns the name owning key. Every entry is compared in constant time
// with no early exit, so timing does not reveal which entry matched.
func (k KeyRing) Caller(key string) (name string, known bool) {
	for _, e := range k.entries {
		if subtle.ConstantTimeCompare([]byte(e.key), []byte(key)) == 1 {
			name, known = e.name, true
		}
	}
	return name, known
}
