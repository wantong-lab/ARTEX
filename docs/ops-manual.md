# 运维手册：C2 / 代理池 / 保留清理 / 调度与监控

面向运维与值班排障。默认安全优先：C2、危险工具、破坏性能力默认关闭，按需显式开启并确认授权范围。

## 1. C2

### 1.1 启用与监听器

```bash
# 启用（settings c2_enabled + 运行时）
POST /api/c2/enable            {"enabled": true}
GET  /api/c2/status

# 创建监听器：kind=tcp（反向 shell）或 http（beacon）
POST /api/c2/listeners
{"name":"lab","kind":"tcp","bind_host":"127.0.0.1","bind_port":44444}
# 返回一次性 token（列表接口不再回传）

POST /api/c2/listeners/{id}/start
POST /api/c2/listeners/{id}/stop
DELETE /api/c2/listeners/{id}
```

默认只允许回环绑定（127.0.0.1/::1）；`0.0.0.0` 等非回环地址创建/启动都会被 400 拒绝，错误里会提示设置名。确需对外监听：

```bash
PUT /api/settings {"c2_allow_non_loopback": true}
```

开启后请同步检查防火墙/安全组、RoE 授权范围与 C2 事件审计。验收完成后建议立即关回 false。

### 1.2 植入端与会话

```bash
GET  /api/c2/implant?listener_id=1      # 生成植入端（内嵌地址/token/密钥）
GET  /api/c2/sessions                   # 上线会话
GET  /api/c2/tasks?session_id=1
POST /api/c2/sessions/1/tasks   {"task_type":"exec","args":"whoami"}
```

要点：
- token 只在创建时返回一次；丢失就删除监听器重建。
- 目标需能回连 `bind_host:bind_port`；NAT 场景用平台可达地址/端口映射，或经隧道。
- 所有 C2 动作落 c2_events 审计；会话/任务表可直接查询。

### 1.3 排障

| 现象 | 原因 | 处理 |
|---|---|---|
| 创建监听器 400 | 非回环地址未开启开关 | 按提示开 c2_allow_non_loopback（确认授权） |
| start 失败 | 端口占用/权限不足 | 换端口；查看服务日志 last_error |
| 会话不上线 | 目标无法回连/被防火墙拦/token 错 | 目标机 curl/telnet 测试；核对 token 与密钥 |
| 列表无 token | 设计如此（只回 has_token） | 需要时重建监听器 |

## 2. 代理池与统一出站

### 2.1 录入与探测

```bash
POST /api/proxy-pool/entries {"url":"socks5://user:pass@host:1080"}
POST /api/proxy-pool/{id}/probe
POST /api/proxy-pool/{id}/switch
POST /api/proxy-pool/fallback
DELETE /api/proxy-pool/{id}
GET  /api/proxy-pool
```

- 支持 http/https/socks5/socks5h；同 scheme+host+port 自动去重。
- URL（含认证）AES-GCM 加密落库（gcm1:），列表只展示 host:port 与认证有无。
- 探测访问 `probe_url`（默认 http://127.0.0.1:18081/ip）；失败置 failed、累计 fail_count。
- 健康巡检由维护调度器 `proxy_health` 任务执行（默认开，30 分钟），UI 显示 healthy/failed。

### 2.2 全局出口与统一出站

```bash
PUT  /api/proxy-pool/global   {"url":"http://127.0.0.1:7890"}   # 空字符串=直连
POST /api/proxy-pool/egress                                    # 验证当前出口 IP
```

- 全局出口代理作用于平台出站流量（不改变目标流量 MITM 代理）。
- `unified_outbound` 表示哪些外部数据源走统一出站（如 sploitus、cve_sync）；改完立即生效。
- MITM 录制代理与出口代理是两套概念：录制用 `-proxy` 启动参数，出口用本设置。

### 2.3 mihomo 导入

```bash
POST /api/proxy-pool/import-mihomo
{"yaml":"proxies:\n  - {name: p1, type: http, server: 1.2.3.4, port: 8080}\n"} 
```

