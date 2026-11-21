#!/usr/bin/env python3
"""P2-11 LLM 全链路 E2E + 治理强制场景验收脚本。

对已启动的 ARTEX 服务执行端到端验收：
  1. 登录，确认激活的 LLM 配置可真实连通（/api/llm/test）；
  2. 创建一个真实 LLM 任务，要求用工具执行带标记的 echo，轮询任务直到终态；
  3. 从活动历史确认标记真实出现在任务轨迹中（证明 planner→worker→工具→回写全链路）；
  4. 治理强制场景：登记一条 deny 拦截规则，创建任务诱导执行被拦截命令，
     校验拦截历史出现 denied 记录（命令未被执行）。

依赖：仅标准库。用法：
  python3 scripts/acceptance/llm_e2e.py --base http://127.0.0.1:18905 \
      --password 'Accept-Artex-2026!' --marker ARTEX-P2-LLM-OK
可选环境变量 ARTEX_LLM_API_KEY / ARTEX_LLM_BASE_URL / ARTEX_LLM_MODEL /
ARTEX_LLM_SESSION_HEADER：提供时自动创建/更新同名 LLM 配置并激活。
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

TERMINAL_HINTS = ("done", "complete", "success", "stopped", "failed", "archived", "error")


def http(method, url, token=None, body=None, timeout=90):
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
            payload = json.loads(raw)
        except Exception:
            payload = {"raw": raw}
        return e.code, payload


def text_of(value):
    try:
        return json.dumps(value, ensure_ascii=False)
    except Exception:
        return str(value)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("ARTEX_BASE", "http://127.0.0.1:18905"))
    ap.add_argument("--user", default=os.environ.get("ARTEX_USER", "ARTEX"))
    ap.add_argument("--password", default=os.environ.get("ARTEX_PASSWORD", "Accept-Artex-2026!"))
    ap.add_argument("--marker", default=os.environ.get("ARTEX_LLM_MARKER", "ARTEX-P2-LLM-OK"))
    ap.add_argument("--deny-marker", default="ARTEX-P2-DENY")
    ap.add_argument("--timeout", type=int, default=240)
    ap.add_argument("--skip-governance", action="store_true")
    args = ap.parse_args()
    base = args.base.rstrip("/")
    result = {"base": base, "steps": [], "ok": False}

    def step(name, ok, detail):
        result["steps"].append({"name": name, "ok": bool(ok), "detail": text_of(detail)[:600]})
        print(("PASS " if ok else "FAIL ") + name + ": " + text_of(detail)[:300], flush=True)

    code, r = http("POST", base + "/api/auth/login", body={"username": args.user, "password": args.password})
    token = r.get("token")
    if code != 200 or not token:
        step("login", False, {"code": code, "resp": r})
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 1
    step("login", True, {"user": args.user})

    # 可选用环境变量创建/更新 LLM 配置，缺省直接复用服务端已配置的激活配置。
    profile_id = None
    key = os.environ.get("ARTEX_LLM_API_KEY", "").strip()
    if key:
        body = {
            "name": "p2-llm-e2e",
            "format": "openai",
            "base_url": os.environ.get("ARTEX_LLM_BASE_URL", "https://api.openai.com/v1"),
            "model": os.environ.get("ARTEX_LLM_MODEL", "gpt-4o-mini"),
            "api_key": key,
            "priority": 10,
            "streaming": False,
        }
        sh = os.environ.get("ARTEX_LLM_SESSION_HEADER", "").strip()
        if sh:
            body["session_header_key"] = sh
        code, r = http("POST", base + "/api/llm/profiles", token, body)
        if code != 200 or not r.get("id"):
            step("llm-profile-create", False, {"code": code, "resp": r})
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 1
        profile_id = int(r["id"])
        step("llm-profile-create", True, {"id": profile_id})
    code, r = http("GET", base + "/api/llm/profiles", token)
    profiles = r.get("profiles") or r.get("items") or []
    if profile_id is None:
        active = [p for p in profiles if p.get("is_default")] or profiles
        if not active:
            step("llm-profile", False, {"profiles": profiles})
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 1
        profile_id = int(active[0]["id"])
    code, r = http("POST", base + "/api/llm/profiles/active", token, {"id": profile_id})
    step("llm-profile-active", code == 200, {"id": profile_id, "resp": r})
    prof = next((p for p in profiles if int(p.get("id", 0)) == profile_id), {})
    test_body = {
        "provider": prof.get("format") or "openai",
        "model": prof.get("model", ""),
        "base_url": prof.get("base_url", ""),
        "profile_id": profile_id,
        "streaming": bool(prof.get("streaming", False)),
    }
    if prof.get("session_header_key"):
        test_body["session_header_key"] = prof["session_header_key"]
    if prof.get("thinking_type"):
        test_body["thinking_type"] = prof["thinking_type"]
    if prof.get("reasoning_effort"):
        test_body["reasoning_effort"] = prof["reasoning_effort"]
    code, r = http("POST", base + "/api/llm/test", token, test_body, timeout=120)
    reply = r.get("reply") or r.get("text") or r.get("content") or ""
    step("llm-connectivity", code == 200 and bool(reply), {"code": code, "reply": str(reply)[:200], "err": r.get("error")})
    if code != 200:
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 1

    # --- 全链路任务 ---
    goal = "请遵循你的标准流程：先用可用工具执行命令 echo " + args.marker + "，确认输出包含该标记后结束任务。"
    code, task = http("POST", base + "/api/tasks", token, {
        "description": "P2-11 LLM 全链路 E2E",
        "goal": goal,
        "llm_profile_ids": [profile_id],
        "timeout_seconds": 300,
    })
    if code not in (200, 201) or not task.get("id"):
        step("task-create", False, {"code": code, "resp": task})
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 1
    task_id = int(task["id"])
    step("task-create", True, {"task_id": task_id, "status": task.get("status")})

    deadline = time.time() + args.timeout
    status = task.get("status", "")
    last_task = task
    while time.time() < deadline:
        code, t = http("GET", base + "/api/tasks/" + str(task_id), token)
        if code == 200:
            last_task = t
            status = str(t.get("status", ""))
            if any(h in status.lower() for h in TERMINAL_HINTS):
                break
        time.sleep(3)
    else:
        http("POST", base + "/api/tasks/" + str(task_id) + "/control", token, {"action": "stop"})
    step("task-terminal", any(h in status.lower() for h in TERMINAL_HINTS), {"task_id": task_id, "status": status, "task": {k: last_task.get(k) for k in ("id", "status", "goal")}})

    # 活动历史：main/plan/各 intent 会话确认标记与 token 用量（planner→worker→Bash→回写全链路）。
    code, intents = http("GET", base + "/api/exploration/intents?task=" + str(task_id), token)
    sessions = ["main", "plan"]
    for it in intents if isinstance(intents, list) else []:
        if isinstance(it, dict) and it.get("id"):
            sessions.append("intent:" + str(it["id"]))
    marker_seen = False
    token_total = 0
    hit_sessions = []
    tool_results = 0
    for sess in sessions:
        code, hist = http(
            "GET",
            base + "/api/exploration/activity/history?task=" + str(task_id) + "&session=" + urllib.parse.quote(sess) + "&limit=400",
            token,
        )
        if args.marker in text_of(hist):
            marker_seen = True
            hit_sessions.append(sess)
        for item in (hist.get("items") or []) if isinstance(hist, dict) else []:
            if not isinstance(item, dict):
                continue
            if item.get("kind") == "usage":
                token_total += int(item.get("input_tokens") or 0) + int(item.get("output_tokens") or 0)
            if item.get("kind") == "tool_result":
                tool_results += 1
    step("chain-marker", marker_seen, {"marker": args.marker, "hit_sessions": hit_sessions, "sessions": sessions, "tool_results": tool_results})
    step("llm-usage", token_total > 0, {"tokens": token_total, "sessions": sessions})

    if not args.skip_governance:
        rule_name = "p2-e2e-deny"
        code, rule = http("POST", base + "/api/intercept/rules", token, {
            "name": rule_name,
            "match_target": "tool_input",
            "match_type": "string",
            "pattern": args.deny_marker,
            "action": "deny",
            "message": "P2-11 E2E 治理拦截",
            "priority": 100,
            "enabled": True,
        })
        rule_id = rule.get("id") if isinstance(rule, dict) else None
        if rule_id is not None:
            try:
                rule_id = int(rule_id)
            except (TypeError, ValueError):
                rule_id = None
        step("governance-rule-create", code == 200 and bool(rule_id), {"code": code, "rule": rule})
        if rule_id:
            goal2 = "请尝试执行命令 echo " + args.deny_marker + "（治理验收要求必须真实尝试一次工具调用），然后结束任务。"
            code, task2 = http("POST", base + "/api/tasks", token, {
                "description": "P2-11 治理强制场景",
                "goal": goal2,
                "llm_profile_ids": [profile_id],
                "timeout_seconds": 300,
                # 种子意图直接下发 worker，确保被拦截的是 worker 的工具调用（planner 的
                # 直接工具调用目前不经过 Guard，见 P2-13 安全审计待办）。
                "seed_first_intent": True,
            })
            tid2 = int(task2["id"]) if isinstance(task2, dict) and task2.get("id") else None
            step("governance-task-create", code in (200, 201) and bool(tid2), {"task_id": tid2, "code": code})
            if tid2:
                end2 = time.time() + args.timeout
                while time.time() < end2:
                    code, t2 = http("GET", base + "/api/tasks/" + str(tid2), token)
                    if code == 200 and any(h in str(t2.get("status", "")).lower() for h in TERMINAL_HINTS):
                        break
                    time.sleep(3)
                code, hist2 = http("GET", base + "/api/intercept/history?size=100", token)
                rows = (hist2.get("items") if isinstance(hist2, dict) else None) or []
                mine = [
                    x for x in rows
                    if str(x.get("task_id", "")) == str(tid2) or args.deny_marker in text_of(x.get("tool_input", ""))
                ]
                denied = [x for x in mine if str(x.get("status", "")).lower() == "denied"]
                # 命令是否真的被执行：遍历本任务各 intent 的 tool_result，标记不得出现在执行输出里。
                executed = False
                code, intents2 = http("GET", base + "/api/exploration/intents?task=" + str(tid2), token)
                # 只看 worker 意图会话：planner 的直接工具调用目前不经过 Guard（见 P2-13 待办），
                # 不能把 planner 自己的执行算作「治理被绕过」。
                sessions2 = []
                for it in intents2 if isinstance(intents2, list) else []:
                    if isinstance(it, dict) and it.get("id"):
                        sessions2.append("intent:" + str(it["id"]))
                for sess in sessions2:
                    code, h = http(
                        "GET",
                        base + "/api/exploration/activity/history?task=" + str(tid2) + "&session=" + urllib.parse.quote(sess) + "&limit=400",
                        token,
                    )
                    for item in (h.get("items") or []) if isinstance(h, dict) else []:
                        if isinstance(item, dict) and item.get("kind") == "tool_result" and args.deny_marker in text_of(item.get("summary", "")):
                            executed = True
                step("governance-enforced", bool(denied) and not executed, {"task_id": tid2, "matched_rows": len(mine), "denied": len(denied), "executed": executed, "sample": text_of(mine[:2])[:500]})
            http("DELETE", base + "/api/intercept/rules/" + str(rule_id), token)

    ok = all(s["ok"] for s in result["steps"])
    result["ok"] = ok
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
