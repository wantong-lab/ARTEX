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
- [x] P1-9 代理池增强：mihomo 适配器、其余出站统一接入、UI 认证延迟/健康度。
- [x] P1-10 SSH/SMB 会话登记 + 隧道真机验证（chisel/suo5）。

## P2 验证与安全加固

- [x] P2-11 LLM 全链路 E2E + 治理强制场景（真实 LLM 全链路 + 全局拦截规则 worker 强制；预算/拒派/接力由 agent M5 单测覆盖）。
- [x] P2-12 全量 race / soak / 升级回滚演练 / 备份恢复。
- [x] P2-13 安全审计：C2/proxy/会话密钥静置加密与轮换、C2 非回环校验、审计完整性、RoE/Scope 边界。
- [x] P2-14 可观测性：/metrics、结构化日志、清理/同步任务监控。

## P3 体验与文档

- [x] P3-15 文档：LLM 配置（opencode-go + session_header_key）、C2/代理池/清理运维手册、FAQ。
- [x] P3-16 前端打磨：C2 Soon 页替换、暗色对比、加载骨架/错误边界、401/403 体验、移动端细节。
- [x] P3-17 Docker 镜像发布启用（secrets）、demo seed 数据、安装体验。

## P4 运营能力补强（v0.4.4）

- [x] P4-18 隧道跨 OS/架构适配：平台/目标端 chisel 二进制分离（ARTEX_CHISEL_CLIENT_DIR/<os>-<arch>/chisel[.exe]）、目标 OS/架构自动探测、Windows 目标临时 HTTP 下载 + WMI 拉起 + 巡检/清理适配；单元测试 + Windows 真机 E2E（macOS 平台 → Windows 192.168.112.133，SOCKS5 经目标访问 LAN HTTP 成功）。提交 148bb81（CI 修复 3e95efb），tag v0.4.4。

- [x] P4-19 公网 VPS 隧道验证与 NAT bootstrap：新增 ARTEX_TUNNEL_STAGE_PORT 固定下载端口；Unix/SSH 拉起改 pid 文件后台块；macOS 平台 → Linux VPS（113.20.3.124，公网映射 39.89.211.55:39444）SOCKS5 访问 VPS 本地 HTTP 与公网出口 IP 校验 PASS。提交 10faddf，tag v0.4.5。

- [x] P4-20 公网 C2 真机验证：HTTP beacon（39443）与 TCP（39444）两种监听器经公网映射在 Linux VPS 113.20.3.124 上线；HTTP 任务回传、TCP 加密信封 exec 均 PASS；新增 scripts/acceptance/c2_public_http.py 与 c2/c2_tcp_public_vps_test.go（env 门控）。

- [x] P4-21 C2 会话作为隧道部署入口：tunnel_deploy 支持 c2_session_id；新增 C2 session adapter（TCP Manager Dispatch / HTTP c2_tasks 队列）、固定 listen_port、gzip 分块上传兜底；公网 C2 TCP（无 SSH）→ chisel SOCKS E2E PASS。提交 eadca3a，tag v0.4.6。

- [x] P4-22 C2 入站回调中继：新增 ingress 隧道类型（迁移 0013）、PlanIngress、c2_ingress_deploy/teardown；平台 C2 listener loopback + VPS chisel local forward；Windows 133 HTTP PowerShell 真机 E2E PASS（VPS 443，20.6s）。提交 2a5fe54，tag v0.4.7。

- [x] P4-23 C2 入站中继 P1：C2 页入站中继卡片 + REST（GET/POST /api/c2/ingress、teardown）+ node_label 多 VPS 归属 + healthy 列表；web/后端构建通过。提交 2da92c0，tag v0.4.8。

- [-] P4-24 P2-A HTTPS/443 反代（暂不做：用户可独立放行端口）：vps_bind_host/public_base_url/path_prefix + openresty 站点反代模板 + Windows HTTPS 真机验收
- [-] P4-25 P2-B 访问控制/安全（暂不做：链路已加密）：源 IP ACL、限速、body/Content-Type 限制、日志脱敏、TLS/HSTS
- [~] P4-26 P2-C 多 VPS 健康台账（进行中）：c2_ingress 表、健康巡检、失联告警、手动重建
- [x] P4-27 P2-D 可观测性：ingress/beacon 指标、c2_events 生命周期事件、事件 API/UI；提交 1cc778b；外发告警待接 notifier。
- [x] P4-28 P2-E UI/文档/验收：会话选择器、健康/节点筛选、runbook、验收脚本；提交 b8f7cda。

