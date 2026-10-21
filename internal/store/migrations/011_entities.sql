CREATE TABLE entity_types (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE entity_fields (
    id BIGSERIAL PRIMARY KEY,
    type_id BIGINT NOT NULL REFERENCES entity_types(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'text',
    required BOOLEAN NOT NULL DEFAULT false,
    options JSONB NOT NULL DEFAULT '[]',
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE (type_id, name)
);

CREATE TABLE entities (
    id BIGSERIAL PRIMARY KEY,
    type_id BIGINT NOT NULL REFERENCES entity_types(id) ON DELETE RESTRICT,
    name TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL DEFAULT '{}',
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX entities_type_idx ON entities (type_id);

ALTER TABLE pages ADD COLUMN entity_id BIGINT REFERENCES entities(id) ON DELETE SET NULL;
ALTER TABLE page_revisions ADD COLUMN entity_id BIGINT REFERENCES entities(id) ON DELETE SET NULL;
CREATE INDEX pages_entity_idx ON pages (entity_id);
