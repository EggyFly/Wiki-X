ALTER TABLE pages ADD COLUMN translation_base_version TEXT NOT NULL DEFAULT '';
ALTER TABLE page_revisions ADD COLUMN translation_base_version TEXT NOT NULL DEFAULT '';
