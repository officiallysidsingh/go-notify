package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/officiallysidsingh/go-notify/config"
)

type DB struct {
	Conn *sqlx.DB
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
		return nil, fmt.Errorf("failed to ping DB: %w", err)
	}

	// Configure the connection pool.
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	return &DB{Conn: db}, nil
}

// Close gracefully closes the database connection.
func (d *DB) Close() error {
	return d.Conn.Close()
}

// Inserts a new notification into the database and returns its generated ID
func (d *DB) InsertNotification(
	ctx context.Context,
	userID, message, status string,
) (int64, error) {
	var id int64

	// Begin a transaction
	tx, err := d.Conn.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Rollback if something goes wrong
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	query := `
		INSERT INTO notifications (user_id, message, status) 
		VALUES ($1, $2, $3) 
		RETURNING id`

	err = tx.QueryRowContext(ctx, query, userID, message, status).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert notification: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return id, nil
}

// updates the status of a notification
func (d *DB) UpdateNotificationStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE notifications SET status = $1 WHERE id = $2`

	// ExecContext is used for executing queries with a context.
	result, err := d.Conn.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update notification status: %w", err)
	}

	// Check that exactly one row was affected.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
