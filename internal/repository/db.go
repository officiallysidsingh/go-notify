package repository

import (
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

func (d *DB) InsertNotification(userID, message, status string) (int64, error) {
	var id int64
	query := "INSERT INTO notifications (user_id, message, status) VALUES ($1, $2, $3) RETURNING id"

	err := d.Conn.QueryRow(
		query,
		userID,
		message,
		status,
	).Scan(&id)

	return id, err
}

func (d *DB) UpdateNotificationStatus(id int64, status string) error {
	query := "UPDATE notifications SET status=$1 WHERE id=$2"

	_, err := d.Conn.Exec(query, status, id)

	return err
}
