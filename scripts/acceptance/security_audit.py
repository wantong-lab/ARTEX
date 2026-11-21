#!/usr/bin/env python3
"""P2-13 安全审计验收脚本。

对运行中的 ARTEX 服务验证：
  1. 密钥静置加密：凭据 secret、代理池 URL、C2 监听器 token 落库为 gcm1: 密文且不含明文；
     会话 secret 若存在则同样校验（无会话时跳过并标注）。
  2. C2 非回环校验：默认拒绝 0.0.0.0 绑定（错误提示 c2_allow_non_loopback）；
     显式开启 c2_allow_non_loopback 后放行；验收后恢复默认关闭。
  3. 审计完整性：GET /api/security/audit/verify 返回 ok=true（哈希链）/历史行统计。
  4. 轮换能力：脚本本身不改 master key（需停服务 + CLI），但校验密文格式可轮换；
     轮换行为由 go test ./db -run TestRotateSecretKeys 与 cmd/artex-rotate-key 覆盖。

用法：python3 scripts/acceptance/security_audit.py --base http://127.0.0.1:18905 \
          --password 'Accept-Artex-2026!' --db artex_p213
"""

import argparse
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

PG_CONTAINER = os.environ.get("PG_CONTAINER", "artex-migtest")


def http(method, url, token=None, body=None, timeout=60):
    data = None
    headers = {"Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", "replace")
            return resp.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


