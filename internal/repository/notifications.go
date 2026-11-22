package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/officiallysidsingh/go-notify/internal/notification"
)

// InsertNotification stores a pending notification and returns its generated ID.
func (d *DB) InsertNotification(
	ctx context.Context,
	message notification.Message,
) (int64, error) {
	query := `
		INSERT INTO notifications (user_id, title, type, priority, message, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	var id int64
	err := d.conn.QueryRowContext(
		ctx, query,
		message.UserID, message.Title, message.Type, message.Priority,
		message.Message, notification.StatusPending,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert notification: %w", err)
	}
	return id, nil
}

// MarkQueued moves a pending notification to queued. A notification that is
// already past pending is left alone and is not an error.
func (d *DB) MarkQueued(ctx context.Context, id int64) error {
	query := `
		UPDATE notifications
		SET status = 'queued', updated_at = now()
		WHERE id = $1 AND status = 'pending'`

	if _, err := d.conn.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("failed to mark queued: %w", err)
	}
	return nil
}

func (d *DB) GetNotificationStatus(ctx context.Context, id int64) (string, error) {
	query := `SELECT status FROM notifications WHERE id = $1`

	var status string
	err := d.conn.GetContext(ctx, &status, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotificationNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get notification status: %w", err)
	}
	return status, nil
}

func (d *DB) UpdateNotificationStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE notifications SET status = $1, updated_at = now() WHERE id = $2`

	result, err := d.conn.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update notification status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

// ClaimStalePending atomically claims up to limit pending notifications that
// have not been touched for staleAfter. Claiming bumps updated_at, so a claimed
// row leaves the stale window, and SKIP LOCKED keeps concurrent sweeps apart.
func (d *DB) ClaimStalePending(
	ctx context.Context,
	staleAfter time.Duration,
	limit int,
) ([]StaleNotification, error) {
	query := `
		UPDATE notifications
		SET sweep_attempts = sweep_attempts + 1, updated_at = now()
		WHERE id IN (
			SELECT id FROM notifications
			WHERE status = 'pending' AND updated_at < now() - $1::interval
			ORDER BY id LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, user_id, title, priority, message, type, sweep_attempts`

	age := fmt.Sprintf("%d seconds", int(staleAfter.Seconds()))
	rows, err := d.conn.QueryContext(ctx, query, age, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to claim stale pending notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var claimed []StaleNotification
	for rows.Next() {
		var stale StaleNotification
		if err := rows.Scan(
			&stale.NotificationID, &stale.UserID, &stale.Title, &stale.Priority,
			&stale.Message.Message, &stale.Type, &stale.SweepAttempts,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stale pending notification: %w", err)
		}
		claimed = append(claimed, stale)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read stale pending notifications: %w", err)
	}
	return claimed, nil
}
