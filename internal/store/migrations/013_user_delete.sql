ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN deleted_by BIGINT REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX users_deleted_idx ON users (deleted_at);
