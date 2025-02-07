package db

import (
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type DB struct {
	Conn *sqlx.DB
}

func NewDB(dataSource string) *DB {
	conn, err := sqlx.Connect("postgres", dataSource)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	return &DB{Conn: conn}
}

func (d *DB) InsertNotification(userID, message, status string) error {
	query := `INSERT INTO notifications (user_id, message, status) VALUES ($1, $2, $3)`

	_, err := d.Conn.Exec(
		query,
		userID,
		message,
		status,
	)

	return err
}
