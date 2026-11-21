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
| M0-01 | 已完成 | `8deb132` | 路线图、协作规范、CI、导入登记表 |
| M0-02 | 待开始 | — | 迁移机制 |
| 其余 | 待开始 | — | 按第 10 节明细推进 |

> 状态真相源：[dev-docs/TASKS.md](dev-docs/TASKS.md)；轮次流水：[dev-docs/PROGRESS.md](dev-docs/PROGRESS.md)；机器状态：[dev-docs/state.json](dev-docs/state.json)；目标定义与执行协议：[dev-docs/GOAL.md](dev-docs/GOAL.md)。

## 10. 阶段工作明细

> 每个 PR 固定四段：目标 / 任务拆解 / 涉及文件 / 验收。人日为单人估算，可并行压缩。

### M0 地基（第 1 周）

#### M0-02 迁移机制 · `feat/m0-02-migrations` · 1.5 人日
**目标**：把单文件 schema 改成可增量、幂等、可回溯的迁移体系。
**任务**
1. 将 `db/schema.sql` 原样落为 `db/migrations/0001_baseline.sql`，保留 `CREATE TABLE IF NOT EXISTS` 的幂等性。
2. 新增 `schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`。
3. 新增 `db/migrate.go`：`go:embed migrations/*.sql`，按文件名排序，事务内执行未应用项并登记；失败回滚，沿用现有重试语义。
4. 历史库兼容：库里已有表但没有迁移记录时，先执行 baseline（幂等）再登记 0001。
5. 保留 `ARTEX_LEGACY_SCHEMA=1` 回退旧路径一个版本周期。
**文件**：新增 `db/migrations/0001_baseline.sql`、`db/migrate.go`、`db/migrate_test.go`；修改 `db/db.go`。
**验收**：`ARTEX_PG_DSN=… go test ./db -run TestMigrate` 绿；空库启动建全表；二次启动 `applied=0`。

#### M0-03 schema 覆盖检查 · `chore/m0-03-schema-check` · 0.5 人日
**目标**：杜绝「代码查表、库里没表」这类干净实例事故。
**任务**
1. 新增 `db/schema_required_test.go`，维护 `requiredTables` 清单（新增表时同步）。
2. 测试把迁移应用到临时 schema，校验清单中每张表及其必需列存在。
3. 在 CI 的 test 步骤中生效；缺失时打印缺失项并失败。
**文件**：新增 `db/schema_required_test.go`；可选 `db/testdata/schema_expect.json`。
**验收**：故意注释一张表 → CI 失败并指出表名。

### M1 认知层（第 2–3 周）

#### M1-01 链路骨架包 · `feat/m1-01-chainskel` · 2 人日
**目标**：九类攻击面的「场景→判据→转向」骨架、meta 铁律、反证清单成为可注入的代码资产。
**任务**
1. 复制 `agent/chainskel/`（4 个 go + 9 个 md + 测试），适配模块路径与包注释。
2. 校验 9 个分类齐全，`Valid/Normalize/Validate` 行为与原实现一致。
3. 增加 `SkeletonForPayload(payload)`：解析 `chain_tags`，无 tag 时走关键词回退。
4. 单测覆盖标签校验、8000 rune 截断、骨架缺失静默降级。
**文件**：`agent/chainskel/*`。
**验收**：`go test ./agent/chainskel` 绿；9 类骨架均可读。

#### M1-02 纪律固定尾接入 · `feat/m1-02-prompt-tails` · 2 人日
**目标**：纪律由代码拥有，DB 正文改不掉。
**任务**
1. `workerSystem` 追加：worker meta 铁律、证据先行、里程碑收尾、反蜜罐红线。
2. `plannerSystem` 追加：planner meta 铁律、蜜罐处置纪律、收尾痕迹清理纪律。
3. `renderIntentTask` 按意图 `chain_tags` 注入 L3 骨架与 L4 原文指引（意图正文在用户消息，不破坏 system 缓存）。
4. `add_intent` 的 payload schema 增加可选 `chain_tags`，用 `ValidateChainTags` 校验。
**文件**：修改 `agent/worker.go`、`agent/planner.go`、`agent/mainagent.go`、`agent/goals.go`、`agent/tools.go`；新增对应 `_test.go`。
**验收**：单测把 DB 正文改任意内容后固定尾仍在；非法 `chain_tags` 返回明确报错。

