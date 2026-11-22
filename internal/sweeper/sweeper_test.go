package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

type statusUpdate struct {
	id     int64
	status string
}

type fakeStore struct {
	mu                sync.Mutex
	claimErr          error
	claimRows         []repository.StaleNotification
	updateErr         error
	markErr           error
	statuses          map[int64]string
	markedIDs         []int64
	updateCalls       []statusUpdate
	claimCount        atomic.Int32
	claimedStaleAfter time.Duration
	claimedLimit      int
}

func newFakeStore(rows ...repository.StaleNotification) *fakeStore {
	return &fakeStore{claimRows: rows, statuses: map[int64]string{}}
}

func (f *fakeStore) ClaimStalePending(_ context.Context, staleAfter time.Duration, limit int) ([]repository.StaleNotification, error) {
	f.claimCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimedStaleAfter = staleAfter
	f.claimedLimit = limit
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return f.claimRows, nil
}

func (f *fakeStore) MarkQueued(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	f.markedIDs = append(f.markedIDs, id)
	f.statuses[id] = notification.StatusQueued
	return nil
}

func (f *fakeStore) UpdateNotificationStatus(_ context.Context, id int64, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls = append(f.updateCalls, statusUpdate{id, status})
	if f.updateErr != nil {
		return f.updateErr
	}
	f.statuses[id] = status
	return nil
}

type publishedMessage struct {
	routingKey string
	body       []byte
}

type fakePublisher struct {
	err           error
	failUntilCall int
	calls         int
	published     []publishedMessage
}

func (p *fakePublisher) Publish(_ context.Context, exchange, routingKey string, body []byte) error {
	p.calls++
	if p.err != nil {
		return p.err
	}
	if p.calls <= p.failUntilCall {
		return errors.New("transient publish failure")
	}
	if exchange != rabbitmq.NotificationExchange {
		return errors.New("unexpected exchange " + exchange)
	}
	p.published = append(p.published, publishedMessage{routingKey, body})
	return nil
}

func staleRow(id int64, attempts int) repository.StaleNotification {
	return repository.StaleNotification{
		Message: notification.Message{
			NotificationID: id,
			UserID:         "u1",
			Title:          "t",
			Priority:       "high",
			Message:        "hi",
			Type:           notification.TypePush,
		},
		SweepAttempts: attempts,
	}
}

func testConfig() config.SweepConfig {
	return config.SweepConfig{
		Interval:    time.Hour,
		StaleAfter:  time.Minute,
		BatchSize:   10,
		MaxAttempts: 3,
	}
}

func newSweeper(t *testing.T, store Store, pub Publisher, cfg config.SweepConfig) (*Sweeper, *metrics.Server) {
	t.Helper()
	m := metrics.NewServer(prometheus.NewRegistry())
	return New(store, pub, cfg, m, zaptest.NewLogger(t)), m
}

func TestSweepOnceRepublishesStaleNotification(t *testing.T) {
	store := newFakeStore(staleRow(7, 1))
	pub := &fakePublisher{}
	s, m := newSweeper(t, store, pub, testConfig())

	s.SweepOnce(context.Background())

	require.Len(t, pub.published, 1)
	assert.Equal(t, "push", pub.published[0].routingKey)
	var got notification.Message
	require.NoError(t, json.Unmarshal(pub.published[0].body, &got))
	assert.Equal(t, int64(7), got.NotificationID)
	assert.Equal(t, "queued", store.statuses[7])
	assert.Equal(t, float64(1), testutil.ToFloat64(m.SweepRepublished))
	assert.Equal(t, float64(0), testutil.ToFloat64(m.SweepFailures))
}

func TestSweepOnceLeavesNotificationPendingWhenPublishFails(t *testing.T) {
	store := newFakeStore(staleRow(7, 1))
	pub := &fakePublisher{err: errors.New("broker down")}
	s, m := newSweeper(t, store, pub, testConfig())

	s.SweepOnce(context.Background())

	assert.Empty(t, pub.published)
	assert.Empty(t, store.statuses[7])
	assert.Empty(t, store.markedIDs)
	assert.Equal(t, float64(1), testutil.ToFloat64(m.SweepFailures))
	assert.Equal(t, float64(0), testutil.ToFloat64(m.SweepRepublished))
}

func TestSweepOnceFailsNotificationAfterMaxAttempts(t *testing.T) {
	store := newFakeStore(staleRow(7, 4))
	pub := &fakePublisher{}
	s, m := newSweeper(t, store, pub, testConfig())

	s.SweepOnce(context.Background())

	assert.Empty(t, pub.published)
	assert.Equal(t, "failed", store.statuses[7])
	assert.Equal(t, float64(1), testutil.ToFloat64(m.SweepFailures))
	assert.Equal(t, float64(0), testutil.ToFloat64(m.SweepRepublished))
}

func TestSweepOnceContinuesAfterOneFailure(t *testing.T) {
	store := newFakeStore(staleRow(1, 1), staleRow(2, 1))
	pub := &fakePublisher{failUntilCall: 1}
	s, m := newSweeper(t, store, pub, testConfig())

	s.SweepOnce(context.Background())

	assert.Empty(t, store.statuses[1])
	assert.Equal(t, "queued", store.statuses[2])
	assert.Equal(t, float64(1), testutil.ToFloat64(m.SweepRepublished))
	assert.Equal(t, float64(1), testutil.ToFloat64(m.SweepFailures))
}

func TestSweepOnceReportsClaimFailure(t *testing.T) {
	store := newFakeStore()
	store.claimErr = errors.New("db gone")
	pub := &fakePublisher{}
	core, logs := observer.New(zap.ErrorLevel)
	m := metrics.NewServer(prometheus.NewRegistry())
	s := New(store, pub, testConfig(), m, zap.New(core))

	s.SweepOnce(context.Background())

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, zap.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].ContextMap()["error"], "db gone")
	assert.Empty(t, pub.published)
	assert.Equal(t, float64(0), testutil.ToFloat64(m.SweepFailures))
	assert.Equal(t, float64(0), testutil.ToFloat64(m.SweepRepublished))
}

func TestSweepOncePassesConfiguredAgeAndBatch(t *testing.T) {
	store := newFakeStore()
	cfg := testConfig()
	cfg.StaleAfter = 5 * time.Minute
	cfg.BatchSize = 100
	s, _ := newSweeper(t, store, &fakePublisher{}, cfg)

	s.SweepOnce(context.Background())

	assert.Equal(t, 5*time.Minute, store.claimedStaleAfter)
	assert.Equal(t, 100, store.claimedLimit)
}

func TestRunSweepsRepeatedlyUntilCancelled(t *testing.T) {
	store := newFakeStore()
	cfg := testConfig()
	cfg.Interval = 5 * time.Millisecond
	s, _ := newSweeper(t, store, &fakePublisher{}, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	assert.Eventually(t, func() bool { return store.claimCount.Load() >= 2 }, time.Second, 5*time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
