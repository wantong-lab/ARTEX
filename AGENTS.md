# ARTEX 仓库协作说明（Agent 上下文）

## 当前仓库

- 定位：ARTEX 主线发布仓库，当前版本 v0.4.x；Go 单体后端 + Next.js 前端（`web/`）+ PostgreSQL 15 + pgvector。
- 关键目录：`agent/`、`c2/`、`db/`、`server/`、`tunnel/`、`session/`、`web/`、`scripts/`、`dev-docs/`。
- 构建与测试：`go build ./...`、`go vet ./...`、`go test ./...`；前端 `cd web && npm run build`。
- 状态源与记录：`dev-docs/BACKLOG.md`（唯一状态源）、`CHANGELOG.md`、`dev-docs/PROGRESS.md`、`dev-docs/state.json`。
- 验收/冒烟：`scripts/acceptance/`；`scripts/smoke/`（`ui_smoke.mjs` 已接入 CI 的 `ui` job）。

## 设计稿先行约束（强制）

- 任何功能逻辑修改或新增（包括隧道、C2、会话、工具/API、数据结构、数据库迁移、生命周期/健康逻辑）必须先落地设计稿，再进入实现。
- 设计稿目录：dev-docs/design/；命名 YYYY-MM-DD-<slug>.md；必须包含：背景/问题、目标与非目标、总体方案、数据模型、接口/工具、生命周期与健康、安全与 OPSEC、兼容性、测试计划、实施阶段、风险与验收标准。
- 实现提交/PR 必须引用对应设计稿路径；未落设计稿不得实现或合并。
- 纯文档/纯测试补充且不改变功能语义时可不新稿；一旦改变行为、接口、数据结构或安全边界，必须新增或更新设计稿。
- 设计稿随功能演进同步修订，过期设计稿要标注被取代/废弃。

## 关联仓库：ARTEX 二开版本

- 本地路径：`/Users/jiangshoubao/Desktop/学习/1111/artex`
- 形态：`backend/`（Go，go.mod 模块路径仍为 `github.com/Autumn-27/artex`）、`frontend/`（Vite 构建产物 + `serve.mjs`）、`deploy/`（技能/toolkit/安装脚本）、`artifacts/`（历史二进制与前端产物）。
- 定位：二开/定制功能参考仓库。不少文件头部标注 `Reconstructed`，部分模块是重建或裁剪版本，**不等同于当前主线实现**。
- 已确认的差异/参考点：
  - C2 socks5：二开版本是通过**外部 C2 MCP（如 Supershell）**的 `manage_socks5` 提供，见 `deploy/c2-supershell/SKILL.md`；它不是内置 c2 监听器（`backend/c2` 的 `Task.Kind` 注释含 `socks`，但未发现内置实现）。
  - 隧道选型：`deploy/tunnel-selection/SKILL.md`（suo5 / chisel / frp / C2 socks5 的优先级与边界）。
  - 额外工具：`backend/server/erjin_tools.go`（`c2_exec`/`vps_exec`/`proxy_pool_control` 等）以及 `deploy/` 下大量 skill。
- 使用约定：
  - 当前仓库是发布主线；二开仓库仅作参考/功能对照，功能改动应在目标仓库单独进行。
  - 不要跨仓库混用构建产物、`frontend/dist` 或 `deploy/skills`；不要把二开仓库的本地路径写死进生产代码。
  - 读二开仓库时以其实际代码为准，注意目录结构与本仓库不同（`backend/` vs 根目录）。

## 当前进行中的方向

- 跨 OS/跨架构隧道适配：v0.4.4 已完成（Windows 真机 E2E）；v0.4.5 补公网 VPS 验证（固定 bootstrap 端口 + SSH 拉起修复，VPS E2E PASS）。
- C2→隧道：v0.4.6 已支持 c2_session_id（C2 TCP/HTTP 适配器 + 固定 listen_port + 分块/gzip 上传）；公网 VPS 仅 C2 TCP 上线 → chisel SOCKS 真机 E2E PASS（2026-11-22）。
- C2 入站回调中继：v0.4.7 已实现 c2_ingress_deploy/teardown（ingress 隧道 + loopback C2 listener + chisel local forward）；v0.4.8 补 C2 页入站中继卡片 + REST + node_label。Windows 133 目标只回连 VPS 的 HTTP C2 E2E PASS。
- P2-C（v0.4.9）：c2_ingress 节点台账 + 30s 健康巡检 + 手动重建 API/UI；P2-A/B 按用户确认暂不做。
- P2-D（v0.4.9）：/metrics ingress/beacon 指标、c2_events 事件、GET /api/c2/ingress/events 与 UI 时间线；外发告警待接 notifier。
- P2-E（v0.4.9）：入站中继 VPS 会话下拉、健康/节点筛选、runbook、验收脚本。
- C2 控制台 UX（v0.4.9）：首屏方式说明/字段速查、监听器→中继→植入端→会话→任务排序、默认端口 39444/39443。
- C2 socks5 仍未内置；二开为外部 C2 MCP（Supershell manage_socks5）集成，主线如需内置走反向 SOCKS-over-C2 或外部 MCP，待定。