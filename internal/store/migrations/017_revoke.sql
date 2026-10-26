ALTER TABLE groups ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE groups ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE groups ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE user_groups ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE user_groups ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE user_groups ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE group_rights ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE group_rights ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE group_rights ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE entity_types ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE entity_types ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE entity_types ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE entity_fields ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE entity_fields ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE entity_fields ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE entities ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE entities ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE entities ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE invite_codes ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE invite_codes ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE invite_codes ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE oidc_clients ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE oidc_clients ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE oidc_clients ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE watchlist ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE watchlist ADD COLUMN revoked_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE watchlist ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_name_key;
CREATE UNIQUE INDEX groups_name_active_idx ON groups (name) WHERE revoked_at IS NULL;

ALTER TABLE entity_types DROP CONSTRAINT IF EXISTS entity_types_name_key;
CREATE UNIQUE INDEX entity_types_name_active_idx ON entity_types (name) WHERE revoked_at IS NULL;

ALTER TABLE entity_fields DROP CONSTRAINT IF EXISTS entity_fields_type_id_name_key;
CREATE UNIQUE INDEX entity_fields_type_name_active_idx ON entity_fields (type_id, name) WHERE revoked_at IS NULL;

ALTER TABLE entities DROP CONSTRAINT IF EXISTS entities_name_key;
CREATE UNIQUE INDEX entities_name_active_idx ON entities (name) WHERE revoked_at IS NULL;

ALTER TABLE pages DROP CONSTRAINT IF EXISTS pages_route_key;
DROP INDEX IF EXISTS pages_route_lower_idx;
CREATE UNIQUE INDEX pages_route_active_idx ON pages (route) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX pages_route_lower_active_idx ON pages (lower(route)) WHERE deleted_at IS NULL;

CREATE INDEX groups_revoked_idx ON groups (revoked_at);
CREATE INDEX user_groups_revoked_idx ON user_groups (revoked_at);
CREATE INDEX group_rights_revoked_idx ON group_rights (revoked_at);
CREATE INDEX entity_types_revoked_idx ON entity_types (revoked_at);
CREATE INDEX entity_fields_revoked_idx ON entity_fields (revoked_at);
CREATE INDEX entities_revoked_idx ON entities (revoked_at);
CREATE INDEX invite_codes_revoked_idx ON invite_codes (revoked_at);
CREATE INDEX oidc_clients_revoked_idx ON oidc_clients (revoked_at);
CREATE INDEX watchlist_revoked_idx ON watchlist (revoked_at);
