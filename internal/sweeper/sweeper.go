// Package sweeper re-publishes notifications stuck in pending.
package sweeper

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

type Store interface {
	ClaimStalePending(ctx context.Context, staleAfter time.Duration, limit int) ([]repository.StaleNotification, error)
	MarkQueued(ctx context.Context, id int64) error
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
}

type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
}

type Sweeper struct {
	store     Store
	publisher Publisher
	cfg       config.SweepConfig
	metrics   *metrics.Server
	logger    *zap.Logger
}

func New(store Store, publisher Publisher, cfg config.SweepConfig, m *metrics.Server, logger *zap.Logger) *Sweeper {
	return &Sweeper{store: store, publisher: publisher, cfg: cfg, metrics: m, logger: logger}
}

// Run sweeps immediately, then on every cfg.Interval until ctx is done.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	s.SweepOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SweepOnce(ctx)
		}
	}
}

// SweepOnce claims stale pending notifications and republishes or fails each.
func (s *Sweeper) SweepOnce(ctx context.Context) {
	rows, err := s.store.ClaimStalePending(ctx, s.cfg.StaleAfter, s.cfg.BatchSize)
	if err != nil {
		s.logger.Error("failed to claim stale pending notifications", zap.Error(err))
		return
	}
	for _, row := range rows {
		s.handleRow(ctx, row)
	}
}

func (s *Sweeper) handleRow(ctx context.Context, row repository.StaleNotification) {
	if row.SweepAttempts > s.cfg.MaxAttempts {
		s.giveUp(ctx, row)
		return
	}
	s.republish(ctx, row)
}

func (s *Sweeper) giveUp(ctx context.Context, row repository.StaleNotification) {
	id := zap.Int64("notification_id", row.NotificationID)
	if err := s.store.UpdateNotificationStatus(ctx, row.NotificationID, notification.StatusFailed); err != nil {
		s.logger.Error("failed to mark notification failed", id, zap.Error(err))
		return
	}
	s.metrics.SweepFailures.Inc()
	s.logger.Warn("notification failed after max sweep attempts", id, zap.Int("attempts", row.SweepAttempts))
}

func (s *Sweeper) republish(ctx context.Context, row repository.StaleNotification) {
	id := zap.Int64("notification_id", row.NotificationID)
	body, err := json.Marshal(row.Message)
	if err != nil {
		s.logger.Warn("failed to marshal notification", id, zap.Error(err))
		s.metrics.SweepFailures.Inc()
		return
	}
	if err := s.publisher.Publish(ctx, rabbitmq.NotificationExchange, row.Type, body); err != nil {
		s.logger.Warn("failed to republish notification", id, zap.Error(err))
		s.metrics.SweepFailures.Inc()
		return
	}
	// MarkQueued failure is non-fatal: the row stays `pending` and the next
	// sweep pass republishes it. The consumer's IsSettled check absorbs the
	// duplicate so the notification is delivered once.
	if err := s.store.MarkQueued(ctx, row.NotificationID); err != nil {
		s.logger.Warn("republished but failed to mark queued", id, zap.Error(err))
	}
	s.metrics.SweepRepublished.Inc()
}