#### M1-03 停滞检测与死面熔断 · `feat/m1-03-stuck` · 3 人日
**目标**：把「同质输出硬磨」「对已死 host 继续打」变成确定性干预。
**任务**
1. `agent/stuck.go`：输出归一化（时间戳/路径/hex/随机串/数字）+ 签名哈希 + 阈值/冷却/次数。
2. `agent/deadsurface.go`：按 host 统计传输层失败、5xx、本地代理口失败三类信号。
3. `agent/repeating.go`：连续 3 次同参数调用时拦截并回显上次结果。
4. 在 worker 的 hook 装配处合并 steer hooks（与 guard 同注册点）；触发时注入纠正消息并写 activity。
5. settings：`stuck_detect`、`stuck_same_trigger`、`dead_surface`，默认开。
**文件**：新增 3 个实现 + 3 个测试；修改 `agent/worker.go`、`server/engine.go`、settings 相关。
**验收**：纯函数单测覆盖「12 次同签名触发」「5 次超时熔断」「冷却期内不重复」；真实任务可观察到注入。

#### M1-04 蜜罐识别与反诱导 · `feat/m1-04-honeydetect` · 2 人日
**目标**：静态签名识别 + 处置纪律 + 反 attestation 红线。
**任务**
1. 复制 `honeydetect/`（含 `signatures.json` 与测试）。
2. 资产落库挂点：服务资产插入/指纹回写后评分，命中回写 `honeypot_score`/`honeypot_evidence`，best-effort。
3. 命中时向探索图写 fact 锚定，planner 态势可见。
4. 处置纪律与反蜜罐红线补单测。
**文件**：`honeydetect/*`；`db/migrations/0002_honeypot_columns.sql`；`agent/tools_insert.go` 挂点；`agent/honeypot.go`。
**验收**：`go test ./honeydetect`；固定 ServiceView 断言评分与证据；评分失败不影响资产登记。

#### M1-05 工具纪律提醒与 UA 池 · `feat/m1-05-nudges` · 1.5 人日
**目标**：减少手写 curl 替代批量工具、手搓明文马；同一任务 UA 稳定。
**任务**
1. `agent/reconnudge.go`：同 run 内 curl 计数达阈值且未用批量工具时注入一行 stderr 提醒（上限 2 次，用过批量工具后永久沉默）。
2. `agent/arsenalnudge.go`：检测写 php/jsp/aspx 且含马体特征时提醒标准模板。
3. `agent/uapool.go` + `proxyEnv` 注入 `ARTEX_UA`（按 taskID 稳定分配），settings `web_ua_pool` 可配。
4. 阈值沿用二开实测值（10 次/间隔 12/上限 2）。
**文件**：新增 2 个 nudge + `agent/uapool.go` + 测试；修改 `agent/worker.go`。
**验收**：单测覆盖阈值与静默条件；`proxyenv_test` 断言 UA 注入且同任务不变。

### M2 内网中枢（第 4–6 周）

#### M2-01 导入会话/投递/隧道基础包 · `feat/m2-01-import-session` · 3 人日
**目标**：原源包先能编译、协议测试绿。
**任务**
1. 复制 `session/`、`stage/`、`tunnel/`、`kb/`（session 依赖 kb）、`db/sessions.go`、`db/tunnels.go`、`db/credentials.go`、`db/host_ledger.go`。
2. 迁移 0003：`sessions`、`tunnels`、`credentials`、`host_ledger`。
3. 在 `dev-docs/upstream-imports.md` 登记来源、路径、日期、改动。
4. 剥离对旧 Server/db 接口的引用，只保留纯逻辑。
**文件**：上述包 + 3 张表迁移 + 登记表。
**验收**：`go build ./... && go vet ./...`；`go test ./session ./stage ./tunnel ./kb` 全绿。

