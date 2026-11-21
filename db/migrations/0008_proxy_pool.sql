-- 0008: 代理池（M4-03）：录入的出口代理与探测健康状态。
-- URL 整体加密落库（含认证信息）；状态：active=可用 / disabled=停用 / failed=探测失败。

CREATE TABLE IF NOT EXISTS proxy_pool (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    url_enc      TEXT NOT NULL,
    scheme       TEXT NOT NULL,
    host         TEXT NOT NULL,
    port         INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','failed')),
    exit_ip      TEXT NOT NULL DEFAULT '',
    latency_ms   INTEGER NOT NULL DEFAULT 0,
    fail_count   INTEGER NOT NULL DEFAULT 0,
    last_checked TIMESTAMPTZ,
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(scheme, host, port)
);

CREATE INDEX IF NOT EXISTS idx_proxy_pool_status ON proxy_pool(status, id);