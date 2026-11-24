-- 0015: 出口代理控制面 P0：受管订阅源持久化。
-- 复用 proxy_pool 作为 endpoint；本表只存一个 subscription source 的配置/状态。

CREATE TABLE IF NOT EXISTS egress_sources (
    id                    BIGSERIAL PRIMARY KEY,
    name                  TEXT NOT NULL DEFAULT '',
    kind                  TEXT NOT NULL DEFAULT 'subscription' CHECK (kind IN ('subscription')),
    desired_state         TEXT NOT NULL DEFAULT 'disabled' CHECK (desired_state IN ('active','disabled')),
    observed_state        TEXT NOT NULL DEFAULT 'disabled' CHECK (observed_state IN ('disabled','starting','ready','degraded','failed')),
    config_enc            TEXT NOT NULL DEFAULT '',
    config_hash           TEXT NOT NULL DEFAULT '',
    last_good_config_enc  TEXT NOT NULL DEFAULT '',
    last_good_hash        TEXT NOT NULL DEFAULT '',
    managed_proxy_id      BIGINT REFERENCES proxy_pool(id) ON DELETE SET NULL,
    managed_port          INTEGER NOT NULL DEFAULT 0,
    etag                  TEXT NOT NULL DEFAULT '',
    last_modified         TEXT NOT NULL DEFAULT '',
    last_fetch_at         TIMESTAMPTZ,
    last_error            TEXT NOT NULL DEFAULT '',
    revision              BIGINT NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS egress_sources_one_subscription ON egress_sources(kind);
CREATE INDEX IF NOT EXISTS idx_egress_sources_desired ON egress_sources(desired_state, id);
