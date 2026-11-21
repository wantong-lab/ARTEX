-- 0009: 数据保留与清理审计（M5-02）。
-- 清理动作（含 dry-run）都会留一行审计，便于复核「删了什么、按什么策略」。

CREATE TABLE IF NOT EXISTS retention_audit (
    id             BIGSERIAL PRIMARY KEY,
    at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor          TEXT NOT NULL DEFAULT '',
    dry_run        BOOLEAN NOT NULL DEFAULT false,
    activity_days  INTEGER NOT NULL DEFAULT 30,
    logs_days      INTEGER NOT NULL DEFAULT 14,
    c2_days        INTEGER NOT NULL DEFAULT 30,
    activity_deleted    BIGINT NOT NULL DEFAULT 0,
    logs_deleted        BIGINT NOT NULL DEFAULT 0,
    c2_tasks_deleted    BIGINT NOT NULL DEFAULT 0,
    c2_events_deleted   BIGINT NOT NULL DEFAULT 0,
    c2_sessions_deleted BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_retention_audit_at ON retention_audit(at DESC);
CREATE INDEX IF NOT EXISTS idx_activity_created ON activity(created_at);
CREATE INDEX IF NOT EXISTS idx_server_logs_created ON server_logs(created_at);