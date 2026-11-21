-- 0007: C2 runtime (M4-01): listeners / sessions / tasks / events.
-- Default off (settings c2_enabled=off); binds a port only after explicit enable.

CREATE TABLE IF NOT EXISTS c2_listeners (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    bind_host   TEXT NOT NULL DEFAULT '127.0.0.1',
    bind_port   INTEGER NOT NULL CHECK (bind_port BETWEEN 1 AND 65535),
    token_enc   TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'stopped' CHECK (status IN ('stopped','running','error')),
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS c2_sessions (
    id            BIGSERIAL PRIMARY KEY,
    listener_id   BIGINT NOT NULL REFERENCES c2_listeners(id) ON DELETE CASCADE,
    implant_uuid  TEXT NOT NULL,
    remote_addr   TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT '',
    arch          TEXT NOT NULL DEFAULT '',
    username      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','dead','killed')),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_check_in TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(listener_id, implant_uuid)
);

CREATE TABLE IF NOT EXISTS c2_tasks (
    id           BIGSERIAL PRIMARY KEY,
    session_id   BIGINT NOT NULL REFERENCES c2_sessions(id) ON DELETE CASCADE,
    task_type    TEXT NOT NULL DEFAULT 'exec',
    args         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sent','success','failed','cancelled')),
    output       TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT '',
    source       TEXT NOT NULL DEFAULT 'ai',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    duration_ms  BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS c2_events (
    id          BIGSERIAL PRIMARY KEY,
    category    TEXT NOT NULL DEFAULT 'session',
    listener_id BIGINT,
    session_id  BIGINT,
    task_id     BIGINT,
    message     TEXT NOT NULL DEFAULT '',
    level       TEXT NOT NULL DEFAULT 'info' CHECK (level IN ('info','warn','critical')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_c2_sessions_listener ON c2_sessions(listener_id, status);
CREATE INDEX IF NOT EXISTS idx_c2_tasks_session ON c2_tasks(session_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_c2_events_created ON c2_events(created_at DESC);