#### M2-02 会话工具与审批接线 · `feat/m2-02-session-tools` · 4 人日
**目标**：agent 可登记/执行/读写立足点，全程走审批与审计。
**任务**
1. 实现 4 个 CoreTool：`register_session`、`session_exec`、`session_read`、`session_write`，schema 与二开一致。
2. 注册进 `BuiltinToolSeeds`，绑定 worker/pentest；`session_exec` 输出上限 16KB。
3. 接 guard/intercept 审批，写 activity 与 host_ledger。
4. 会话启动探活：`last_beat` 超时的 alive 会话重新探测，判死后置状态。
**文件**：新增 `server/session_tools.go` + 测试；修改 `agent/toolcatalog.go`、`server/intercept.go`。
**验收**：用 httptest 假一句话马跑通 exec/read/write；审批拒绝路径有测试。

#### M2-03 隧道与任务级 MITM · `feat/m2-03-tunnel` · 4 人日
**目标**：隧道计划可持久化重拉；每任务 MITM 上游可热切换。
**任务**
1. 隧道工具：`tunnel_deploy`、`tunnel_list`、`tunnel_teardown`；计划整体存 `deploy_params`。
2. 健康巡检：定期探活、断链重建、状态回写。
3. `traffic` 改多实例：`EnsureTaskTraffic(taskID)`、每实例独立 CA 目录、`SetUpstreamProxy` 热切换。
4. 上游选择顺序：任务 alive socks 隧道 → 全局代理 → 直连。
**文件**：新增 `server/tunnel_tools.go`、`traffic/manager.go`；修改 `traffic/traffic.go`、`server/assembly.go`。
**验收**：计划序列化往返；上游切换后录制仍归属任务库；隧道死亡自动回退。

#### M2-04 凭据、RoE 与范围审批 · `feat/m2-04-cred-scope` · 4 人日
**目标**：凭据加密托管；出界行为可管；新网段必须审批。
**任务**
1. 凭据存储复用现有密钥派生，`gcm1:` 封装；单行解密失败降级不拖垮列表。
2. `guard` 扩展：`ScopeMatcher` 三态、命令/URL 目标提取、模式 off/warn/strict。
3. `pending_scope` 表 + API：新 IP 按 /24 聚合、审批后写 `task_scope`、即时生效。
4. 任务移交：以会话为起点建子任务，初始 scope /32，父子关联。
**文件**：新增 `guard/scope.go`、`guard/roe.go`、`guard/extract.go`、`server/pending_scope.go`、`server/handoff.go`；迁移 0004。
**验收**：三态单测；strict 不拦 loopback；审批后下一次工具调用生效。

#### M2-05 内网中枢页 · `feat/m2-05-ui-intranet` · 5 人日
**目标**：单页承载会话/隧道/凭据/拓扑/投递五个视图。
**任务**
1. 路由 `(main)/function/intranet/page.tsx`，五个页签。
2. `web/src/lib/api` 增加 sessions/tunnels/credentials/intranet/stage 客户端。
3. 侧栏新增「立足点与后渗透」一级分组，容纳内网与 C2。
4. 空状态、加载态、错误态齐全；危险操作二次确认。
**文件**：新增页面与组件；修改 `navigation/sidebar/sidebar-items.ts`。
**验收**：`npm run build` 通过；五页签在 mock 数据下渲染；危险操作有确认。

### M3 情报面（第 7–8 周）

#### M3-01 知识库与语义记忆包 · `feat/m3-01-import-kb-memory` · 3 人日
**目标**：知识检索与历史相似案例注入可用且可降级。
**任务**
1. `kb` 已随 M2-01 引入；本 PR 补 `memory/` 与 reflector 接线。
2. 迁移 0005：`mem_entries` + `vector` 扩展（无扩展时降级关键词）。
3. 嵌入配置：`ARTEX_EMBED_URL`、`ARTEX_EMBED_MODEL`，不可达静默降级。
4. 沉淀：意图 done 提炼打法（IP/URL 脱敏），blocked 提炼负面清单。
**文件**：`memory/*`、`server/reflector.go`、迁移 0005。
**验收**：`go test ./memory`（需补测试）；嵌入不可达时主流程不受影响。

