#!/usr/bin/env python3
"""P2-14 可观测性验收脚本。

对运行中的服务（建议以 ARTEX_LOG_FORMAT=json 启动）验证：
  1. /metrics（Prometheus 文本）：artex_up / build_info / http 计数 / 任务与调度指标 /
     DB 连接 / Go runtime；请求计数随真实请求增长；
  2. GET /api/observability/status：版本、uptime、任务计数、DB、调度任务、HTTP 摘要；
  3. 结构化访问日志：--log-file 指定的 stderr 日志中存在 JSON 行（http_request 字段）。

用法：
  python3 scripts/acceptance/observability_check.py --base http://127.0.0.1:18907 \
      --password 'Accept-Artex-2026!' --log-file /tmp/artex-obs.log
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request


def http(method, url, token=None, body=None, timeout=30):
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
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def parse_metric(text, name):
    """返回 name 的所有 (labels, value)（仅精确指标名）。"""
    out = []
    for line in text.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        m = re.match(r"^([a-zA-Z0-9_:]+)(\{[^}]*\})?\s+(\S+)$", line)
        if not m or m.group(1) != name:
            continue
        labels = m.group(2) or ""
        try:
            value = float(m.group(3))
        except ValueError:
            continue
        out.append((labels, value))
    return out


def total_requests(text):
    return sum(v for _, v in parse_metric(text, "artex_http_requests_total"))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("ARTEX_BASE", "http://127.0.0.1:18907"))
    ap.add_argument("--user", default=os.environ.get("ARTEX_USER", "ARTEX"))
    ap.add_argument("--password", default=os.environ.get("ARTEX_PASSWORD", "Accept-Artex-2026!"))
    ap.add_argument("--log-file", default=os.environ.get("ARTEX_LOG_FILE", ""))
    args = ap.parse_args()
    base = args.base.rstrip("/")
    result = {"base": base, "steps": [], "ok": False}

    def step(name, ok, detail):
        result["steps"].append({"name": name, "ok": bool(ok), "detail": str(detail)[:500]})
        print(("PASS " if ok else "FAIL ") + name + ": " + str(detail)[:240], flush=True)

    code, body = http("POST", base + "/api/auth/login", body={"username": args.user, "password": args.password})
    token = ""
    try:
        token = json.loads(body).get("token", "")
    except Exception:
        pass
    if not token:
        step("login", False, body[:200])
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 1
    step("login", True, {"user": args.user})

    # --- /metrics 基础集合 ---
    code, metrics = http("GET", base + "/metrics")
    required = ["artex_up", "artex_build_info", "artex_http_in_flight_requests", "artex_go_goroutines", "artex_go_mem_alloc_bytes", "artex_tasks_total"]
    missing = [m for m in required if not parse_metric(metrics, m)]
    step("metrics-basics", code == 200 and not missing,
         {"code": code, "missing": missing, "content_type_head": metrics[:60]})
    step("metrics-build-info", any('version=' in labels for labels, _ in parse_metric(metrics, "artex_build_info")),
         {"build_info": parse_metric(metrics, "artex_build_info")[:1]})
    step("metrics-go-runtime", bool(parse_metric(metrics, "artex_go_goroutines")), {})

    # 触发若干真实请求后，计数器必须增长。
    before = total_requests(metrics)
    for _ in range(5):
        http("GET", base + "/api/health")
    time.sleep(0.2)
    code, metrics2 = http("GET", base + "/metrics")
    after = total_requests(metrics2)
    step("metrics-counter-growth", code == 200 and after >= before + 5, {"before": before, "after": after})
    step("metrics-task-gauge", bool(parse_metric(metrics2, "artex_tasks_total")),
         {"total": parse_metric(metrics2, "artex_tasks_total"), "by_status": parse_metric(metrics2, "artex_tasks")[:5]})
    jobs = parse_metric(metrics2, "artex_scheduler_job_enabled")
    step("metrics-scheduler", len(jobs) >= 3, {"jobs": len(jobs), "sample": jobs[:3]})
    dbconn = parse_metric(metrics2, "artex_db_connections")
    step("metrics-db-pool", len(dbconn) >= 3, {"states": dbconn})

    # --- observability status JSON ---
    code, body = http("GET", base + "/api/observability/status", token)
    status = {}
    try:
        status = json.loads(body)
    except Exception:
        pass
    need = ["version", "uptime_secs", "tasks", "db", "scheduler", "http", "log_format"]
    missing = [k for k in need if k not in status]
    step("observability-status", code == 200 and not missing,
         {"code": code, "missing": missing, "log_format": status.get("log_format"),
          "uptime": status.get("uptime_secs"), "scheduler_jobs": len(status.get("scheduler") or [])})

    # --- 结构化访问日志 ---
    if args.log_file:
        found = None
        try:
            with open(args.log_file, "r", errors="replace") as f:
                lines = f.readlines()[-400:]
            for line in reversed(lines):
                line = line.strip()
                if not line.startswith("{"):
                    continue
                try:
                    rec = json.loads(line)
                except Exception:
                    continue
                if rec.get("msg") == "http_request" and "path" in rec and "status" in rec:
                    found = rec
                    break
        except OSError as e:
            found = {"error": str(e)}
        step("structured-access-log", isinstance(found, dict) and found.get("msg") == "http_request",
             {"record": found})
    else:
        step("structured-access-log", True, "skipped: --log-file 未提供")

    result["ok"] = all(s["ok"] for s in result["steps"])
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
