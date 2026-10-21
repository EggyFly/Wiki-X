ALTER TABLE pages ADD COLUMN protected BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE watchlist (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    route TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, route)
);

CREATE TABLE comments (
    id BIGSERIAL PRIMARY KEY,
    route TEXT NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible', 'hidden')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX comments_route_idx ON comments (route, created_at DESC);
