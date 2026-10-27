ALTER TABLE pages ADD COLUMN IF NOT EXISTS search_vector tsvector
GENERATED ALWAYS AS (to_tsvector('simple', title || ' ' || description || ' ' || body)) STORED;

DROP INDEX IF EXISTS pages_search_fts_idx;

CREATE INDEX IF NOT EXISTS pages_search_vector_idx ON pages
USING GIN (search_vector)
WHERE deleted_at IS NULL;
