package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

func TestInsertNotificationReturnsGeneratedID(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("INSERT INTO notifications").
		WithArgs("user-1", "", "", "", "hello", "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))

	id, err := db.InsertNotification(
		context.Background(), notification.Message{UserID: "user-1", Message: "hello"},
	)

	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertNotificationStoresDeliveryFields(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("INSERT INTO notifications").
		WithArgs("user-1", "Title", "push", "3", "hello", "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	_, err := db.InsertNotification(context.Background(), notification.Message{
		UserID: "user-1", Title: "Title", Type: "push", Priority: "3", Message: "hello",
	})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertNotificationWrapsDriverError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("INSERT INTO notifications").WillReturnError(errBoom)

	_, err := db.InsertNotification(context.Background(), notification.Message{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert notification")
	assert.ErrorIs(t, err, errBoom)
}

func TestMarkQueuedOnlyMovesPendingNotifications(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec(`status = 'queued'.*status = 'pending'`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, db.MarkQueued(context.Background(), 42))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkQueuedIgnoresNotificationsPastPending(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec("UPDATE notifications").
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	assert.NoError(t, db.MarkQueued(context.Background(), 42))
}

func TestMarkQueuedWrapsDriverError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec("UPDATE notifications").WillReturnError(errBoom)

	err := db.MarkQueued(context.Background(), 42)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mark queued")
	assert.ErrorIs(t, err, errBoom)
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

func TestGetNotificationStatusWrapsDriverError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("SELECT status FROM notifications").WillReturnError(errBoom)

	_, err := db.GetNotificationStatus(context.Background(), 42)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "get notification status")
	assert.ErrorIs(t, err, errBoom)
}

func TestUpdateNotificationStatusRefreshesUpdatedAt(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec(`SET status = \$1, updated_at = now\(\)`).
		WithArgs("sent", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, db.UpdateNotificationStatus(context.Background(), 42, "sent"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNotificationStatusReportsMissingNotification(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec("UPDATE notifications").
		WithArgs("sent", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := db.UpdateNotificationStatus(context.Background(), 42, "sent")

	assert.ErrorIs(t, err, ErrNotificationNotFound)
}

func TestUpdateNotificationStatusWrapsDriverError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec("UPDATE notifications").WillReturnError(errBoom)

	err := db.UpdateNotificationStatus(context.Background(), 42, "sent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "update notification status")
	assert.ErrorIs(t, err, errBoom)
}

func TestClaimStalePendingReturnsClaimedNotifications(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("UPDATE notifications").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "user_id", "title", "priority", "message", "type", "sweep_attempts"},
		).AddRow(7, "user-1", "T", "3", "hello", "push", 2))

	claimed, err := db.ClaimStalePending(context.Background(), time.Minute, 10)

	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, int64(7), claimed[0].NotificationID)
	assert.Equal(t, "user-1", claimed[0].UserID)
	assert.Equal(t, "T", claimed[0].Title)
	assert.Equal(t, "3", claimed[0].Priority)
	assert.Equal(t, "hello", claimed[0].Message.Message)
	assert.Equal(t, "push", claimed[0].Type)
	assert.Equal(t, 2, claimed[0].SweepAttempts)
}

func TestClaimStalePendingPassesAgeAndLimit(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
		WithArgs("300 seconds", 100).
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "user_id", "title", "priority", "message", "type", "sweep_attempts"},
		))

	claimed, err := db.ClaimStalePending(context.Background(), 5*time.Minute, 100)

	require.NoError(t, err)
	assert.Empty(t, claimed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimStalePendingWrapsDriverError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("UPDATE notifications").WillReturnError(errBoom)

	_, err := db.ClaimStalePending(context.Background(), time.Minute, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim stale pending")
	assert.ErrorIs(t, err, errBoom)
}

func TestUpdateNotificationStatusWrapsRowsAffectedError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectExec("UPDATE notifications").WillReturnResult(sqlmock.NewErrorResult(errBoom))

	err := db.UpdateNotificationStatus(context.Background(), 42, "sent")

	assert.ErrorContains(t, err, "rows affected")
	assert.ErrorIs(t, err, errBoom)
}

func TestClaimStalePendingWrapsScanError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("UPDATE notifications").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "user_id", "title", "priority", "message", "type", "sweep_attempts"},
		).AddRow("not-a-number", "user-1", "T", "3", "hello", "push", 2))

	claimed, err := db.ClaimStalePending(context.Background(), time.Minute, 10)

	assert.Nil(t, claimed)
	assert.ErrorContains(t, err, "scan stale pending")
}

func TestClaimStalePendingWrapsRowIterationError(t *testing.T) {
	db, mock := newMockedDB(t)
	mock.ExpectQuery("UPDATE notifications").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "user_id", "title", "priority", "message", "type", "sweep_attempts"},
		).AddRow(7, "user-1", "T", "3", "hello", "push", 2).RowError(0, errBoom))

	claimed, err := db.ClaimStalePending(context.Background(), time.Minute, 10)

	assert.Nil(t, claimed)
	assert.ErrorContains(t, err, "read stale pending")
	assert.ErrorIs(t, err, errBoom)
}
