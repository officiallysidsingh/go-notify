package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockedDB(t *testing.T) (*DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = mockDB.Close() })
	return &DB{conn: sqlx.NewDb(mockDB, "sqlmock")}, mock
}

func TestInsertNotificationReturnsGeneratedID(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("INSERT INTO notifications").
		WithArgs("user-1", "hello", "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))

	id, err := db.InsertNotification(context.Background(), "user-1", "hello", "pending")

	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetNotificationStatusReturnsStoredStatus(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("SELECT status FROM notifications").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("sent"))

	status, err := db.GetNotificationStatus(context.Background(), 42)

	require.NoError(t, err)
	assert.Equal(t, "sent", status)
}

func TestGetNotificationStatusReportsMissingNotification(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("SELECT status FROM notifications").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)

	_, err := db.GetNotificationStatus(context.Background(), 42)

	assert.ErrorIs(t, err, ErrNotificationNotFound)
}

func TestUpdateNotificationStatusReportsMissingNotification(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec("UPDATE notifications").
		WithArgs("sent", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := db.UpdateNotificationStatus(context.Background(), 42, "sent")

	assert.ErrorIs(t, err, ErrNotificationNotFound)
}
