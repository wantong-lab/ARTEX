-- 0005: 越界范围申请（M2-04）。新 IP 按 /24（IPv6 /64）聚合后进入审批队列，
-- 通过后写入 task_scope 并即时生效（guard 每次工具调用重读 scope）。

CREATE TABLE IF NOT EXISTS pending_scope (
    id           BIGSERIAL PRIMARY KEY,
    task_id      BIGINT NOT NULL,
    kind         TEXT NOT NULL CHECK(kind IN ('ip','cidr','domain')),
    value        TEXT NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
    requested_by TEXT NOT NULL DEFAULT 'agent',
    reviewed_by  TEXT NOT NULL DEFAULT '',
    reviewed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(task_id, kind, value)
);

CREATE INDEX IF NOT EXISTS idx_pending_scope_status ON pending_scope(status, task_id);

-- 审批通过写入 task_scope 时用 source='approval'（与 auto/agent/manual 区分审计来源）。
ALTER TABLE task_scope DROP CONSTRAINT IF EXISTS task_scope_source_check;
ALTER TABLE task_scope ADD CONSTRAINT task_scope_source_check
    CHECK (source IN ('auto','agent','manual','approval'));