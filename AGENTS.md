# ARTEX 仓库协作说明（Agent 上下文）

## 当前仓库

- 定位：ARTEX 主线发布仓库，当前版本 v0.4.x；Go 单体后端 + Next.js 前端（`web/`）+ PostgreSQL 15 + pgvector。
- 关键目录：`agent/`、`c2/`、`db/`、`server/`、`tunnel/`、`session/`、`web/`、`scripts/`、`dev-docs/`。
- 构建与测试：`go build ./...`、`go vet ./...`、`go test ./...`；前端 `cd web && npm run build`。
- 状态源与记录：`dev-docs/BACKLOG.md`（唯一状态源）、`CHANGELOG.md`、`dev-docs/PROGRESS.md`、`dev-docs/state.json`。
- 验收/冒烟：`scripts/acceptance/`；`scripts/smoke/`（`ui_smoke.mjs` 已接入 CI 的 `ui` job）。

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
- C2 socks5 能力调研：二开版本为外部 C2 MCP（Supershell manage_socks5）集成；主线如需内置，走反向 SOCKS-over-C2 或外部 MCP 两种方案，待定。
