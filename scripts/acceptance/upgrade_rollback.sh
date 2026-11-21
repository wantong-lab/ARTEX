#!/usr/bin/env bash
# P2-12 升级 / 回滚 / 备份恢复 / soak 验收脚本。
# 流程：旧版 v0.4.1 起服务→建任务→备份(pg_dump+data tar)→新版升级校验→soak→
# 恢复备份+旧版回滚校验→再次升级校验。依赖 docker 容器 artex-migtest、curl、python3。
set -uo pipefail

BASE_PORT=18912
DB=artex_p212
DATA_DIR=/tmp/artex-p212-data
WORK=/tmp/artex-p212
OLD_BIN=/tmp/artex-v041/artex-0.4.1-darwin-arm64/artex
NEW_BIN=/tmp/artex-p212-current
BACKUP_DIR=$WORK/backup
SOAK_SECONDS=45
SOAK_WORKERS=8
EXPECTED_MIGRATIONS=11
PG_CONTAINER=artex-migtest
BASE=http://127.0.0.1:$BASE_PORT
USER_NAME=ARTEX
PASSWORD=Accept-Artex-2026!

PID=""
FAILED=0
PASS_COUNT=0

log()  { echo "[$(date +%H:%M:%S)] $*"; }
pass() { PASS_COUNT=$((PASS_COUNT+1)); echo "PASS $*"; }
fail() { FAILED=1; echo "FAIL $*"; }

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null
    for _ in $(seq 1 20); do kill -0 "$PID" 2>/dev/null || break; sleep 0.2; done
    kill -9 "$PID" 2>/dev/null
  fi
}
trap cleanup EXIT

psql_admin() { docker exec "$PG_CONTAINER" psql -U artex -d postgres -v ON_ERROR_STOP=1 "$@"; }
psql_db()    { docker exec "$PG_CONTAINER" psql -U artex -d "$DB" -t -A "$@"; }

wait_health() {
  local i
  for i in $(seq 1 60); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/health")" = "200" ]; then return 0; fi
    sleep 0.5
  done
  return 1
}

start_bin() {
  local bin=$1 logfile=$2
  ARTEX_PG_DSN="postgres://artex:artex@127.0.0.1:55432/$DB?sslmode=disable" \
    nohup "$bin" -addr "127.0.0.1:$BASE_PORT" -data "$DATA_DIR" -proxy "" > "$logfile" 2>&1 &
  PID=$!
  wait_health || { fail "start $bin (health timeout, log $logfile)"; tail -20 "$logfile"; return 1; }
  pass "start $(basename "$bin") pid=$PID"
}

stop_bin() {
  [ -n "$PID" ] || return 0
  kill "$PID" 2>/dev/null
  local i
  for i in $(seq 1 30); do kill -0 "$PID" 2>/dev/null || break; sleep 0.2; done
  kill -9 "$PID" 2>/dev/null
  wait "$PID" 2>/dev/null
  PID=""
}

login() {
  local resp
  resp=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASSWORD\"}")
  TOKEN=$(printf '%s' "$resp" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))' 2>/dev/null)
  [ -n "$TOKEN" ]
}

api_get() {
  curl -s -o "$WORK/last_resp.json" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$BASE$1"
}

mkdir -p "$WORK" "$BACKUP_DIR"
[ -x "$OLD_BIN" ] || { echo "FAIL old binary missing: $OLD_BIN"; exit 1; }
[ -x "$NEW_BIN" ] || { echo "FAIL new binary missing: $NEW_BIN"; exit 1; }
pass "prerequisites: old=$OLD_BIN new=$NEW_BIN"

psql_admin -c "DROP DATABASE IF EXISTS \"$DB\" WITH (FORCE)" >/dev/null 2>&1
psql_admin -c "CREATE DATABASE \"$DB\"" >/dev/null
rm -rf "$DATA_DIR"; mkdir -p "$DATA_DIR"
pass "fresh database $DB + data dir"

start_bin "$OLD_BIN" "$WORK/old.log" || exit 1
curl -s -o /dev/null -X POST "$BASE/api/auth/init" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASSWORD\"}"
login || { fail "old login"; exit 1; }
pass "old login"
TASK_RESP=$(curl -s -X POST "$BASE/api/tasks" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"description":"P2-12 升级回滚演练任务","goal":"验证升级/回滚后任务数据不丢失"}')
TASK_ID=$(printf '%s' "$TASK_RESP" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
[ -n "$TASK_ID" ] || { fail "old create task: $TASK_RESP"; exit 1; }
pass "old create task #$TASK_ID"
OLD_MIG=$(psql_db -c 'SELECT count(*) FROM schema_migrations' 2>/dev/null | tr -d ' ')
pass "old schema_migrations=$OLD_MIG"
stop_bin

docker exec "$PG_CONTAINER" pg_dump -U artex -Fc -d "$DB" > "$BACKUP_DIR/db.dump" || { fail "pg_dump"; exit 1; }
tar -czf "$BACKUP_DIR/data.tar.gz" -C "$DATA_DIR" . || { fail "tar data"; exit 1; }
DB_BYTES=$(wc -c < "$BACKUP_DIR/db.dump" | tr -d ' ')
DATA_BYTES=$(wc -c < "$BACKUP_DIR/data.tar.gz" | tr -d ' ')
pass "backup db=$DB_BYTES bytes data=$DATA_BYTES bytes"

start_bin "$NEW_BIN" "$WORK/new.log" || exit 1
login || { fail "new login"; exit 1; }
pass "new login"
NEW_MIG=$(psql_db -c 'SELECT count(*) FROM schema_migrations' 2>/dev/null | tr -d ' ')
[ "$NEW_MIG" -ge "$EXPECTED_MIGRATIONS" ] && pass "upgrade migrations=$NEW_MIG" || fail "upgrade migrations=$NEW_MIG"
CODE=$(api_get "/api/tasks/$TASK_ID")
GOAL=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("goal",""))' "$WORK/last_resp.json" 2>/dev/null)
[ "$CODE" = "200" ] && pass "upgrade task persisted code=$CODE goal=$GOAL" || fail "upgrade task code=$CODE"
for path in /api/tasks /api/scheduler/status /api/llm/profiles /api/intranet/credentials /api/kb/entries?limit=1 /api/settings; do
  CODE=$(api_get "$path")
  [ "$CODE" = "200" ] && pass "upgrade api $path -> $CODE" || fail "upgrade api $path -> $CODE"
