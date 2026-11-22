//go:build integration

package integration_test

import (
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const firstMigration = 20250207175052

func TestMigrationsApplyAndRollBack(t *testing.T) {
	db := openPostgres(t)
	defer func() { _ = db.Close() }()
	dropSchema(t, db)
	require.NoError(t, goose.Up(db, migrationsDir))
	require.NoError(t, goose.DownTo(db, migrationsDir, 0))
	require.NoError(t, goose.Up(db, migrationsDir))
}

func TestMigrationFailsLeftoverPendingRows(t *testing.T) {
	db := openPostgres(t)
	defer func() { _ = db.Close() }()
	dropSchema(t, db)
	require.NoError(t, goose.UpTo(db, migrationsDir, firstMigration))

	_, err := db.Exec(`INSERT INTO notifications (user_id, message, status) VALUES
		('u1', 'pending row', 'pending'),
		('u2', 'null row', NULL),
		('u3', 'sent row', 'sent')`)
	require.NoError(t, err)

	require.NoError(t, goose.Up(db, migrationsDir))

	rows, err := db.Query(`SELECT user_id, status FROM notifications ORDER BY user_id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	got := map[string]string{}
	for rows.Next() {
		var user, status string
		require.NoError(t, rows.Scan(&user, &status))
		got[user] = status
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, map[string]string{"u1": "failed", "u2": "failed", "u3": "sent"}, got)
}
