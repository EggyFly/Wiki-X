ALTER TABLE pages ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE pages ADD COLUMN deleted_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE pages ADD COLUMN delete_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX pages_deleted_idx ON pages (deleted_at);
