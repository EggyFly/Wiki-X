CREATE INDEX pages_search_fts_idx ON pages
USING GIN (to_tsvector('simple', title || ' ' || description || ' ' || body))
WHERE deleted_at IS NULL;