- [x] P4-29 C2 管理页信息架构/文案优化：方式说明卡、字段速查、卡片排序与默认值。

- [x] P4-30 C2 入站中继反向模式：PlanIngressReverse + Manager.deployReverse + mode/UI/worker 决策；手动 chisel 反向链路验证通过；提交 b1f2372；待完整 C2 E2E。

- [x] P4-31 统一出口代理：egress_mode direct/fixed/pool + EffectiveProxy + 代理池同步/重建 + 设置页模式选择。

- [x] P4-32 出口代理池独立页面：/function/proxy-pool + 侧边栏入口；代理池 UI 从内网中枢迁出；显示 egress_mode/effective。

- [x] P4-33 出口代理池 Clash 订阅导入：订阅 URL UI + 后端 http/https 校验；复用 import-mihomo。

- [x] P4-34 Clash 订阅代理（mihomo 内核）：订阅自动 meta 格式 + 本地 mihomo mixed 口 + 代理池条目/启停/状态；用户订阅真机 E2E 通过。

- [~] P4-35 出口代理控制面 P0：egress_sources/恢复、resolver/fail-closed、supervisor PID/端口、订阅刷新/清洗、状态 UI 已实现；自动刷新/Windows/last-good 故障注入待补。

## 变更记录

- 2026-11-21：建立任务板；P0-2、P0-3 完成（提交后回填 commit）。

- 2026-11-21：P0 全部完成。提交：88c485a（P0-2/P0-3）、237a26f（v0.4.1 定版）。CI run 44909962033 success；release run 44910352144 success（五平台 zip + SHA256SUMS，非 draft/预发布）。下一步 P1-4 统一定时调度器。

- 2026-11-21：P1-4 完成（server/maintenance.go + cve launch 复用 + /api/scheduler/*）。集成验证：默认任务表、retention 手动执行删到期行并写 actor=scheduler 审计、空池巡检跳过、未知任务 400、状态 last/next 回填。

- 2026-11-21：P1-5 完成（/system/retention：策略/预览/dry-run/真删确认/审计/清理清单/调度状态+手动执行）。Playwright 7 项交互全过、0 console error。

- 2026-11-21：P1-6 完成（迁移 0010 kind；HTTP beacon + token 路径；c2_http.go 会话/任务复用；DELETE listener；POST session task；GET implant 三语言；/function/c2 前端页）。后端集成 13 项全过；Playwright 11 项 E2E 全过（含真实 beacon 领取/回传、端口关闭），0 console error；db 全量测试 fresh DB 全绿。
- 2026-11-23：P4-26 P2-C 完成（迁移 0014、健康巡检、重建 API/UI）；P2-A/B 按用户确认暂不做。
- 2026-11-23：P4-27 P2-D 完成指标/事件/UI 时间线；外发告警留后续。
- 2026-11-23：P2-C 提交 f9903e7、P2-D 提交 1cc778b；fresh DB go test ./... 全绿、web build、/metrics 冒烟通过；代码已推 Gitea wantong/ARTEX。
- 2026-11-23：P4-28 P2-E 完成（UI 选择器/筛选、runbook、验收脚本）；web build 通过。
- 2026-11-23：P4-29 C2 页面 UX 优化（说明卡/速查/排序/默认值）；web build 通过。
- 2026-11-23：P4-30 反向 ingress 实现；手动 chisel 反向隧道 VPS 回调 200；待完整 C2 E2E。
- 2026-11-23：P4-31 统一出口代理完成；go build/vet、web build、EffectiveProxy 单测通过。
- 2026-11-23：P4-32 出口代理池独立页面完成；web build 通过。
- 2026-11-24：P4-33 Clash 订阅导入完成；go build/单测/web build 通过。
- 2026-11-24：P4-34 Clash 订阅代理完成；用户 vless/hysteria2 订阅真机启停/出口/清理验证通过。
- 2026-11-24：P4-35 Egress Control Plane 设计稿/ P0 计划完成；代码 P0 待开工。
- 2026-11-24：P4-35 设计稿按“不过度设计”修订为 P0 精简版；待 P0-1 开工。
- 2026-11-24：P4-35 P0 代码实现并完成本机启动/刷新/重启恢复/停止 E2E；go build/vet/单测通过。
