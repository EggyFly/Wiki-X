CREATE TABLE interactive_nodes (
    id BIGSERIAL PRIMARY KEY,
    node_type TEXT NOT NULL CHECK (node_type IN ('widget', 'game')),
    node_key TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    revoked_at TIMESTAMPTZ,
    revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    revoke_reason TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX interactive_nodes_active_key ON interactive_nodes (node_type, node_key)
WHERE revoked_at IS NULL;

CREATE INDEX interactive_nodes_list_idx ON interactive_nodes (node_type, node_key)
WHERE revoked_at IS NULL;