done

RSS_BEFORE=$(ps -o rss= -p "$PID" | tr -d ' ')
SOAK_OK=0; SOAK_ERR=0
soak_worker() {
  local end=$1 token=$2 errors=0 total=0 code path
  while [ "$(date +%s)" -lt "$end" ]; do
    for path in /api/health /api/tasks /api/scheduler/status /api/llm/profiles "/api/exploration/activity/history?task=1&session=main"; do
      code=$(curl -s -o /dev/null -w '%{http_code}' -m 5 -H "Authorization: Bearer $token" "$BASE$path")
      total=$((total+1))
      case "$code" in 2*|3*) ;; *) errors=$((errors+1));; esac
    done
  done
  echo "$total $errors"
}
SOAK_END=$(( $(date +%s) + SOAK_SECONDS ))
TMP=$(mktemp -d)
SOAK_PIDS=""
for _ in $(seq 1 "$SOAK_WORKERS"); do
  soak_worker "$SOAK_END" "$TOKEN" > "$TMP/worker_$RANDOM" &
  SOAK_PIDS="$SOAK_PIDS $!"
done
# 只等 soak worker：裸 wait 会把 start_bin 起的服务进程也算进去，永远等不到。
for wp in $SOAK_PIDS; do wait "$wp"; done
for f in "$TMP"/worker_*; do
  [ -f "$f" ] || continue
  read -r t e < "$f"
  SOAK_OK=$((SOAK_OK+t)); SOAK_ERR=$((SOAK_ERR+e))
done
rm -rf "$TMP"
RSS_AFTER=$(ps -o rss= -p "$PID" | tr -d ' ')
CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/health")
if [ "$SOAK_ERR" -eq 0 ] && [ "$CODE" = "200" ]; then
  pass "soak $SOAK_SECONDS s workers=$SOAK_WORKERS requests=$SOAK_OK errors=$SOAK_ERR health=$CODE rss=$RSS_BEFORE KB -> $RSS_AFTER KB"
else
  fail "soak requests=$SOAK_OK errors=$SOAK_ERR health=$CODE"
fi
stop_bin

psql_admin -c "DROP DATABASE IF EXISTS \"$DB\" WITH (FORCE)" >/dev/null 2>&1
psql_admin -c "CREATE DATABASE \"$DB\"" >/dev/null
docker exec -i "$PG_CONTAINER" pg_restore -U artex -d "$DB" --no-owner --exit-on-error < "$BACKUP_DIR/db.dump" >/dev/null 2>&1 || { fail "pg_restore"; exit 1; }
rm -rf "$DATA_DIR"; mkdir -p "$DATA_DIR"
tar -xzf "$BACKUP_DIR/data.tar.gz" -C "$DATA_DIR" || { fail "restore data"; exit 1; }
pass "restore backup db+data"
start_bin "$OLD_BIN" "$WORK/rollback.log" || exit 1
login || { fail "rollback login"; exit 1; }
CODE=$(api_get "/api/tasks/$TASK_ID")
[ "$CODE" = "200" ] && pass "rollback task visible code=$CODE" || fail "rollback task code=$CODE"
RB_MIG=$(psql_db -c 'SELECT count(*) FROM schema_migrations' 2>/dev/null | tr -d ' ')
pass "rollback schema_migrations=$RB_MIG"
stop_bin

start_bin "$NEW_BIN" "$WORK/reupgrade.log" || exit 1
login || { fail "re-upgrade login"; exit 1; }
CODE=$(api_get "/api/tasks/$TASK_ID")
[ "$CODE" = "200" ] && pass "re-upgrade task visible code=$CODE" || fail "re-upgrade task code=$CODE"
REUP_MIG=$(psql_db -c 'SELECT count(*) FROM schema_migrations' 2>/dev/null | tr -d ' ')
[ "$REUP_MIG" -ge "$EXPECTED_MIGRATIONS" ] && pass "re-upgrade migrations=$REUP_MIG" || fail "re-upgrade migrations=$REUP_MIG"
stop_bin

if [ "$FAILED" -eq 0 ]; then
  echo "RESULT PASS steps=$PASS_COUNT"
  exit 0
fi
echo "RESULT FAIL steps=$PASS_COUNT"
exit 1