支持 http/https/socks5（socks5 带 auth）；ss/vmess 等不支持的类型会被跳过并在返回 skipped 中标注。导入前建议先在测试库验证。

### 2.4 排障

| 现象 | 原因 | 处理 |
|---|---|---|
| 探测失败但代理可用 | probe_url 不可达/需鉴权 | 确认目标 URL 与代理认证 |
| healthy 一直 0 | 巡检任务未跑/全部失败 | 看 /api/scheduler/status 的 proxy_health 与 /metrics |
| 导入 0 条 | YAML 格式/类型不支持 | 检查 proxies 结构与 type；看 skipped |
| 全局出口不生效 | 缓存/任务未重建 | 保存设置后新任务生效；检查 unified_outbound |

## 3. 保留清理（retention）

### 3.1 策略与手动执行

```bash
GET  /api/retention/status
PUT  /api/retention/policy    {"enabled":true,"interval_hours":24,"days":30}
GET  /api/retention/cleanup-list          # 按 host_ledger 生成的目标侧清理清单
POST /api/retention/cleanup   {"dry_run":true}   # 先看影响面
POST /api/retention/cleanup   {"dry_run":false}  # 真删
```

- 自动清理由维护调度器 `retention` 任务执行（setting `retention_auto_enabled` / `retention_auto_interval_hours`）。
- cleanup-list 来源于 host_ledger 中 artifact=true 的行（落盘文件/账号/key/服务等），带 cleanup_cmd 用于目标侧回滚复核。
- 清理的是平台侧留存数据（会话/活动/流量等按策略）；目标侧痕迹需按 cleanup_cmd 人工/脚本执行。
- 删除前务必 dry_run；证据（findings、报告、导出）优先保留。
- 清理动作本身写 host_ledger，可用审计校验接口验证链完整。

### 3.2 调度与监控

```bash
GET  /api/scheduler/status          # 三个任务：retention / cve_autosync / proxy_health
POST /api/scheduler/run/retention   # 手动触发
GET  /api/observability/status      # 汇总：任务计数/DB/调度/HTTP
GET  /metrics                       # artex_scheduler_job_* 指标（默认仅回环）
```

`ARTEX_SCHEDULER_TICK_SECONDS` 控制检查粒度（默认 60s，范围 1–3600）。配置改动每分钟重读，不需要重启。

| 现象 | 原因 | 处理 |
|---|---|---|
| 任务一直 enabled=0 | settings 未开 | PUT /api/retention/policy 或对应开关 |
| last_error 非空 | 上次执行失败 | /api/scheduler/status 看 last_result/last_error，手动 run 复现 |
| 到点不跑 | tick 未到/任务 running | 看 next_run_unix；长任务 10 分钟超时 |
| 清理后仍有数据 | 策略范围外/证据目录 | 检查 cleanup-list 与证据保留规则 |

## 4. 升级、回滚与备份

完整步骤见 `docs/deploy-v0.4.0.md`；自动化演练脚本：

```bash
bash scripts/acceptance/upgrade_rollback.sh
```

- 升级前备份 PostgreSQL（pg_dump -Fc）与 data 目录；迁移只向前、回滚必须恢复备份。
- 迁移记录在 schema_migrations，当前最新 0012_audit_chain。
- 密钥轮换（服务停止时）：`./artex-rotate-key -key-dir ./ -data ./data -dry-run`，确认后去掉 -dry-run；轮换会重加密会话/凭据/代理/C2 token，并使已登录 token 失效。

## 5. 安全与审计

- /metrics 默认回环；跨机抓取设置 `ARTEX_METRICS_TOKEN` 并带 `X-Metrics-Token`。
- 结构化日志：`ARTEX_LOG_FORMAT=json`；访问日志不含 query。
- 审计完整性：`GET /api/security/audit/verify` 返回 ok/total/hashed/broken_at；broken_at 非 0 说明历史台账被改/删，立即排查。
- 全局拦截规则、RoE 范围同时覆盖 planner/worker/chat 的工具调用（P2-13 起）。
