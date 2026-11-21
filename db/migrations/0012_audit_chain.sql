-- 0012: host_ledger 审计完整性（哈希链）。
-- 每行记录 prev_hash 与 entry_hash，任何对历史行的修改都会在 /api/security/audit/verify
-- 的链式校验中暴露。历史行（升级前）entry_hash 为空，校验从第一条有哈希的行开始。

CREATE TABLE IF NOT EXISTS host_ledger(
    id BIGSERIAL PRIMARY KEY, session_id BIGINT NOT NULL DEFAULT 0,
    host TEXT NOT NULL DEFAULT '', channel TEXT NOT NULL DEFAULT '',
    cmd TEXT NOT NULL DEFAULT '', stdout TEXT NOT NULL DEFAULT '',
    ok BOOLEAN NOT NULL DEFAULT true, ms INTEGER NOT NULL DEFAULT 0,
    artifact BOOLEAN NOT NULL DEFAULT false, artifact_type TEXT NOT NULL DEFAULT '',
    cleanup_cmd TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now());

ALTER TABLE host_ledger ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE host_ledger ADD COLUMN IF NOT EXISTS entry_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_host_ledger_entry_hash ON host_ledger(entry_hash);
