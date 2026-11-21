# ARTEX 产品化路线图

> 基线：当前仓库（upstream 0.3.15 · AGPL-3.0 · Next.js 全源码）
> 能力来源：二开版（只取后端原源包与设计参数；81 个 `Reconstructed` 文件不直接复制）
> 管理方式：git 全程管理，trunk-based，每个 PR 是一个可回滚单元

## 1. 四条铁律

1. **前端只在当前仓库重建**，不使用二开反编译 UI（13 页全 `@ts-nocheck`）。
2. **只复制原源包**；标注 `Reconstructed` 的文件只作设计参考，接线由本仓库重写。
3. **新能力默认关闭或温和模式**，出问题可一键回退，不改变既有任务的行为。
4. **每个 PR 独立可 revert**；数据迁移只新增表/列，不破坏旧数据。

## 2. Git 工作流

| 项 | 约定 |
|---|---|
| 主干 | `main` 始终可构建、可运行、可发布 |
| 分支 | `feat|fix|chore|docs|refactor|test/<里程碑>-<序号>-<简述>`，例如 `feat/m1-01-chainskel` |
| 提交 | Conventional Commits：`feat(scope): …` / `fix(scope): …` / `chore(scope): …` |
| 合并 | 单 PR 单主题；本地 `rebase` 保持线性，里程碑处打 tag |
| Tag | `baseline-0.3.15`（已打）→ `v0.4.0-m0` … `v0.4.0` |
| 远端 | 当前代理不可达，先本地提交；网络恢复后 `git push -u origin <branch>` |

提交正文要求：说清「为什么」与「验收命令」，不只写「改了什么」。

## 3. 里程碑总览

| 里程碑 | 周期 | 目标 | 出口标准 |
|---|---|---|---|
| M0 地基 | 第 1 周 | 迁移机制、协作规范、CI | 空库一键起、迁移幂等、CI 绿 |
| M1 认知层 | 第 2–3 周 | 链路骨架、纪律固定尾、停滞/死面/蜜罐 | 提示词纪律不可被 DB 覆盖，纯函数单测全绿 |
| M2 内网中枢 | 第 4–6 周 | 会话/投递/隧道/凭据/范围审批 | 会话协议测试通过，中枢页可演示 |
| M3 情报面 | 第 7–8 周 | 知识库/记忆/CVE | 检索质量回归通过，嵌入不可达可降级 |
| M4 后渗透面 | 第 9–10 周 | C2/WebShell/代理池 | 各面闭环，可开关关闭 |
| M5 收口 | 第 11 周 | 分级路由、清理策略、发布 | 全量测试绿，打 `v0.4.0` |

## 4. PR 清单

| ID | 分支 | 交付物 | 依赖 | 验收 |
|---|---|---|---|---|
| M0-01 | `chore/m0-01-governance` | 本文件、CONTRIBUTING、PR 模板、CI、导入登记表 | — | `git log` 可见、CI 绿 |
| M0-02 | `feat/m0-02-migrations` | `db/migrations/0001_baseline.sql`、`schema_migrations`、加载器 | M0-01 | 空库启动幂等；重复启动零迁移 |
| M0-03 | `chore/m0-03-schema-check` | schema 与代码期望一致性检查脚本 + CI 步骤 | M0-02 | 故意漏表时 CI 失败 |
| M1-01 | `feat/m1-01-chainskel` | 9 类骨架、meta 铁律、反证 checklist、转向建议 + 单测 | M0-01 | `go test ./agent/chainskel` 绿 |
| M1-02 | `feat/m1-02-prompt-tails` | 四个 agent 的代码固定尾接入 + 单测 | M1-01 | DB 正文改成任意内容，尾仍在 |
| M1-03 | `feat/m1-03-stuck` | 停滞检测、死面熔断、重复调用拦截 + settings | M1-02 | 纯函数单测；阈值可配；默认开 |
| M1-04 | `feat/m1-04-honeydetect` | 蜜罐签名包、资产落库挂点、反 attestation 红线 | M1-02 | `go test ./honeydetect` 绿；失败不影响资产登记 |
| M1-05 | `feat/m1-05-nudges` | 侦察/军火库提醒、UA 池 | M1-03 | 提醒有上限；同任务 UA 稳定 |
| M2-01 | `feat/m2-01-import-session` | 复制 `session`/`stage`/`tunnel` 包与 db 层 + 登记 | M1-05 | 三个包 `go test` 全绿 |
| M2-02 | `feat/m2-02-session-tools` | `register_session`/`session_exec`/`session_read`/`session_write` + guard 接线 | M2-01 | 工具经审批与审计；输出上限 16KB |
| M2-03 | `feat/m2-03-tunnel` | 部署计划、探测、健康巡检、任务级 MITM 上游切换 | M2-02 | 断链后按 `deploy_params` 原样重拉 |
| M2-04 | `feat/m2-04-cred-scope` | 凭据 gcm1、RoE 三态、`pending_scope` 审批、任务移交 | M2-03 | `in/out/unknown` 单测；strict 不误伤 loopback |
| M2-05 | `feat/m2-05-ui-intranet` | 内网中枢页（会话/隧道/凭据/拓扑/投递）+ 一级菜单 | M2-04 | 页面可用，空状态完整 |
| M3-01 | `feat/m3-01-import-kb` | `kb`/`memory` 包、`mem_entries` 迁移、reflector 脱敏沉淀 | M2-05 | 包测试绿；embedder 不可达整体降级 |
| M3-02 | `feat/m3-02-kb-api` | 检索/入库/回填/insights + 入库员 | M3-01 | 同义查询不返回同批条目 |
| M3-03 | `feat/m3-03-cve` | CVE 检索、同步任务、Sploitus 查询 | M3-01 | 203k 数据可检索；同步可中断 |
| M3-04 | `feat/m3-04-ui-intel` | 知识库页、CVE 情报页 | M3-03 | 两页可用，检索分页正常 |
| M4-01 | `feat/m4-01-c2` | C2 包接入、监听器/载荷/会话 API（胶水重写） | M2-05 | 帧协议单测；默认关闭 |
| M4-02 | `feat/m4-02-webshell` | WebShell 接线重写 + 中枢页签 | M4-01 | 会话可登记、可执行、可清理 |
| M4-03 | `feat/m4-03-proxypool` | 代理池最小闭环（录入/探测/切换），后续再扩 | M4-02 | 出口探测可验证；失败可回退 |
| M5-01 | `feat/m5-01-agent-routing` | worker 五档预算、意图预算、角色接力、同任务广播 | M4-03 | 预算按意图自动分档，frontier 有上限 |
| M5-02 | `feat/m5-02-cleanup` | 保留策略、清理与审计页整合 | M5-01 | 到期数据可清，操作有审计 |
| M5-03 | `docs/m5-03-release` | 文档、CHANGELOG、`v0.4.0` tag | M5-02 | 全量测试绿，发布说明完整 |

