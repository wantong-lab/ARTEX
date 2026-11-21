#!/usr/bin/env python3
"""ARTEX demo 数据种子。

通过公开 HTTP API 灌入可安全演示的数据（无 LLM、无外网依赖）：
  - 2 个已暂停的示例任务（带 [DEMO] 前缀，便于识别/清理）；
  - 4 条知识库条目（ammo / tactic / negative 三档）；
  - 4 个示例资产（域名/子域/IP/HTTP 服务，全部使用 example.test 与 192.0.2.0/24 保留地址）。

幂等：已有 [DEMO] 前缀的任务/知识库条目会跳过；可重复执行。

用法：
  python3 scripts/demo/seed_demo.py --base http://127.0.0.1:8787 --password '你的登录密码'
  docker compose exec artex python3 /app/seed_demo.py --base http://127.0.0.1:8787 --password '你的登录密码'
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

MARKER = "[DEMO]"


def http(method, url, token=None, body=None, timeout=20):
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
            return e.code, {"error": raw[:200]}


def items_of(payload):
    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict):
        for key in ("items", "entries", "tasks", "rows", "data"):
            v = payload.get(key)
            if isinstance(v, list):
                return v
    return []


def wait_health(base, timeout=45):
    end = time.time() + timeout
    while time.time() < end:
        try:
            code, _ = http("GET", base + "/api/health", timeout=3)
            if code == 200:
                return True
        except Exception:
            pass
        time.sleep(1)
    return False


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("ARTEX_BASE", "http://127.0.0.1:8787"))
    ap.add_argument("--user", default=os.environ.get("ARTEX_USER", "ARTEX"))
    ap.add_argument("--password", default=os.environ.get("ARTEX_DEMO_PASSWORD", ""))
    ap.add_argument("--no-tasks", action="store_true")
    ap.add_argument("--no-kb", action="store_true")
    ap.add_argument("--no-assets", action="store_true")
    args = ap.parse_args()
    base = args.base.rstrip("/")
    out = {"base": base, "steps": []}

    def step(name, ok, detail):
        out["steps"].append({"name": name, "ok": bool(ok), "detail": detail})
        print(("PASS " if ok else "FAIL ") + name + ": " + str(detail)[:300], flush=True)

    if not args.password:
        print("缺少 --password（或 ARTEX_DEMO_PASSWORD）", file=sys.stderr)
        return 2
    if not wait_health(base):
        step("health", False, "服务不可达：" + base)
        return 1
    step("health", True, base)

    code, st = http("GET", base + "/api/auth/status")
    token = ""
    if code == 200 and isinstance(st, dict) and not st.get("initialized"):
        code, r = http("POST", base + "/api/auth/init", body={"username": args.user, "password": args.password})
        if code not in (200, 201):
            step("init", False, r)
            return 1
        step("init", True, {"user": args.user})
    code, r = http("POST", base + "/api/auth/login", body={"username": args.user, "password": args.password})
    token = r.get("token", "") if isinstance(r, dict) else ""
    if code != 200 or not token:
        step("login", False, {"code": code, "resp": r})
        print("提示：若服务已完成初始化，--password 必须是当前登录密码。", file=sys.stderr)
        return 1
    step("login", True, {"user": args.user})

    demo_task_ids = []
    if not args.no_tasks:
        code, payload = http("GET", base + "/api/tasks?limit=200", token)
        existing = [t for t in items_of(payload) if MARKER in str(t.get("description", "")) or MARKER in str(t.get("name", ""))]
        demo_task_ids = [str(t.get("id")) for t in existing if t.get("id")]
        wanted = [
            (MARKER + " 外网门户基线评估", "对 demo.example.test 门户做信息收集、技术指纹与低风险验证，输出发现与证据。"),
            (MARKER + " 内网资产梳理", "梳理 192.0.2.0/24 示例网段的存活主机与常见服务，标注可利用面。"),
        ]
        created = 0
        for name, goal in wanted:
            if any(name in str(t.get("name", "")) for t in existing):
                continue
            code, r = http("POST", base + "/api/tasks", token, {
                "name": name,
                "description": name,
                "goal": goal,
                "timeout_seconds": 0,
                "seed_first_intent": False,
            })
            tid = r.get("id") if isinstance(r, dict) else None
            if code in (200, 201) and tid:
                demo_task_ids.append(str(tid))
                created += 1
        if demo_task_ids:
            # 没有配置 LLM 时任务会立刻失败；demo 数据以「已暂停」状态展示更稳。
            code, r = http("POST", base + "/api/tasks/control/batch", token,
                           {"task_ids": demo_task_ids, "action": "pause"})
            step("tasks", True, {"created": created, "ids": demo_task_ids, "pause_code": code, "pause": r})
        else:
            step("tasks", True, {"created": 0, "ids": demo_task_ids, "note": "全部已存在"})

    if not args.no_kb:
        entries = [
            {"title": MARKER + " SQL 注入手工验证清单", "category": "ammo", "vuln_class": "sqli",
             "product": "", "cve": "", "content": "1) 单引号/布尔/时间盲注逐级验证；2) 记录请求与回显差异；3) 仅在授权范围内读取最小证明数据；4) 写入发现时附带可复现请求。"},
            {"title": MARKER + " 后台入口优先探测路径", "category": "tactic", "vuln_class": "",
             "product": "", "cve": "", "content": "常见后台路径：/admin /manage /console /login；先指纹识别框架，再按框架默认路径探测；命中后检查默认口令、目录遍历与未授权 API。"},
            {"title": MARKER + " 默认口令快速排查", "category": "tactic", "vuln_class": "",
             "product": "", "cve": "", "content": "产品/框架默认口令表（admin/admin、admin/123456 等）；测试前确认授权与速率；成功后立即记录证据并停止横向尝试。"},
            {"title": MARKER + " 不要反复重放破坏性请求", "category": "negative", "vuln_class": "",
             "product": "", "cve": "", "content": "删除/写入/命令执行类验证：先读后写、先无害命令，避免对生产数据造成不可逆影响；命中即停，禁止批量重放。"},
        ]
        code, payload = http("GET", base + "/api/kb/entries?limit=500", token)
        existing_titles = {str(e.get("title", "")) for e in items_of(payload)}
        added = 0
        for entry in entries:
            if entry["title"] in existing_titles:
                continue
            code, r = http("POST", base + "/api/kb/add", token, entry)
            if code in (200, 201):
                added += 1
        step("kb", added > 0 or all(e["title"] in existing_titles for e in entries),
             {"added": added, "total": len(entries)})

    if not args.no_assets and demo_task_ids:
        code, r = http("POST", base + "/api/assets", token, {
            "task_id": int(demo_task_ids[0]),
            "assets": [
                {"type": "root_domain", "domain": "demo.example.test"},
                {"type": "subdomain", "domain": "www.demo.example.test", "record_type": "A", "record_value": ["192.0.2.10"]},
                {"type": "ip", "ip": "192.0.2.10", "bound_domains": ["www.demo.example.test"]},
                {"type": "service_http", "url": "http://www.demo.example.test/", "technologies": ["nginx", "ARTEX Demo"],
                 "status_code": 200, "content_length": 512, "page_title": "ARTEX Demo Portal"},
            ],
        })
        step("assets", code in (200, 201), {"code": code, "resp": r if code not in (200, 201) else "ok"})

    out["ok"] = all(s["ok"] for s in out["steps"])
    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0 if out["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
