#!/usr/bin/env python3
"""P3-18 LLM 真实失败转移 + 流式探测验收。

依赖数据库中已有一个可用的激活 LLM profile（干净环境先用 UI/API 配好）。

流程：
  1. 创建一条坏的 openai profile（base_url 指向不可达端口，pool_exclude=true）；
  2. 创建任务并使用有序 profile 链 [坏配置, 好配置] + seed_first_intent，验证：
     坏配置建连失败后任务能切到好配置并真实执行命令，输出标记；
  3. 探测流式 SSE（/api/llm/test streaming=true，超时即失败）与同配置非流式；
  4. 删除临时坏配置。

用法：
  python3 scripts/acceptance/llm_failover.py --base http://127.0.0.1:18911 \
      --password 'Accept-Artex-2026!'
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

TERMINAL = ("done", "error", "failed", "stopped", "cancelled", "canceled")
BAD_NAME = "p318-bad-openai"


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
            return e.code, {"error": raw[:300]}


def items(payload, *keys):
    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict):
        for k in keys:
            v = payload.get(k)
            if isinstance(v, list):
                return v
        for v in payload.values():
            if isinstance(v, list):
                return v
    return []


def text_of(v):
    try:
        return json.dumps(v, ensure_ascii=False)
    except Exception:
        return str(v)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("ARTEX_BASE", "http://127.0.0.1:18911"))
    ap.add_argument("--user", default=os.environ.get("ARTEX_USER", "ARTEX"))
    ap.add_argument("--password", default=os.environ.get("ARTEX_PASSWORD", "Accept-Artex-2026!"))
    ap.add_argument("--timeout", type=int, default=300)
    ap.add_argument("--skip-stream", action="store_true")
    args = ap.parse_args()
    base = args.base.rstrip("/")
    steps = []

    def step(name, ok, detail):
        steps.append({"name": name, "ok": bool(ok), "detail": detail})
        print(("PASS " if ok else "FAIL ") + name + ": " + str(detail)[:260], flush=True)

    code, body = http("POST", base + "/api/auth/login", body={"username": args.user, "password": args.password})
    token = body.get("token", "") if isinstance(body, dict) else ""
    if not token:
        step("login", False, body)
        return 1
    step("login", True, {"user": args.user})

    code, payload = http("GET", base + "/api/llm/profiles", token)
    profiles = items(payload, "profiles")
    good = None
    for p in profiles:
        if p.get("is_default"):
            good = p
            break
    if not good:
        step("good-profile", False, "没有 is_default=true 的可用 profile")
        return 1
    good_id = int(good["id"])
    step("good-profile", True, {"id": good_id, "model": good.get("model"), "streaming": good.get("streaming")})

    # 清理同名旧坏配置
    for p in profiles:
        if str(p.get("name", "")) == BAD_NAME:
            http("DELETE", base + "/api/llm/profiles/" + str(p["id"]), token)
    code, bad = http("POST", base + "/api/llm/profiles", token, {
        "name": BAD_NAME,
        "format": "openai",
        "base_url": "http://127.0.0.1:9/v1",
        "model": "bad-model",
        "api_key": "bad-key",
        "streaming": False,
        "priority": 1,
        "pool_exclude": True,
    })
    bad_id = int(bad.get("id") or bad.get("profile", {}).get("id") or 0) if isinstance(bad, dict) else 0
    if not bad_id:
        # 列表回查同名
        code, payload = http("GET", base + "/api/llm/profiles", token)
        for p in items(payload, "profiles"):
            if str(p.get("name", "")) == BAD_NAME:
                bad_id = int(p["id"])
    step("bad-profile-create", bool(bad_id), {"id": bad_id, "code": code})
    if not bad_id:
        return 1

    marker = "ARTEX-P318-FAILOVER"
    goal = "请直接执行命令 echo " + marker + "，确认输出包含该标记后结束任务。"
    code, task = http("POST", base + "/api/tasks", token, {
        "name": "P3-18 LLM failover",
        "description": "P3-18 LLM 失败转移验收",
        "goal": goal,
        "llm_profile_ids": [bad_id, good_id],
        "seed_first_intent": True,
        "timeout_seconds": 0,
    })
    tid = str(task.get("id")) if isinstance(task, dict) and task.get("id") else ""
    step("task-create-chain", bool(tid), {"task_id": tid, "code": code})
    if not tid:
        http("DELETE", base + "/api/llm/profiles/" + str(bad_id), token)
        return 1

    final = {}
    end = time.time() + args.timeout
    while time.time() < end:
        code, final = http("GET", base + "/api/tasks/" + tid, token)
        st = str(final.get("status", "")).lower() if isinstance(final, dict) else ""
        if st in TERMINAL:
            break
        time.sleep(3)
    status = str(final.get("status", "")).lower() if isinstance(final, dict) else ""
    step("task-terminal", status == "done", {"task_id": tid, "status": status,
         "active_llm_profile_id": final.get("active_llm_profile_id"),
         "llm_failover_state": final.get("llm_failover_state"),
         "llm_failover_reason": final.get("llm_failover_reason")})

    # 活动历史里找标记的真实执行（Bash tool_result），而不是提示词文本。
    executed = False
    hit_sessions = []
    code, intents = http("GET", base + "/api/exploration/intents?task=" + tid, token)
    sessions = ["plan"] + ["intent:" + str(i.get("id")) for i in items(intents, "intents") if i.get("id")]
    for sess in sessions:
        code, h = http("GET", base + "/api/exploration/activity/history?task=" + tid + "&session=" + urllib.parse.quote(sess) + "&limit=400", token)
        for item in items(h, "items", "activities"):
            if not isinstance(item, dict):
                continue
            if item.get("kind") == "tool_result" and str(item.get("tool", "")) in ("Bash", "shell_open", "shell_send") and marker in text_of(item.get("summary", "")):
                executed = True
                hit_sessions.append(sess)
    step("failover-marker-executed", executed, {"sessions": hit_sessions})

    # 失败转移证据：任务不应再绑定坏配置（若字段暴露则校验）
    active = final.get("active_llm_profile_id") if isinstance(final, dict) else None
    step("failover-active-profile", active in (None, "", good_id) or str(active) == str(good_id),
         {"active": active, "good": good_id, "bad": bad_id})

    if not args.skip_stream:
        probe = {
            "provider": good.get("format") or "openai",
            "model": good.get("model", ""),
            "base_url": good.get("base_url", ""),
            "profile_id": good_id,
            "session_header_key": good.get("session_header_key", ""),
            "streaming": True,
        }
        start = time.time()
        code, resp = http("POST", base + "/api/llm/test", token, probe, timeout=90)
        elapsed = round(time.time() - start, 1)
        stream_ok = code == 200 and isinstance(resp, dict) and resp.get("ok") is not False and not resp.get("error")
        step("streaming-sse", stream_ok, {"code": code, "elapsed_s": elapsed, "resp": resp if not stream_ok else "ok"})
        probe["streaming"] = False
        start = time.time()
        code, resp = http("POST", base + "/api/llm/test", token, probe, timeout=90)
        elapsed = round(time.time() - start, 1)
        step("nonstreaming", code == 200 and not (isinstance(resp, dict) and resp.get("error")),
             {"code": code, "elapsed_s": elapsed, "err": resp.get("error") if isinstance(resp, dict) else resp})

    http("DELETE", base + "/api/llm/profiles/" + str(bad_id), token)
    step("cleanup-bad-profile", True, {"id": bad_id})
    ok = all(s["ok"] for s in steps)
    print(json.dumps({"ok": ok, "steps": steps}, ensure_ascii=False, indent=2))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
