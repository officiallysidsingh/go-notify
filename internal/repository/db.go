package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/officiallysidsingh/go-notify/config"
)

var ErrNotificationNotFound = errors.New("notification not found")

type DB struct {
	conn *sqlx.DB
}

// Creates a new database connection
func NewDB(cfg config.PostgresConfig) (*DB, error) {
	// Create a context with timeout for establishing the connection.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnTimeout)
	defer cancel()

	// Open a database connection using sqlx.
	db, err := sqlx.Open("postgres", cfg.DataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open DB: %w", err)
	}

	// Ping the database to ensure a successful connection.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping DB: %w", err)
	}

	// Configure the connection pool.
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	return &DB{conn: db}, nil
}

// Close gracefully closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// InsertNotification stores a notification and returns its generated ID.
func (d *DB) InsertNotification(
	ctx context.Context,
	userID, message, status string,
) (int64, error) {
	query := `
		INSERT INTO notifications (user_id, message, status)
		VALUES ($1, $2, $3)
		RETURNING id`

	var id int64
	if err := d.conn.QueryRowContext(ctx, query, userID, message, status).Scan(&id); err != nil {
		return 0, fmt.Errorf("failed to insert notification: %w", err)
	}
	return id, nil
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
	query := `UPDATE notifications SET status = $1 WHERE id = $2`

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
