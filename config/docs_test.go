package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// documentedKeys lists every key Load reads (dotted, as written in the YAML).
func documentedKeys() []string {
	return []string{
		"grpc.port", "grpc.reflection",
		"auth.apiKeys", "auth.disabled",
		"logging.level",
		"ntfy.baseURL", "ntfy.topic", "ntfy.token", "ntfy.timeout",
		"rabbitmq.url",
		"postgres.DataSourceName", "postgres.MaxOpenConns", "postgres.MaxIdleConns",
		"postgres.ConnMaxLifetime", "postgres.ConnMaxIdleTime", "postgres.ConnTimeout",
		"redis.addr", "redis.limit", "redis.window",
		"metrics.port",
		"worker.concurrency", "worker.sendAttempts", "worker.retryDelay", "worker.metricsPort",
		"sweep.interval", "sweep.staleAfter", "sweep.batchSize", "sweep.maxAttempts",
		"health.interval",
	}
}

func TestEveryConfigKeyIsDocumented(t *testing.T) {
	body, err := os.ReadFile("../docs/configuration.md")
	require.NoError(t, err)
	text := string(body)
	for _, key := range documentedKeys() {
		assert.Contains(t, text, "`"+key+"`", "key %s missing from docs/configuration.md", key)
	}
}

// enumerateConfigKeys derives the dotted viper keys from the Config struct.
// Sections are lowercased; field names are lower-camel except Postgres, whose
// keys keep their PascalCase spelling (matching the SetDefault calls).
func enumerateConfigKeys() []string {
	var keys []string
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		sec := t.Field(i)
		if !sec.IsExported() || sec.Type.Kind() != reflect.Struct {
			continue
		}
		name := strings.ToLower(sec.Name)
		st := sec.Type
		for j := 0; j < st.NumField(); j++ {
			f := st.Field(j).Name
			if name != "postgres" {
				f = lowerCamel(f)
			}
			keys = append(keys, name+"."+f)
		}
	}
	return keys
}

// lowerCamel lowercases the leading capital run: APIKeys -> apiKeys, BaseURL -> baseURL, Port -> port.
func lowerCamel(s string) string {
	n := 0
	for n < len(s) && s[n] >= 'A' && s[n] <= 'Z' {
		n++
	}
	switch {
	case n == 0:
		return s
	case n == 1 || n == len(s):
		return strings.ToLower(s[:n]) + s[n:]
	default:
		return strings.ToLower(s[:n-1]) + s[n-1:]
	}
}

func TestDocumentedKeysMatchesConfigFields(t *testing.T) {
	assert.ElementsMatch(t, enumerateConfigKeys(), documentedKeys(),
		"documentedKeys list is out of sync with the Config struct")
}

func TestExampleConfigListsEveryKey(t *testing.T) {
	body, err := os.ReadFile("config.example.yaml")
	require.NoError(t, err)
	var y map[string]any
	require.NoError(t, yaml.Unmarshal(body, &y))

	for _, key := range documentedKeys() {
		cur := any(y)
		ok := true
		for _, p := range strings.Split(key, ".") {
			m, isMap := cur.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			found := false
			for k, v := range m {
				if strings.EqualFold(k, p) {
					cur, found = v, true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		assert.True(t, ok, "key %s missing from config.example.yaml", key)
	}
}
