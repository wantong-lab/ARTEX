# v0.4.0 部署与升级指南

本文覆盖从 0.3.x 升级到 v0.4.0 的步骤、新增开关与运维命令。默认值以「安全优先」为原则，破坏性/后渗透能力默认关闭。

## 1. 前置条件

- PostgreSQL 15+；建议使用 pgvector 镜像（pgvector/pgvector:pg15）。没有 vector 扩展时语义记忆自动降级为关键词召回，不阻塞启动。
- Go 1.26（源码构建）或直接使用 Release 二进制（五平台）。
- 前端已随二进制内嵌（-tags embedui），无需单独部署 Node。

## 2. 升级步骤

1. 备份数据库（pg_dump）。
2. 停止旧进程，替换二进制 / Docker 镜像到 v0.4.0。
3. 启动。启动时自动应用 0004–0012 迁移（内网台账、待审批范围、语义记忆扩展、C2 台账、代理池、清理审计、审计哈希链）；迁移按文件事务执行，记录在 schema_migrations。
4. 打开 Web 控制台确认「任务 / 内网中枢 / 情报」页面可用。
5. 如需 C2：先配置 settings 再显式启用（见第 4 节）。

命令行检查：

    ./artex -h
    psql "$ARTEX_PG_DSN" -c "select version from schema_migrations order by version"

预期最后一条版本为 0012_audit_chain。

## 3. 新增环境变量 / 设置

| 键 | 默认 | 说明 |
|---|---|---|
| roe_scope_mode | warn | 范围执行器：off / warn / strict |
| c2_enabled | off | C2 总开关；off 时不监听任何端口，c2_* 工具拒绝执行 |
| c2_token | 自动生成 | 植入端握手 token（API 不回显） |
| cve_autosync | off | 预留开关；同步由 /api/cve/sync 手动触发 |
| proxy_global_url | 空 | 全局兜底代理（活动代理失败时回退；空=直连） |
| proxy_active_id | 0 | 当前活动代理 id（随 /api/proxy-pool/{id}/switch 持久化） |
| proxy_probe_url | https://api.ipify.org?format=json | 出口探测地址 |
| retention_activity_days | 30 | activity 保留天数 |
| retention_logs_days | 14 | 后端日志保留天数 |
| retention_c2_days | 30 | C2 台账保留天数 |

语义记忆：ARTEX_EMBED_URL / ARTEX_EMBED_MODEL / ARTEX_EMBED_DIM（embedder 不可达时只存不向量化，恢复后回填）。

## 4. 启用 C2（默认关闭）

1. 在「系统设置」或直接写 settings 表把 c2_enabled 置 on。
2. 调用 POST /api/c2/enable {"enabled":true}（或重启时自动按台账拉起监听器）。
3. 创建监听器：POST /api/c2/listeners {"name":"lab","bind_host":"127.0.0.1","bind_port":44444}，返回一次性 token。
4. 生成植入端：payload 模板见 c2/payload.go（Python 最小实现），内嵌监听地址/token/密钥。
5. 会话上线后可用 /api/c2/sessions 查看，或让 agent 使用 c2_list_sessions / c2_exec。
6. 停用：POST /api/c2/enable {"enabled":false}，会取消监听并停止接受新会话。

注意：C2 监听器默认只允许回环绑定（127.0.0.1/::1）；0.0.0.0 等非回环地址会被 400 拒绝，确需对外监听须先在设置中显式开启 c2_allow_non_loopback（并确认 RoE 与授权范围）。C2 动作全部落 c2_events 审计。

## 5. 代理池

- 录入：POST /api/proxy-pool/entries {"url":"http://user:pass@host:port"}（支持 http/https/socks5/socks5h，自动去重、URL 加密落库）。
- 探测：POST /api/proxy-pool/{id}/probe → 出口 IP 与延迟；失败置 failed。
- 切换：POST /api/proxy-pool/{id}/switch；切回兜底：POST /api/proxy-pool/fallback。
- 自检：POST /api/proxy-pool/egress → 当前出口 IP（失败自动回退全局/直连）。
- 代理池 client 已注入 Sploitus 检索；其他出站路径后续统一接入。

## 6. 清理与保留

- 预览：GET /api/retention/status → 策略 + 各表到期条数 + 最近审计。
- Dry-run：POST /api/retention/cleanup {"dry_run":true} → 只回 counts，不删数据、不写审计。
- 真实清理：POST /api/retention/cleanup {"dry_run":false,"actor":"ops"} → 单事务删除到期数据并写 retention_audit。
- 清理清单：GET /api/retention/cleanup-list → 按 host_ledger 的 artifact 行生成目标侧清理清单（按 host 聚合）。
- 修改策略：PUT /api/retention/policy {"activity_days":30,"logs_days":14,"c2_days":30}（1..3650，非法值落默认）。

