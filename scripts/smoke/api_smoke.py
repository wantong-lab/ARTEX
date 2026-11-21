#!/usr/bin/env python3
"""ARTEX API 冒烟：无 LLM、无外网依赖，CI 与本地均可跑。

用法：SMOKE_BASE=http://127.0.0.1:18899 python3 scripts/smoke/api_smoke.py
可选：SMOKE_PASSWORD（默认 Smoke-Artex-2026!）、SMOKE_C2_PORT（默认 19955）
退出码：0=全通过，1=有失败。
"""
import json
import os
import socket
import sys
import time
import urllib.error
import urllib.request

BASE = os.environ.get("SMOKE_BASE", "http://127.0.0.1:18899")
PASSWORD = os.environ.get("SMOKE_PASSWORD", "Smoke-Artex-2026!")
C2_PORT = int(os.environ.get("SMOKE_C2_PORT", "19955"))
TOKEN = None
RESULTS = []


def req(method, path, body=None, token=None, timeout=30):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(BASE + path, data=data, method=method)
    if body is not None:
        r.add_header("Content-Type", "application/json")
    tok = token if token is not None else TOKEN
    if tok:
        r.add_header("Authorization", "Bearer " + tok)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", "replace")
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, (json.loads(raw) if raw else None)
        except Exception:
            return e.code, raw
    except Exception as e:
        return 0, str(e)


def case(name, fn):
    try:
        ok, detail = fn()
        RESULTS.append((name, bool(ok), str(detail)[:180]))
    except Exception as e:
        RESULTS.append((name, False, "EXC " + str(e)[:160]))


def port_open(port):
    try:
        s = socket.create_connection(("127.0.0.1", port), timeout=1.5)
        s.close()
        return True
    except Exception:
        return False


