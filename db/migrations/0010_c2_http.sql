-- 0010: C2 HTTP beacon listener (P1-6).
-- kind: tcp（默认）| http（植入端轮询 /c2/beacon/<token>）。
ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'tcp';
ALTER TABLE c2_listeners DROP CONSTRAINT IF EXISTS c2_listeners_kind_check;
ALTER TABLE c2_listeners ADD CONSTRAINT c2_listeners_kind_check CHECK (kind IN ('tcp','http'));
CREATE INDEX IF NOT EXISTS idx_c2_tasks_session_status ON c2_tasks(session_id, status);
