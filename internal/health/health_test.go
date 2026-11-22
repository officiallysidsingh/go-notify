package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

func pass(context.Context) error { return nil }

func fail(context.Context) error { return errors.New("down") }

func serve(m *Monitor) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return rec
}

// changes collects onChange calls safely across goroutines.
type changes struct {
	mu   sync.Mutex
	vals []bool
}

func (c *changes) record(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals = append(c.vals, v)
}

func (c *changes) snapshot() []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.vals...)
}

func startRun(m *Monitor, onChange func(bool)) (cancel func()) {
	ctx, cancelCtx := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx, onChange)
	}()
	return func() {
		cancelCtx()
		<-done
	}
}

func TestMonitorIsUnhealthyBeforeFirstCheck(t *testing.T) {
	m := NewMonitor(map[string]Check{"redis": pass}, time.Minute, zaptest.NewLogger(t))

	assert.False(t, m.Healthy())
	rec := serve(m)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "not ready", rec.Body.String())
}

func TestMonitorReportsHealthyWhenEveryCheckPasses(t *testing.T) {
	m := NewMonitor(map[string]Check{"redis": pass, "postgres": pass}, 10*time.Millisecond, zaptest.NewLogger(t))
	first := make(chan bool, 1)
	cancel := startRun(m, func(h bool) {
		select {
		case first <- h:
		default:
		}
	})
	defer cancel()

	require.True(t, <-first)
	assert.True(t, m.Healthy())
	assert.Equal(t, http.StatusOK, serve(m).Code)
}

func TestMonitorReportsUnhealthyWhenAnyCheckFails(t *testing.T) {
	m := NewMonitor(map[string]Check{"redis": fail, "postgres": pass}, 10*time.Millisecond, zaptest.NewLogger(t))
	first := make(chan bool, 1)
	cancel := startRun(m, func(h bool) {
		select {
		case first <- h:
		default:
		}
	})
	defer cancel()

	require.False(t, <-first)
	assert.False(t, m.Healthy())
	rec := serve(m)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "redis")
}

func TestMonitorNotifiesOnlyOnChange(t *testing.T) {
	m := NewMonitor(map[string]Check{"redis": pass}, 5*time.Millisecond, zaptest.NewLogger(t))
	var got changes
	cancel := startRun(m, got.record)

	time.Sleep(50 * time.Millisecond)
	cancel()

	assert.Equal(t, []bool{true}, got.snapshot())
}

func TestMonitorRecovers(t *testing.T) {
	var mu sync.Mutex
	failing := true
	check := func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			return errors.New("down")
		}
		return nil
	}
	m := NewMonitor(map[string]Check{"redis": check}, 5*time.Millisecond, zaptest.NewLogger(t))
	var got changes
	cancel := startRun(m, got.record)
	defer cancel()

	require.Eventually(t, func() bool { return len(got.snapshot()) == 1 }, time.Second, time.Millisecond)
	mu.Lock()
	failing = false
	mu.Unlock()
	require.Eventually(t, func() bool { return len(got.snapshot()) == 2 }, time.Second, time.Millisecond)

	assert.Equal(t, []bool{false, true}, got.snapshot())
}

func TestMonitorTimesOutHangingCheck(t *testing.T) {
	hang := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	m := NewMonitor(map[string]Check{"redis": hang}, 20*time.Millisecond, zaptest.NewLogger(t))
	var got changes
	ctx, cancelCtx := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx, got.record)
	}()

	time.Sleep(60 * time.Millisecond)
	assert.False(t, m.Healthy())
	assert.Equal(t, []bool{false}, got.snapshot())

	cancelCtx()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestHandlerReturns503NamingFailedChecks(t *testing.T) {
	m := NewMonitor(map[string]Check{"redis": fail, "postgres": pass}, time.Minute, zaptest.NewLogger(t))
	m.runOnce(context.Background())

	rec := serve(m)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "redis")
	assert.NotContains(t, rec.Body.String(), "postgres")
}

func TestHandlerReturns200WhenHealthy(t *testing.T) {
	m := NewMonitor(map[string]Check{"redis": pass}, time.Minute, zaptest.NewLogger(t))
	m.runOnce(context.Background())

	rec := serve(m)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestMonitorLogsFailureOnce(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	m := NewMonitor(map[string]Check{"redis": fail}, time.Minute, zap.New(core))

	for range 3 {
		m.runOnce(context.Background())
	}

	entries := logs.FilterLevelExact(zapcore.ErrorLevel).FilterField(zap.String("check", "redis")).All()
	require.Len(t, entries, 1)
	assert.Equal(t, 0, logs.FilterLevelExact(zapcore.InfoLevel).Len())
}

func TestMonitorLogsRecoveryOnce(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	var mu sync.Mutex
	failing := true
	check := func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			return errors.New("down")
		}
		return nil
	}
	m := NewMonitor(map[string]Check{"redis": check}, time.Minute, zap.New(core))

	m.runOnce(context.Background())
	mu.Lock()
	failing = false
	mu.Unlock()
	m.runOnce(context.Background())
	m.runOnce(context.Background())

	assert.Equal(t, 1, logs.FilterLevelExact(zapcore.InfoLevel).FilterField(zap.String("check", "redis")).Len())
}