## 5. 验收门（Definition of Done）

每个 PR 合入前必须全部满足：

1. `go build ./... && go vet ./... && go test ./...` 全绿（无 PG 时数据库用例自动跳过）。
2. 新增纯逻辑必须有表驱动单测；新增 API 至少一条 handler 测试。
3. 不改变既有任务行为：新能力默认关闭或 `warn`。
4. 迁移可重复执行；新表只增不改。
5. 若复制了外部代码，已在 `dev-docs/upstream-imports.md` 登记来源与路径。

## 6. 导入策略

**白名单（可复制）**：`session/`、`tunnel/`、`stage/`、`kb/`、`memory/`、`c2/`、`recon/`、`honeydetect/`、`agent/chainskel/`、`tools/manifest.go`、`db/sessions.go`、`db/tunnels.go`、`db/credentials.go`、`db/host_ledger.go`。

**黑名单（只作设计参考，接线重写）**：81 个 `Reconstructed` 文件，集中在 `server/c2_server.go`、`server/proxypool*.go`（11 个）、`server/webshell.go`、`server/kb_ui.go`、`server/cve_ui.go`、`server/cleanup_retention.go`、`db/c2.go`、`db/proxy_pool.go` 等。

**登记格式**：来源仓库、来源路径、目标路径、复制日期、是否改动。

## 7. 能力开关默认值

| 键 | 默认 | 说明 |
|---|---|---|
| `roe_enforcement` | `warn` | 出界只警告，`strict` 才拦截 |
| `stuck_detect` | `on` | 同质输出触发转向 |
| `dead_surface` | `on` | 目标不可达熔断 |
| `tarpit_guard` | `on` | 每 host 抓取预算 |
| `egress_guard` | `on` | 平台自身敏感指纹拦截 |
| `proxy_guard` | `off` | 内网测试易误伤，默认关 |
| `c2` | `off` | 显式启用后才监听 |
| `proxypool` | `off` | 显式启用后才调度 |

## 8. 风险与回滚

| 风险 | 对策 |
|---|---|
| 还原码行为偏差 | 只参考 API 形状；重写后补测试；登记为黑名单 |
| 迁移写入不可逆 | 只新增表/列；每个迁移附回滚说明 |
| 提示词变更影响既有任务 | 固定尾默认关闭开关；灰度按任务类型 |
| UI 工作量超预期 | 先占位页+空状态，按里程碑逐页填 |
| 数据资产体积大 | 不入 git；部署时挂载或下载 |

## 9. 状态板

| ID | 状态 | commit | 备注 |
|---|---|---|---|
| M0-01 | 进行中 | — | 本文件随该 PR 引入 |
| 其余 | 待开始 | — | 按依赖顺序推进 |

## 10. 变更记录

| 日期 | 变更 |
|---|---|
| 2026-11-21 | 建立路线图、协作规范、CI 与导入登记表 |