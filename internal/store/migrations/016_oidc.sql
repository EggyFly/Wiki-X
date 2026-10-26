CREATE TABLE oidc_clients (
    client_id TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    logo_url TEXT NOT NULL DEFAULT '',
    redirect_uris JSONB NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'openid profile email',
    enabled BOOLEAN NOT NULL DEFAULT true,
    token_endpoint_auth_method TEXT NOT NULL DEFAULT 'client_secret_basic',
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE oidc_auth_codes (
    code_hash TEXT PRIMARY KEY,
    client_id TEXT NOT NULL REFERENCES oidc_clients(client_id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri TEXT NOT NULL,
    scopes TEXT NOT NULL,
    nonce TEXT NOT NULL DEFAULT '',
    code_challenge TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL DEFAULT 'S256',
    auth_time TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);

CREATE TABLE oidc_grants (
    grant_id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL REFERENCES oidc_clients(client_id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scopes TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE oidc_sessions (
    session_id TEXT PRIMARY KEY,
    grant_id TEXT NOT NULL REFERENCES oidc_grants(grant_id) ON DELETE CASCADE,
    client_id TEXT NOT NULL REFERENCES oidc_clients(client_id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE oidc_refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES oidc_sessions(session_id) ON DELETE CASCADE,
    grant_id TEXT NOT NULL REFERENCES oidc_grants(grant_id) ON DELETE CASCADE,
    client_id TEXT NOT NULL REFERENCES oidc_clients(client_id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE oidc_consents (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL REFERENCES oidc_clients(client_id) ON DELETE CASCADE,
    scopes TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, client_id)
);

CREATE TABLE oidc_signing_keys (
    kid TEXT PRIMARY KEY,
    private_key_pem TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    active BOOLEAN NOT NULL DEFAULT true
);

CREATE INDEX oidc_auth_codes_client_idx ON oidc_auth_codes(client_id);
CREATE INDEX oidc_grants_user_idx ON oidc_grants(user_id);
CREATE INDEX oidc_grants_client_idx ON oidc_grants(client_id);
CREATE INDEX oidc_sessions_grant_idx ON oidc_sessions(grant_id);
CREATE INDEX oidc_sessions_user_idx ON oidc_sessions(user_id);
CREATE INDEX oidc_refresh_session_idx ON oidc_refresh_tokens(session_id);
CREATE INDEX oidc_refresh_grant_idx ON oidc_refresh_tokens(grant_id);
CREATE INDEX oidc_signing_keys_active_idx ON oidc_signing_keys(active);
