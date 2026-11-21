# 后续任务板（P0–P3）

> 单一状态源。每项完成后更新本文件、CHANGELOG 与 dev-docs/PROGRESS.md，并提交推送。
> [x]=已完成，[~]=进行中，[ ]=待开始。

## P0 发布收口与质量门

- [x] P0-1 v0.4.1 补丁发布：CHANGELOG 定版、打 tag、release 五平台构建与 Release 成功。
- [x] P0-2 验收脚本入仓 + CI 门禁：scripts/smoke/api_smoke.py（14 用例）、ui_smoke.mjs（手动）、CI 增加 gofmt 与 api smoke。
- [x] P0-3 UI 小修：系统配置 6 个开关补 aria-label；任务页补 h1；未知路由返回 404（自定义中文 404 页 + Go 静态服务 404 语义）。

## P1 能力补全（对齐 ROADMAP 偏差）

- [x] P1-4 统一定时调度器：retention 自动清理、CVE autosync 实际调度、代理池健康巡检（settings 可控）。
- [x] P1-5 保留/清理前端页：策略编辑、预览、dry-run、审计、目标侧清理清单。
- [x] P1-6 C2 完善：前端页（监听器/会话/任务）、listener 删除 API、HTTP beacon、多语言 implant（DNS 可拆）。
- [x] P1-7 WebShell/凭据闭环：目标侧马体物理删除+校验；凭据注入/复用 UI。
- [x] P1-8 知识库补全：服务端分页、条目编辑/删除、planner extras 注入 kb/cve、insights SQL 聚合。
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

- 2026-11-21：P0 全部完成。提交：88c485a（P0-2/P0-3）、237a26f（v0.4.1 定版）。CI run 44909962033 success；release run 44910352144 success（五平台 zip + SHA256SUMS，非 draft/预发布）。下一步 P1-4 统一定时调度器。

- 2026-11-21：P1-4 完成（server/maintenance.go + cve launch 复用 + /api/scheduler/*）。集成验证：默认任务表、retention 手动执行删到期行并写 actor=scheduler 审计、空池巡检跳过、未知任务 400、状态 last/next 回填。

- 2026-11-21：P1-5 完成（/system/retention：策略/预览/dry-run/真删确认/审计/清理清单/调度状态+手动执行）。Playwright 7 项交互全过、0 console error。

- 2026-11-21：P1-6 完成（迁移 0010 kind；HTTP beacon + token 路径；c2_http.go 会话/任务复用；DELETE listener；POST session task；GET implant 三语言；/function/c2 前端页）。后端集成 13 项全过；Playwright 11 项 E2E 全过（含真实 beacon 领取/回传、端口关闭），0 console error；db 全量测试 fresh DB 全绿。

- 2026-11-21：P1-7 完成（迁移 0011 artifact_path；清理先 rm 后 test ! -e 校验，失败保留会话；凭据 POST/GET secret/DELETE + UI 新增/显示/复制/删除）。Go 测试（物理删除删文件、相对路径拒绝）+ 全量 db/server/c2 全绿；Playwright 6/6（凭据增显删、WebShell 路径登记、物理清理校验）、0 console error。

- 2026-11-21：P1-8 完成（kb/manage.go 分页/更新/删除/SQL Insights；kb API 三个新路由；planner extras 注入 KB/CVE；情报页全部条目分页+编辑+删除）。Go 单测 + kb/db/server/agent 全绿；API 集成 13 项、Playwright 5 项（分页 20/5、编辑、删除、统计）全过。
