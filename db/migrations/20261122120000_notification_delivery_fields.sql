-- +goose Up
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'push';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS sweep_attempts INTEGER NOT NULL DEFAULT 0;

UPDATE notifications SET status = 'failed' WHERE status IS NULL OR status = 'pending';

ALTER TABLE notifications ALTER COLUMN status SET NOT NULL;

CREATE INDEX notifications_status_updated_at_idx ON notifications (status, updated_at);

-- +goose Down
DROP INDEX IF EXISTS notifications_status_updated_at_idx;

ALTER TABLE notifications ALTER COLUMN status DROP NOT NULL;

ALTER TABLE notifications DROP COLUMN IF EXISTS sweep_attempts;
ALTER TABLE notifications DROP COLUMN IF EXISTS updated_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS priority;
ALTER TABLE notifications DROP COLUMN IF EXISTS type;
ALTER TABLE notifications DROP COLUMN IF EXISTS title;
