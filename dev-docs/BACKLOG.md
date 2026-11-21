# 后续任务板（P0–P3）

> 单一状态源。每项完成后更新本文件、CHANGELOG 与 dev-docs/PROGRESS.md，并提交推送。
> [x]=已完成，[~]=进行中，[ ]=待开始。

## P0 发布收口与质量门

- [ ] P0-1 v0.4.1 补丁发布：CHANGELOG 定版、打 tag、release 五平台构建与 Release 成功。
- [x] P0-2 验收脚本入仓 + CI 门禁：scripts/smoke/api_smoke.py（14 用例）、ui_smoke.mjs（手动）、CI 增加 gofmt 与 api smoke。
- [x] P0-3 UI 小修：系统配置 6 个开关补 aria-label；任务页补 h1；未知路由返回 404（自定义中文 404 页 + Go 静态服务 404 语义）。

## P1 能力补全（对齐 ROADMAP 偏差）

- [ ] P1-4 统一定时调度器：retention 自动清理、CVE autosync 实际调度、代理池健康巡检（settings 可控）。
- [ ] P1-5 保留/清理前端页：策略编辑、预览、dry-run、审计、目标侧清理清单。
- [ ] P1-6 C2 完善：前端页（监听器/会话/任务）、listener 删除 API、HTTP beacon、多语言 implant（DNS 可拆）。
- [ ] P1-7 WebShell/凭据闭环：目标侧马体物理删除+校验；凭据注入/复用 UI。
- [ ] P1-8 知识库补全：服务端分页、条目编辑/删除、planner extras 注入 kb/cve、insights SQL 聚合。
- [ ] P1-9 代理池增强：mihomo 适配器、其余出站统一接入、UI 认证延迟/健康度。
- [ ] P1-10 SSH/SMB 会话登记 + 隧道真机验证（chisel/suo5）。

## P2 验证与安全加固

- [ ] P2-11 LLM 全链路 E2E + 治理强制场景（预算耗尽/frontier 拒派/接力，fake LLM 注入）。
- [ ] P2-12 全量 race / soak / 升级回滚演练 / 备份恢复。
- [ ] P2-13 安全审计：C2/proxy/会话密钥静置加密与轮换、C2 非回环校验、审计完整性、RoE/Scope 边界。
- [ ] P2-14 可观测性：/metrics、结构化日志、清理/同步任务监控。

## P3 体验与文档

- [ ] P3-15 文档：LLM 配置（opencode-go + session_header_key）、C2/代理池/清理运维手册、FAQ。
- [ ] P3-16 前端打磨：C2 Soon 页替换、暗色对比、加载骨架/错误边界、401/403 体验、移动端细节。
- [ ] P3-17 Docker 镜像发布启用（secrets）、demo seed 数据、安装体验。

## 变更记录

- 2026-11-21：建立任务板；P0-2、P0-3 完成（提交后回填 commit）。
