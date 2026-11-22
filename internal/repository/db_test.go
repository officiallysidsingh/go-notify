package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/officiallysidsingh/go-notify/config"
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

func newPingMonitoredDB(t *testing.T) (*DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mockDB.Close() })
	return &DB{conn: sqlx.NewDb(mockDB, "sqlmock")}, mock
}

func TestPingReportsUnreachableDatabase(t *testing.T) {
	db, mock := newPingMonitoredDB(t)
	mock.ExpectPing().WillReturnError(errors.New("boom"))

	err := db.Ping(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping")
	assert.Contains(t, err.Error(), "boom")
}

func TestPingSucceedsWhenDatabaseIsReachable(t *testing.T) {
	db, mock := newPingMonitoredDB(t)
	mock.ExpectPing()

	assert.NoError(t, db.Ping(context.Background()))
}

func TestNewDBReportsOpenFailure(t *testing.T) {
	open := func(string, string) (*sqlx.DB, error) { return nil, errors.New("bad dsn") }

	_, err := newDB(config.PostgresConfig{}, open)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "open")
	assert.Contains(t, err.Error(), "bad dsn")
}

func TestNewDBReportsPingFailure(t *testing.T) {
	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	mock.ExpectPing().WillReturnError(errors.New("unreachable"))
	mock.ExpectClose()
	open := func(string, string) (*sqlx.DB, error) { return sqlx.NewDb(mockDB, "sqlmock"), nil }

	_, err = newDB(config.PostgresConfig{ConnTimeout: 1e9}, open)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping")
	assert.Contains(t, err.Error(), "unreachable")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNewDBConfiguresPoolOnSuccess(t *testing.T) {
	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mockDB.Close() })
	mock.ExpectPing()
	open := func(string, string) (*sqlx.DB, error) { return sqlx.NewDb(mockDB, "sqlmock"), nil }

	db, err := newDB(config.PostgresConfig{ConnTimeout: 1e9, MaxOpenConns: 7}, open)

	require.NoError(t, err)
	assert.Equal(t, 7, db.conn.Stats().MaxOpenConnections)
}