def main():
    global TOKEN
    case("health", lambda: (req("GET", "/api/health")[0] == 200, req("GET", "/api/health")[1]))
    case("auth 未授权拦截", lambda: (req("GET", "/api/intranet/sessions", token="")[0] == 401, "401"))

    code, body = req("GET", "/api/auth/status")
    initialized = bool(body.get("initialized")) if isinstance(body, dict) else False
    if initialized:
        code, body = req("POST", "/api/auth/login", {"username": "ARTEX", "password": PASSWORD}, token="")
    else:
        code, body = req("POST", "/api/auth/init", {"password": PASSWORD}, token="")
    TOKEN = body.get("token") if isinstance(body, dict) else None
    case("auth 初始化/登录", lambda: (code == 200 and bool(TOKEN), "code=%s" % code))

    def five_lists():
        out = []
        for p in ["sessions", "tunnels", "credentials", "host-ledger", "pending-scope"]:
            c, _ = req("GET", "/api/intranet/" + p)
            out.append(p + ":" + str(c))
        return all(x.endswith(":200") for x in out), ",".join(out)
    case("M2 内网中枢列表", five_lists)

    def kb_flow():
        payload = {"id": "smoke-kb-1", "title": "Smoke SQLi", "category": "tactic", "vuln_class": "sqli", "content": "smoke 冒烟正文：注入"}
        c1, _ = req("POST", "/api/kb/add", payload)
        c2, s = req("GET", "/api/kb/search?q=Smoke%20SQLi")
        c3, e = req("GET", "/api/kb/entry?id=smoke-kb-1")
        empty = req("GET", "/api/kb/search")[1]
        bad = req("POST", "/api/kb/add", {"title": "", "content": "x"})[0]
        ok = c1 == 200 and c2 == 200 and s.get("total", 0) >= 1 and c3 == 200 and empty.get("total") == 0 and bad == 400
        return ok, "add=%s total=%s entry=%s empty=%s bad=%s" % (c1, s.get("total"), c3, empty.get("total"), bad)
    case("M3 知识库增查", kb_flow)

    def ws_generate():
        out = []
        for lang in ["php", "jsp", "aspx"]:
            c, d = req("POST", "/api/webshell/generate", {"lang": lang})
            out.append(lang + ":" + str(c))
            if c != 200 or not d:
                return False, ",".join(out)
        bad = req("POST", "/api/webshell/generate", {"lang": "ruby"})[0]
        return bad == 400, ",".join(out) + ",bad=" + str(bad)
    case("M4 WebShell 生成", ws_generate)
    case("M4 不存在会话执行 502", lambda: (req("POST", "/api/webshell/exec", {"session_id": 999999, "command": "id"})[0] == 502, "502"))
    case("M4 不存在会话清理 404", lambda: (req("DELETE", "/api/webshell/shells/999999")[0] == 404, "404"))

    def c2_life():
        d = req("GET", "/api/c2/status")[1]
        if d.get("enabled") is not False:
            return False, "default off expected"
        on = req("POST", "/api/c2/enable", {"enabled": True})[1].get("enabled") == "on"
        c, l = req("POST", "/api/c2/listeners", {"name": "smoke", "bind_host": "127.0.0.1", "bind_port": C2_PORT})
        if c != 200 or not l.get("id"):
            return False, "listener create %s" % c
        lid = l["id"]
        start = req("POST", "/api/c2/listeners/%s/start" % lid, {})[0]
        time.sleep(0.4)
        opened = port_open(C2_PORT)
        stop = req("POST", "/api/c2/listeners/%s/stop" % lid, {})[0]
        time.sleep(0.4)
        closed = not port_open(C2_PORT)
        off = req("POST", "/api/c2/enable", {"enabled": False})[1].get("enabled") == "off"
        return on and start == 200 and opened and stop == 200 and closed and off, "start=%s open=%s stop=%s closed=%s" % (start, opened, stop, closed)
    case("M4 C2 起停闭环", c2_life)
    case("M4 C2 畸形请求 400", lambda: (req("POST", "/api/c2/enable", {"enabled": "x"})[0] == 400, "400"))

    def proxy_flow():
        c, d = req("GET", "/api/proxy-pool")
        if c != 200 or "entries" not in d:
            return False, "list"
        bad = req("POST", "/api/proxy-pool/entries", {"url": "ftp://1.2.3.4:21"})[1]
        return bool((bad.get("results") or [{}])[0].get("error")), "invalid rejected"
    case("M4 代理池校验", proxy_flow)

    def ret_flow():
        c, d = req("GET", "/api/retention/status")
        p = d.get("policy", {}) if isinstance(d, dict) else {}
        defaults = p.get("activity_days") == 30 and p.get("logs_days") == 14 and p.get("c2_days") == 30
        empty = req("POST", "/api/retention/cleanup")
        empty_dry = bool(isinstance(empty[1], dict) and empty[1].get("dry_run") is True)
        return c == 200 and defaults and empty_dry, "defaults=%s empty_dry=%s" % (defaults, empty_dry)
    case("M5 保留安全默认", ret_flow)

    def task_flow():
        c, t = req("POST", "/api/tasks", {"description": "smoke-task", "goal": "smoke"})
        if c not in (200, 201) or not isinstance(t, dict) or not t.get("id"):
            return False, "create=%s" % c
        d = req("DELETE", "/api/tasks/%s" % t["id"])[0]
        return d == 200, "create=%s delete=%s" % (c, d)
    case("任务创建/删除", task_flow)
    case("M3 CVE status", lambda: (req("GET", "/api/cve/status")[0] == 200, "200"))

    fails = [r for r in RESULTS if not r[1]]
    for name, ok, detail in RESULTS:
        print("%s | %s | %s" % ("PASS" if ok else "FAIL", name, detail))
    print("SMOKE_TOTAL=%d PASS=%d FAIL=%d" % (len(RESULTS), len(RESULTS) - len(fails), len(fails)))
    return 0 if not fails else 1


if __name__ == "__main__":
    sys.exit(main())
