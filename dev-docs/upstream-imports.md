# 上游导入登记

规则：只复制原源包；`Reconstructed` 文件只作设计参考。每次复制在此登记一行。

| 日期 | 来源仓库 | 来源路径 | 目标路径 | 是否改动 | 备注 |
|---|---|---|---|---|---|
| — | — | — | — | — | 尚未开始复制 |

## 白名单

`session/`、`tunnel/`、`stage/`、`kb/`、`memory/`、`c2/`、`recon/`、`honeydetect/`、`agent/chainskel/`、`tools/manifest.go`、`db/sessions.go`、`db/tunnels.go`、`db/credentials.go`、`db/host_ledger.go`

## 黑名单（Reconstructed，禁止直接复制）

`server/c2_server.go`、`server/proxypool*.go`、`server/webshell.go`、`server/kb_ui.go`、`server/cve_ui.go`、`server/cleanup_retention.go`、`db/c2.go`、`db/proxy_pool.go`、`agent/armor.go`、`server/armor.go`、`server/avevasion_agent.go`、`server/subagents.go`、`db/vps.go`、`server/conversation_rewind.go`、`server/task_chat_rewind.go`、`server/pysitter*.go`、`server/mcp_builtin.go`、`server/mcp_presets.go`