// Package health runs named dependency checks on an interval and reports when
// overall health flips. It exposes an HTTP handler suitable for liveness or
// readiness probes.
package health

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Check reports whether a dependency is reachable.
type Check func(ctx context.Context) error

// Monitor periodically runs its checks and tracks which ones are failing.
type Monitor struct {
	checks   map[string]Check
	interval time.Duration
	logger   *zap.Logger

	mu        sync.RWMutex
	healthy   bool
	firstDone bool
	failing   map[string]error
}

func NewMonitor(checks map[string]Check, interval time.Duration, logger *zap.Logger) *Monitor {
	return &Monitor{
		checks:   checks,
		interval: interval,
		logger:   logger,
		failing:  map[string]error{},
	}
}

// Run checks immediately, then every interval until ctx is done. onChange is
// called with the first result and again whenever overall health flips.
func (m *Monitor) Run(ctx context.Context, onChange func(healthy bool)) {
	m.runOnce(ctx)
	last := m.Healthy()
	onChange(last)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runOnce(ctx)
			if current := m.Healthy(); current != last {
				last = current
				onChange(current)
			}
		}
	}
}

func (m *Monitor) runOnce(ctx context.Context) {
	var (
		wg       sync.WaitGroup
		resultMu sync.Mutex
		failing  = map[string]error{}
	)
	for name, check := range m.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, m.interval)
			defer cancel()
			if err := check(checkCtx); err != nil {
				resultMu.Lock()
				failing[name] = err
				resultMu.Unlock()
			}
		}()
	}
	wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()
	for name, err := range failing {
		if _, was := m.failing[name]; !was {
			m.logger.Error("health check failing", zap.String("check", name), zap.Error(err))
		}
	}
	for name := range m.failing {
		if _, still := failing[name]; !still {
			m.logger.Info("health check recovered", zap.String("check", name))
		}
	}
	m.failing = failing
	m.healthy = len(failing) == 0
	m.firstDone = true
}

// Healthy is false until the first pass completes and while any check fails.
func (m *Monitor) Healthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthy && m.firstDone
}

// Handler serves 200 "ok" when healthy, otherwise 503 naming the failing checks.
func (m *Monitor) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m.mu.RLock()
		firstDone := m.firstDone
		names := make([]string, 0, len(m.failing))
		for name := range m.failing {
			names = append(names, name)
		}
		m.mu.RUnlock()

		switch {
		case !firstDone:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
		case len(names) == 0:
			_, _ = w.Write([]byte("ok"))
		default:
			sort.Strings(names)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(strings.Join(names, "\n")))
		}
	})
}
