CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE identities (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    subject TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);

CREATE INDEX identities_user_idx ON identities (user_id);

CREATE TABLE groups (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE user_groups (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE group_rights (
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    right_name TEXT NOT NULL,
    PRIMARY KEY (group_id, right_name)
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT ''
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE pages (
    id BIGSERIAL PRIMARY KEY,
    route TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    draft BOOLEAN NOT NULL DEFAULT false,
    version TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    current_revision_id BIGINT
);

CREATE UNIQUE INDEX pages_route_lower_idx ON pages (lower(route));
CREATE INDEX pages_updated_idx ON pages (updated_at DESC);

CREATE TABLE page_revisions (
    id BIGSERIAL PRIMARY KEY,
    page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    author_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    comment TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending', 'published', 'rejected')),
    base_revision_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    version TEXT NOT NULL
);

CREATE INDEX page_revisions_page_idx ON page_revisions (page_id, created_at DESC);
CREATE INDEX page_revisions_status_idx ON page_revisions (status, created_at);

CREATE TABLE media (
    id BIGSERIAL PRIMARY KEY,
    file_name TEXT NOT NULL UNIQUE,
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL,
    uploader_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_created_idx ON audit_logs (created_at DESC);

INSERT INTO groups (name) VALUES ('admin'), ('editor'), ('user')
ON CONFLICT DO NOTHING;

INSERT INTO group_rights (group_id, right_name)
SELECT g.id, v.right_name
FROM groups g
JOIN (VALUES
    ('admin', 'read'),
    ('admin', 'edit'),
    ('admin', 'review'),
    ('admin', 'upload'),
    ('admin', 'manage_users'),
    ('editor', 'read'),
    ('editor', 'edit'),
    ('editor', 'upload'),
    ('user', 'read')
) AS v(group_name, right_name) ON v.group_name = g.name
ON CONFLICT DO NOTHING;
