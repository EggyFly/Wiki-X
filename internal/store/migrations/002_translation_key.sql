ALTER TABLE pages ADD COLUMN translation_key TEXT NOT NULL DEFAULT '';
ALTER TABLE page_revisions ADD COLUMN translation_key TEXT NOT NULL DEFAULT '';
CREATE INDEX pages_translation_key_idx ON pages (translation_key);
