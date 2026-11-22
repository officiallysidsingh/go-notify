package repository

import (
	"context"
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
	return newDB(cfg, sqlx.Open)
}

// newDB takes the open function as a seam so tests can inject failures.
func newDB(
	cfg config.PostgresConfig,
	open func(driver, dsn string) (*sqlx.DB, error),
) (*DB, error) {
	// Create a context with timeout for establishing the connection.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnTimeout)
	defer cancel()

	// Open a database connection using sqlx.
	db, err := open("postgres", cfg.DataSourceName)
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

// Ping verifies the database is reachable.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.conn.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping DB: %w", err)
	}
	return nil
}
