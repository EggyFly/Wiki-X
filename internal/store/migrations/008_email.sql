ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email)) WHERE email <> '';

CREATE TABLE email_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX email_tokens_user_idx ON email_tokens (user_id, kind);
CREATE INDEX email_tokens_expires_idx ON email_tokens (expires_at);