#### M3-02 知识库 API 与入库员 · `feat/m3-02-kb-api` · 3 人日
**目标**：检索/入库/回填/洞察闭环。
**任务**
1. API：`/api/kb/status|search|entry|backfill`、`/api/v2/kb-insights`。
2. 检索：FTS5 + 严格过滤，宁可空结果也不污染调用方。
3. 入库员：漏洞确认后自动提炼入库，失败只记日志。
**文件**：新增 `server/kb_api.go`、`server/kb_curator.go` + 测试。
**验收**：同义查询不返回同批条目；入库幂等。

#### M3-03 CVE 与 Sploitus · `feat/m3-03-cve` · 3 人日
**目标**：203k CVE 可检索，Sploitus 可查。
**任务**
1. 引入 `kb/cve.go`、`sploitus.go`；重写同步任务（可中断、可续跑）。
2. API：status/search/sploitus/sync/autosync。
3. 只读挂载回退 `immutable=1`，避免 WAL 打不开。
**文件**：`kb/cve*.go`、`server/cve_api.go` + 测试。
**验收**：fixture 库检索命中；同步中断可续；只读挂载可检索。

#### M3-04 情报页 · `feat/m3-04-ui-intel` · 4 人日
**目标**：知识库页 + CVE 情报页。
**任务**：检索、详情、入库、同步进度四个区块；分页与空状态；同步可中断。
**验收**：`npm run build`；检索分页正常；同步进度可中断。

### M4 后渗透面（第 9–10 周）

#### M4-01 C2 · `feat/m4-01-c2` · 5 人日
**目标**：监听器/载荷/会话闭环，默认关闭。
**任务**
1. 接入 `c2` 包（协议/信封/管理器）；迁移 0006 `c2_*`。
2. 重写 `server/c2_server.go`：监听器生命周期、会话、任务下发、结果回传。
3. 工具：`c2_list_sessions`、`c2_exec`、`c2_task_result`。
4. 默认关闭，需显式启用并配置回调地址。
**验收**：帧编解码单测；监听器启停；关闭时不监听端口。

#### M4-02 WebShell 接线 · `feat/m4-02-webshell` · 3 人日
**目标**：WebShell 生成与管理接线重写，复用 `session/http_shell`。
**任务**：生成/登记/管理 API；中枢页增加 WebShell 页签；清理走 host_ledger。
**验收**：登记→执行→读取→清理全链路测试。

#### M4-03 代理池最小闭环 · `feat/m4-03-proxypool` · 4 人日
**目标**：能录入、探测、切换、回退；mihomo 适配器后置。
**任务**：`proxy_pool` 表与 API；出口探测（exit IP）；切换后新请求走新出口；失败回退全局代理。
**验收**：探测可用性判断；切换生效；失败自动回退。

### M5 收口（第 11 周）

#### M5-01 Agent 分级路由 · `feat/m5-01-agent-routing` · 3 人日
**目标**：预算按意图自动分档，frontier 可控，角色可接力。
**任务**：worker 五档（900/1800/2400/2400/1800）；意图预算分类；frontier 上限 8；角色接力表；同任务广播总线。
**验收**：分类单测；frontier 满时拒派；接力幂等。

#### M5-02 清理与保留 · `feat/m5-02-cleanup` · 1.5 人日
**目标**：数据有保留策略，收尾有痕迹清理清单。
**任务**：保留策略（activity 30d / logs 14d / c2 30d，可配）；清理 API 与审计；按 host_ledger 生成清理清单。
**验收**：只删到期数据；dry-run 不删。

#### M5-03 发布 · `docs/m5-03-release` · 2 人日
**目标**：可发布版本。
**任务**：更新 CHANGELOG、README、部署文档；打 `v0.4.0` tag；release workflow 产出五平台二进制。
**验收**：CI 全绿；tag 触发发布成功。

## 11. 变更记录

| 日期 | 变更 |
|---|---|
| 2026-11-21 | 建立路线图、协作规范、CI 与导入登记表（`8deb132`） |
| 2026-11-21 | 补充每个阶段与 PR 的工作明细（目标/任务/文件/验收） |
