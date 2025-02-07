package repository

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
