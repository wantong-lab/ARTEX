# 上游导入登记

规则：只复制原源包；`Reconstructed` 文件只作设计参考。每次复制在此登记一行。

| 日期 | 来源仓库 | 来源路径 | 目标路径 | 是否改动 | 备注 |
|---|---|---|---|---|---|
| 2026-11-21 | 二开版（自有资产） | `backend/agent/chainskel/` | `agent/chainskel/` | 无（逐字节一致） | 15 个文件（4 go + 2 test + 9 md）；sha256 校验一致；仅新增 README |
| 2026-11-21 | 二开版（自有资产） | `backend/honeydetect/` | `honeydetect/` | 无（逐字节一致） | 3 个文件（go + test + signatures.json）；sha256 校验一致 |
| 2026-11-21 | 二开版（自有资产） | `backend/session/`、`stage/`、`tunnel/`、`kb/` | 同名顶层包 | 无 | 含 `ldpreload_x86_64.so`（8.3K）与 `shelltpl/*.tpl` 三个模板；生成器在 `session/ldpreloadgen/` |
| 2026-11-21 | 二开版（自有资产） | `backend/db/{sessions,tunnels,credentials,host_ledger}.go` | `db/` | 仅补 `itoa64` 助手 | 表结构同步进迁移 `0004_inner_net.sql` |
| 2026-11-21 | 二开版（自有资产） | `backend/memory/memory.go` | `memory/` | 补 pgvector 缺失降级（vecOK）、修复 Close 关队列导致 worker 空转 | 表由 ensure() 按 ARTEX_EMBED_DIM 建，迁移 `0006` 只装扩展 |

## 白名单

`session/`、`tunnel/`、`stage/`、`kb/`、`memory/`、`c2/`、`recon/`、`honeydetect/`、`agent/chainskel/`、`tools/manifest.go`、`db/sessions.go`、`db/tunnels.go`、`db/credentials.go`、`db/host_ledger.go`

## 黑名单（Reconstructed，禁止直接复制）

`server/c2_server.go`、`server/proxypool*.go`、`server/webshell.go`、`server/kb_ui.go`、`server/cve_ui.go`、`server/cleanup_retention.go`、`db/c2.go`、`db/proxy_pool.go`、`agent/armor.go`、`server/armor.go`、`server/avevasion_agent.go`、`server/subagents.go`、`db/vps.go`、`server/conversation_rewind.go`、`server/task_chat_rewind.go`、`server/pysitter*.go`、`server/mcp_builtin.go`、`server/mcp_presets.go`