def psql(db, sql):
    out = subprocess.run(
        ["docker", "exec", PG_CONTAINER, "psql", "-U", "artex", "-d", db, "-t", "-A", "-c", sql],
        capture_output=True, text=True, timeout=30,
    )
    if out.returncode != 0:
        raise RuntimeError("psql failed: " + out.stderr.strip())
    return out.stdout.strip()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("ARTEX_BASE", "http://127.0.0.1:18905"))
    ap.add_argument("--user", default=os.environ.get("ARTEX_USER", "ARTEX"))
    ap.add_argument("--password", default=os.environ.get("ARTEX_PASSWORD", "Accept-Artex-2026!"))
    ap.add_argument("--db", default=os.environ.get("ARTEX_AUDIT_DB", "artex_p213"))
    args = ap.parse_args()
    base = args.base.rstrip("/")
    result = {"base": base, "db": args.db, "steps": [], "ok": False}

    def step(name, ok, detail):
        result["steps"].append({"name": name, "ok": bool(ok), "detail": str(detail)[:500]})
        print(("PASS " if ok else "FAIL ") + name + ": " + str(detail)[:240], flush=True)

    code, r = http("POST", base + "/api/auth/login", body={"username": args.user, "password": args.password})
    token = r.get("token")
    if code != 200 or not token:
        step("login", False, r)
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 1
    step("login", True, {"user": args.user})

    # 找一个任务用于凭据归属；没有就创建一个。
    code, tasks = http("GET", base + "/api/tasks", token)
    task_id = None
    if isinstance(tasks, dict):
        for key in ("tasks", "items"):
            if tasks.get(key):
                task_id = int(tasks[key][0]["id"])
                break
        if task_id is None and tasks.get("id"):
            task_id = int(tasks["id"])
    if task_id is None:
        code, t = http("POST", base + "/api/tasks", token, {"description": "P2-13 安全审计", "goal": "安全审计占位任务"})
        task_id = int(t["id"])
    step("task-context", True, {"task_id": task_id})

    secret_plain = "P2-13-SECRET-PLAIN-do-not-store"
    code, cred = http("POST", base + "/api/intranet/credentials", token, {
        "task_id": task_id, "username": "sec-audit", "cred_type": "password",
        "secret": secret_plain, "source": "audit-script",
    })
    cred_id = int(cred["id"]) if isinstance(cred, dict) and cred.get("id") else None
    stored = psql(args.db, "SELECT secret FROM credentials WHERE id=" + str(cred_id)) if cred_id else ""
    step("credential-encrypted-at-rest", code in (200, 201) and stored.startswith("gcm1:") and secret_plain not in stored,
         {"code": code, "stored_prefix": stored[:24], "plaintext_leaked": secret_plain in stored})
    if cred_id:
        http("DELETE", base + "/api/intranet/credentials/" + str(cred_id), token)

    proxy_plain = "P2-13-PROXYPASS"
    proxy_url = "socks5://audituser:" + proxy_plain + "@127.0.0.1:18081"
    code, proxy = http("POST", base + "/api/proxy-pool/entries", token, {"name": "sec-audit", "url": proxy_url})
    results = proxy.get("results") if isinstance(proxy, dict) else None
    proxy_id = None
    proxy_err = ""
    if results:
        proxy_id = int(results[0]["id"]) if results[0].get("id") else None
        proxy_err = str(results[0].get("error", ""))
    stored = psql(args.db, "SELECT url_enc FROM proxy_pool WHERE id=" + str(proxy_id)) if proxy_id else ""
    step("proxy-url-encrypted-at-rest", code == 200 and stored.startswith("gcm1:") and proxy_plain not in stored,
         {"code": code, "id": proxy_id, "error": proxy_err, "stored_prefix": stored[:24], "plaintext_leaked": proxy_plain in stored})
    if proxy_id:
        http("DELETE", base + "/api/proxy-pool/" + str(proxy_id), token)

    # C2：默认非回环拒绝；显式开启后允许；验收后恢复关闭。
    http("PUT", base + "/api/settings", token, {"c2_allow_non_loopback": False})
    code, denied = http("POST", base + "/api/c2/listeners", token,
                        {"name": "sec-audit-external", "bind_host": "0.0.0.0", "bind_port": 45999, "kind": "tcp"})
    err = str(denied.get("error", "")) if isinstance(denied, dict) else ""
    step("c2-non-loopback-rejected", code == 400 and "c2_allow_non_loopback" in err, {"code": code, "error": err[:160]})

    http("PUT", base + "/api/settings", token, {"c2_allow_non_loopback": True})
    code, created = http("POST", base + "/api/c2/listeners", token,
                         {"name": "sec-audit-external", "bind_host": "0.0.0.0", "bind_port": 45999, "kind": "tcp"})
    c2_id = int(created["id"]) if isinstance(created, dict) and created.get("id") else None
    c2_token = created.get("token", "") if isinstance(created, dict) else ""
    tok_enc = psql(args.db, "SELECT token_enc FROM c2_listeners WHERE id=" + str(c2_id)) if c2_id else ""
    step("c2-token-encrypted-and-optin", code == 200 and tok_enc.startswith("gcm1:") and c2_token not in tok_enc,
         {"code": code, "stored_prefix": tok_enc[:24], "plaintext_leaked": c2_token in tok_enc})
    if c2_id:
        http("DELETE", base + "/api/c2/listeners/" + str(c2_id), token)
    http("PUT", base + "/api/settings", token, {"c2_allow_non_loopback": False})
    code, payload = http("GET", base + "/api/settings", token)
    step("c2-setting-restored", code == 200 and payload.get("c2_allow_non_loopback") is False,
         {"code": code, "c2_allow_non_loopback": payload.get("c2_allow_non_loopback")})

    # 会话 secret 若存在，校验密文格式（无会话时跳过）。
    try:
        bad_sessions = psql(args.db, "SELECT count(*) FROM sessions WHERE secret <> '' AND secret NOT LIKE 'gcm1:%'")
        total_sessions = psql(args.db, "SELECT count(*) FROM sessions")
        step("session-secret-encrypted-at-rest", bad_sessions.strip() == "0",
             {"sessions": total_sessions.strip(), "non_gcm1": bad_sessions.strip()})
    except Exception as e:
        step("session-secret-encrypted-at-rest", False, str(e))

    # 审计完整性：哈希链校验端点。
    code, audit = http("GET", base + "/api/security/audit/verify", token)
    step("audit-chain-verify", code == 200 and audit.get("ok") is True,
         {"code": code, "audit": audit})

    result["ok"] = all(s["ok"] for s in result["steps"])
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
