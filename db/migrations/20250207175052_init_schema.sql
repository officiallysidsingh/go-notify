-- +goose Up
CREATE TABLE notifications (
    id SERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE notifications;
