-- 0014: C2 入站中继节点台账（P2-C）
CREATE TABLE IF NOT EXISTS c2_ingress (
    id             BIGSERIAL PRIMARY KEY,
    tunnel_id      BIGINT NOT NULL UNIQUE,
    listener_id    BIGINT NOT NULL DEFAULT 0,
    task_id        BIGINT NOT NULL DEFAULT 0,
    vps_host       TEXT NOT NULL DEFAULT '',
    vps_port       INT NOT NULL DEFAULT 0,
    node_label     TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'unknown',
    healthy        BOOLEAN NOT NULL DEFAULT false,
    last_health_at TIMESTAMPTZ,
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_c2_ingress_active ON c2_ingress(stopped_at, id);