## 7. 回滚

- 迁移不可逆：回滚前必须从备份恢复数据库。
- 二进制可直接回退到 0.3.x，但新表（0004–0012）会保留，旧版本忽略它们。

## 8. 故障排查

| 现象 | 处理 |
|---|---|
| 启动报 cve.db 不存在 | 属正常降级：CVE 检索不可用，其他功能不受影响；如需语料请放置 data/cve/cve.db 或调用 /api/cve/sync |
| 语义记忆不生效 | 检查 ARTEX_EMBED_URL 可达；无 pgvector 时自动关键词降级，日志会有 NOTICE |
| C2 工具报「未启用」 | 需要 settings c2_enabled=on 且配置监听器，见第 4 节 |
| 代理探测全部失败 | 检查出网与代理认证；失败条目会标 failed，切换会被拒绝，先处理再切换 |
| 升级后前端 404 | 确认二进制用 -tags embedui 构建（Release 包默认内嵌） |



## 9. 安全审计与密钥轮换（P2-13）

- 密文静置：会话 secret、凭据 secret、代理池 URL、C2 监听器 token 均以 AES-256-GCM 落库（gcm1: 前缀），密钥由 master key（keyDir/jwt.key）按子系统域分离派生；DB 中不出现明文。
- C2 非回环：默认拒绝非回环绑定；开启 settings.c2_allow_non_loopback=true 后放行，创建/启动监听器两处都会复核。
- 审计完整性：host_ledger 自 0012 迁移起写入 prev_hash/entry_hash 哈希链；GET /api/security/audit/verify 返回 ok/total/hashed/broken_at，篡改历史行会精确报出断链行号（历史无哈希的旧行自动跳过）。
- planner 工具调用治理：planner 与 worker/chat 一样挂载任务 Guard，全局拦截规则、RoE 范围与审计覆盖 planner 自己的 Bash/Write 等工具调用。
- master key 轮换（服务停止时执行；jwt.key 同时用于 JWT 签名，轮换后所有登录 token 失效）：

```bash
# 预检：解密旧数据并用新 key 重封后回滚，不写库不换 key
ARTEX_PG_DSN='postgres://...' ./artex-rotate-key -key-dir ./ -data ./data -dry-run
# 正式轮换：事务内重加密 sessions/credentials/proxy_pool/c2_listeners，成功后原子替换 jwt.key
ARTEX_PG_DSN='postgres://...' ./artex-rotate-key -key-dir ./ -data ./data
```

失败时数据库事务自动回滚、旧 key 不清除；只有数据库提交成功才写新 key。


## 10. 监控与可观测性（P2-14）

- GET /metrics：Prometheus 文本格式，默认仅允许本机回环抓取；跨机抓取设置 ARTEX_METRICS_TOKEN 环境变量，并在请求头 X-Metrics-Token 携带同值。
  指标包含：artex_up / artex_build_info / artex_uptime_seconds；artex_http_requests_total{method,code}、artex_http_request_duration_seconds_sum/count{method}、artex_http_in_flight_requests；artex_tasks{status} 与 artex_tasks_total；artex_db_connections{state} 与 artex_db_max_open_connections；artex_llm_active_calls；artex_scheduler_job_enabled/running/interval_seconds/last_run_timestamp_seconds/last_error{job}（保留清理、CVE 同步、代理健康巡检）；artex_go_goroutines / artex_go_mem_alloc_bytes。
- 结构化日志：ARTEX_LOG_FORMAT=json 时，HTTP 访问日志与维护调度任务事件输出单行 JSON（ts/level/msg + method/path/status/duration_ms/bytes/remote 等字段）；不记录 query string（避免 token 进日志）。默认文本模式保持原样；文本模式下可用 ARTEX_ACCESS_LOG=1 打开访问日志。
- GET /api/observability/status（需登录）：返回版本、uptime、任务状态计数、DB 连接池、调度任务、HTTP 摘要与当前日志格式，用于运维排障看板。


## 11. 延伸阅读

- [LLM 配置指南](llm-config.md)：协议格式、profile 字段、opencode-go/session_header_key、流式/非流式、失败转移与五层重试、用量记录、排障。
- [运维手册：C2 / 代理池 / 保留清理 / 调度与监控](ops-manual.md)：命令、开关、审计与常见故障。
- [FAQ](faq.md)：LLM、C2、代理池、清理、部署升级、监控审计、治理拦截的实战问答。
