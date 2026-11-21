-- 0004: 内网中枢基础表（M2-01）：会话、隧道、凭据、主机台账。
-- 来源：二开版 db/schema.sql 与 db/host_ledger.go 的建表语句，逐列对齐。

CREATE TABLE IF NOT EXISTS sessions (
    id                BIGSERIAL PRIMARY KEY,
    kind              TEXT NOT NULL,
    host_asset_id     BIGINT,
    url               TEXT NOT NULL DEFAULT '',
    secret            TEXT NOT NULL DEFAULT '',
    lang              TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'alive' CHECK(status IN ('alive','dead')),
    created_by_task   BIGINT,
    created_by_intent BIGINT,
    last_beat         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status, last_beat);

CREATE TABLE IF NOT EXISTS tunnels (
    id             BIGSERIAL PRIMARY KEY,
    task_id        BIGINT NOT NULL DEFAULT 0,
    kind           TEXT NOT NULL CHECK(kind IN ('socks','portfwd')),
    adapter        TEXT NOT NULL DEFAULT 'chisel',
    listen_host    TEXT NOT NULL DEFAULT '',
    listen_port    INT  NOT NULL DEFAULT 0,
    target_host    TEXT NOT NULL DEFAULT '',
    target_port    INT  NOT NULL DEFAULT 0,
    via_session_id BIGINT NOT NULL DEFAULT 0,
    remote_pid     INT  NOT NULL DEFAULT 0,
    deploy_params  JSONB NOT NULL DEFAULT '{}',
    state          TEXT NOT NULL DEFAULT 'deploying' CHECK(state IN ('deploying','alive','error','stopped')),
    last_check     TIMESTAMPTZ NOT NULL DEFAULT now(),
    error          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tunnels_task ON tunnels(task_id, state);

CREATE TABLE IF NOT EXISTS credentials (
    id            BIGSERIAL PRIMARY KEY,
    task_id       BIGINT NOT NULL,
    host_asset_id BIGINT,
    username      TEXT NOT NULL DEFAULT '',
    cred_type     TEXT NOT NULL CHECK(cred_type IN (
        'password','hash_nt','hash_lm','hash_sha1','ticket','ssh_key','token','hash_unix')),
    secret        TEXT NOT NULL DEFAULT '',
    domain        TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT '',
    verified      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_credentials_task ON credentials(task_id);

CREATE TABLE IF NOT EXISTS host_ledger (
    id            BIGSERIAL PRIMARY KEY,
    session_id    BIGINT NOT NULL DEFAULT 0,
    host          TEXT NOT NULL DEFAULT '',
    channel       TEXT NOT NULL DEFAULT '',
    cmd           TEXT NOT NULL DEFAULT '',
    stdout        TEXT NOT NULL DEFAULT '',
    ok            BOOLEAN NOT NULL DEFAULT true,
    ms            INTEGER NOT NULL DEFAULT 0,
    artifact      BOOLEAN NOT NULL DEFAULT false,
    artifact_type TEXT NOT NULL DEFAULT '',
    cleanup_cmd   TEXT NOT NULL DEFAULT '',
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_host_ledger_session ON host_ledger(session_id);
CREATE INDEX IF NOT EXISTS idx_host_ledger_host ON host_ledger(host);