# 轮次流水（Goal Mode）

> 只追加不修改。每条格式：轮次 / 时间 / 动作 / 结果 / 下一步。

## 轮次 0 · 2026-11-21
- 动作：确认基线（upstream 0.3.15 / AGPL / Next.js 全源码）、完成技术可行性探针（10 个包直接复制可编译、测试绿）。
- 结果：建立治理基线 `8deb132`（ROADMAP/CONTRIBUTING/PR 模板/CI/导入登记表），补充阶段明细 `77740cf`，分支已推送。
- 下一步：建立 goal 记录文件并创建 goal，随后进入 M0-02。

## 轮次 1 · 2026-11-21
- 动作：M0-02 迁移机制。`db/schema.sql` 迁为 `db/migrations/0001_baseline.sql`；新增 `db/migrate.go`（加载/排序/事务执行/版本登记）与 `db/migrate_test.go`；`db.Open` 改走迁移路径，`ARTEX_LEGACY_SCHEMA=1` 保留旧路径。
- 结果：build/vet 通过；一次性 PG15+pgvector 实例上首次应用 1 个迁移、二次 0 个；db 包全量测试绿。提交 `7e192c8`。
- 下一步：M0-03 schema 覆盖检查。

### 轮次 1 补充 · 2026-11-21
- 动作：全量回归四种组合（并行/串行 × 空库/预置 schema），并与基线 `0aa09d0` A/B 对照。
- 结果：基线既有失败 2 个（T-02 task scope 隔离、T-03 TempDir 清理）；空库另有 T-01（`llm_usage` 由代码运行时建表）。预置 schema 后失败从 6 降至 2，且基线同样失败 → 与 M0-02 无关。已登记 M0-04 与 T-01~T-03。
- 下一步：M0-03 schema 覆盖检查。

## 轮次 2 · 2026-11-21
- 动作：M0-03 schema 覆盖检查。新增 `db/schema_coverage_test.go`：用 `go/parser` 只扫描字符串字面量里的 SQL（剔除注释、EXTRACT 等函数噪声、CTE 与非表 token），与迁移 DDL 求差；另加核心表清单校验与真实 PG 上的表/列校验。
- 结果：扫描 78 个表引用 / 迁移 49 张表，差集 0；fixture 漏表用例可检出；PG 列校验通过；db 包全量绿。提交 `0a8374b`。
- 下一步：M0-04 测试基线（0002 收敛运行时建表 + 修 T-02/T-03 + CI 预置 schema）。

## 轮次 3 · 2026-11-21
- 动作：M0-04 测试基线。新增 `db/migrations/0002_llm_observability.sql` 收敛 `llm_records`/`llm_usage`；T-02 改为校验当前契约（company scope 行 + 覆盖度分母）；T-03 新增 `testTempDir` 并在 server 测试中统一替换；CI 改为按序应用全部迁移。
- 结果：空库 + 应用全部迁移后，`go test -p 1 ./...` 连续两次 15 包全绿（0 失败）；`go build`/`go vet` 通过。提交 `5910df5`。
- 下一步：M1-01 chainskel 链路骨架包。

## 轮次 4 · 2026-11-21
- 动作：M1-01 chainskel。从二开版逐字节复制 9 类骨架（web/ad/app/priv/pwn/recon/rev/pivot/meta）、meta 铁律、反证 checklist、转向建议；补包内 README 说明路由/降级/接线状态；导入登记表记录来源与 sha256。
- 结果：15 个文件 sha256 与来源一致；`go build/vet` 通过；`go test ./agent/chainskel` 17 个用例全绿（骨架齐全、标签校验、8000 rune 截断、缺失静默降级）。提交 `7aed114`，分支 `feat/m1-01-chainskel`。
- 下一步：M1-02 固定尾接入（worker/planner/mainagent/goals + 意图骨架注入）。

## 轮次 5 · 2026-11-21
- 动作：M1-02 固定尾接入。新增 `agent/discipline.go`：worker 追加 meta 铁律/证据先行/里程碑收尾/反蜜罐红线；planner 追加 meta 铁律/痕迹清理纪律。`renderIntentTask` 按意图 `chain_tags` 注入 L3 骨架与 L4 指引。`add_intent` 增加 `chain_tags` 字段、枚举 schema 与规整校验。
- 结果：5 个新用例全绿（固定尾不可被 DB 覆盖、骨架注入、无匹配静默、标签校验）；agent 包测试通过；全量 16 包全绿（EXIT=0）。提交 `c8d1747`。
- 偏差：planner 的蜜罐处置纪律依赖 `honeypot_assets` 态势字段，延后到 M1-04 与评分一起落地。
- 下一步：M1-03 停滞检测与死面熔断。

## 轮次 6 · 2026-11-21
- 动作：M1-03。新增 `agent/stuck.go`（输出归一化+签名，同签名 12 次或同工具失败 15 次且相似度 ≥0.8）、`agent/deadsurface.go`（按 host 传输失败 5/5xx 8/出口代理 3）、`agent/repeating.go`（同参数连续 3 次拦截并回放上次结果）、`agent/detectors.go`（组合 + settings 开关）。经 `steerHooks` 挂在 PreToolUse/PostToolUse：命中拦下一次调用、把转向指令交回模型，并写 system 活动。
- 结果：10 个单测全绿；`go build/vet` 通过；全量 16 包全绿（EXIT=0）。提交 `e5db440`。
- 下一步：M1-04 蜜罐识别 + planner 蜜罐处置纪律（补齐 M1-02 延期项）。

## 轮次 7 · 2026-11-21
- 动作：M1-04。逐字节复制 honeydetect 包（sha256 一致）；迁移 0003 给 assets 加 `honeypot_score`/`honeypot_evidence` 与部分索引；服务资产落库后 best-effort 评分、回写并写 fact 锚定；planner 固定尾补「蜜罐处置纪律」，M1-02 延期项关闭。
- 结果：honeydetect 6 个用例、db 评分读写（取最高分、低分不覆盖）与 planner 固定尾用例全绿；`go build` 通过；全量 17 包全绿（EXIT=0）。提交 `e972c91`。
- 下一步：M1-05 侦察/军火库软提醒 + UA 池，随后进入 M2 内网中枢。

## 轮次 8 · 2026-11-21
- 动作：M1-05。`nudges.go`：侦察提醒（curl/wget 累计阈值、用过批量工具后永久沉默、上限 2）与手写 webshell 提醒，经 steer 钩子把提示前置到 Bash 的 stderr（不改语义）。`uapool.go`：按 taskID 稳定分配 UA 并经 ARTEX_UA 注入 BashEnv，settings `web_ua_pool` 可配；worker 固定尾补客户端指纹纪律。
- 结果：8 个单测全绿；`go build/vet` 通过；全量 17 包全绿（EXIT=0）。提交 `7fdad77`。**M1 认知层里程碑全部完成。**
- 下一步：M2-01 导入 session/stage/tunnel/kb 基础包与 0004 迁移，进入内网中枢。

## 轮次 9 · 2026-11-21
- 动作：M2-01。逐字节复制 `session`/`stage`/`tunnel`/`kb` 四个包（含 `ldpreload_x86_64.so` 与 `shelltpl/*.tpl` 三个模板、生成器）与 `db/{sessions,tunnels,credentials,host_ledger}.go`；新增 `0004_inner_net.sql` 建四张表与索引，迁移测试期望改为 4；补 `itoa64` 助手；登记导入来源。
- 结果：`go build/vet` 通过；session/stage/tunnel/kb 包测试全绿（session 3.7s）；真实 PG 上 4 个迁移应用成功；全量 21 包全绿（EXIT=0）。提交 `8d2ca82`，分支 `feat/m2-01-import-session`。
- 下一步：M2-02 会话工具与审批接线。

## 轮次 10 · 2026-11-21
- 动作：M2-02。新增 `server/session_tools.go`：会话中枢（注册表 + 落库 + 台账 + RoE 门）与四个工具 `register_session`/`session_exec`/`session_read`/`session_write`。秘密经 `SessionStore` gcm1 加密；执行/读/写逐次写 `host_ledger`（写文件记 artifact+cleanup）；`session_exec` 16KB 截断；`last_beat` 超时会话加载时探活并置 dead；元数据进 `BuiltinToolSeeds` 绑定 worker/pentest。
- 结果：httptest 假 phpcmd 马跑通 register→exec→write→read；截断、RoE 拒绝、陈旧判死、schema 不漂移共 5 个用例全绿；全量 21 包全绿（EXIT=0）。提交 `2e74c05`。
- 偏差：ssh/smb 会话登记与后台巡检扫器未纳入本 PR（延后 M2-04/M2-05）；活动流依赖 harness 既有 tool-call 记账，host_ledger 为显式台账。
- 下一步：M2-03 隧道工具与任务级 MITM 热切换。

## 轮次 11 · 2026-11-21
- 动作：M2-03。新增 `server/tunnel_tools.go`（tunnel_deploy/list/teardown + 健康巡检随 server 生命周期启动 + 状态变化回调）与 `server/traffic_manager.go`（任务级 MITM：懒建独立实例/CA 目录/端口池，上游顺序 alive socks→全局→直连，热切换并刷新已缓存 worker 代理）；工具元数据进 BuiltinToolSeeds 绑定 worker/pentest。
- 结果：上游选择表驱动、热切换含死亡回退、Plan 序列化往返、工具 schema 不漂移共 4 个用例全绿；全量 21 包全绿（EXIT=0）。提交 `a332e93`。
- 偏差：`traffic/traffic.go` 未改（当前上游 0.3.15 已内置 SetUpstreamProxy）；chisel 二进制缺失，真机部署路径未端到端验证（工具返回明确错误）；回调地址取 env `ARTEX_CALLBACK_ADDR`，后续接入 config/设置页。
- 下一步：M2-04 凭据与 RoE 范围审批。

## 轮次 12 · 2026-11-21
- 动作：M2-04。`guard/extract.go` 提取 URL/host:port/IP/CIDR（跳过文件名）；`guard/scope.go` 三态匹配（loopback 永不拦，空 scope=unknown 不拦）；`guard/roe.go` 三模式 off/warn/strict 并接入 Guard PreToolUse；任务 Guard 从 settings 读 `roe_scope_mode`（默认 warn）、scope 每轮从 task_scope 重建。新增 0005 迁移 `pending_scope`（IP 按 /24、IPv6 /64 聚合；source=approval 约束），批准写 task_scope 即时生效、拒绝可审计。
- 结果：guard 7 个单测全绿；PG 上审批写范围、重复审批拒绝、拒绝路径用例通过；全量 21 包全绿（EXIT=0）。提交 `15482b7`。
- 偏差：pending_scope 的 HTTP API 与任务移交（以会话为起点建子任务、初始 /32）延后到 M2-05；凭据单行解密降级实现随 M2-01 导入已存在，未新增专测（记入 M2-05 待补）。
- 下一步：M2-05 内网中枢页与审批 UI。

## 轮次 13 · 2026-11-21
- 动作：M2-05。新增 `server/intranet_api.go`：会话/隧道/凭据/主机台账/范围审批列表只读 API，范围批准/拒绝、立足点移交（建子任务并继承单主机 scope、SourceTaskIDs 血缘）。前端新增 `/function/intranet` 五页签（会话/隧道/凭据/主机台账/范围审批），含空/加载/错误态与危险操作二次确认；侧栏新增「立足点与后渗透」分组；`lib/api` 增客户端方法、`lib/types` 增类型。补凭据单行解密降级 PG 用例。
- 结果：`npm run build` 通过且生成 `/function/intranet` 路由；Go 全量 21 包全绿（EXIT=0）；凭据错密钥场景列表可用、单行 `SecretOK=false`。提交 `c416901`。**M2 内网中枢里程碑全部完成。**
- 偏差：页签为会话/隧道/凭据/台账/审批，规格中的「拓扑」「投递」视图延后（需拓扑图数据与 stage API）；未做 MOCK 渲染验证（页面走真实 API）。
- 下一步：M3-01 知识库与语义记忆包导入。

## 轮次 14 · 2026-11-21
- 动作：M3-01。复制 `memory` 包并适配：无 pgvector 扩展时降级关键词召回（TEXT 列 + ILIKE），修复 fork 的 `Close` 关队列导致 worker 空转缺陷（closed 通道 + 幂等 Close）；新增 `0006_memory.sql`（尝试安装扩展，表由 ensure 按 ARTEX_EMBED_DIM 建）。`server/reflector.go`：意图 done→打法、blocked/exhausted→负面清单，内容脱敏后异步入记忆；引擎终态调用。worker system 注入历史相似经验块（Manager 懒初始化 memory，召回为空/不可用不注入）。
- 结果：memory 4 用例 + reflector 3 用例全绿；干净测试库全量 **22 包全绿**（EXIT=0）。提交 `7717328`。
- 过程问题：mock 阶段曾因共用 artex_test 残留 company_scope 行导致 `TestCompanyScopeAttribution` 误报；重建干净库后全绿（环境残留，非代码回归）。
- 偏差：迁移编号为 0006（规格写 0005，编号已被 pending_scope 占用）；`mem_entries` 表结构由运行时 ensure 建（维度依赖 ARTEX_EMBED_DIM），迁移只装扩展。
- 下一步：M3-02 知识库 API 与入库员。

## 轮次 15 · 2026-11-21
- 动作：M3-02。新增 `server/kb_api.go`（status/search/entry/backfill + v2/kb-insights）、`server/kb_tools.go`（kb_search/kb_add，元数据进 BuiltinToolSeeds 并注入 worker/pentest）、`server/kb_curator.go`（FindingRecorder 装饰器：漏洞记录成功后幂等入库、内容脱敏、失败只记日志）；`kb.Reindex` 兼容 poc/entries 两种库形态；知识库在 `data/kb` 懒打开，失败静默降级。
- 结果：空查询与无关查询严格返回空、命中可检索、条目/洞察/backfill 用例、入库幂等（同 finding 两次仍 1 条）与脱敏用例共 6 个全绿；干净库全量 **22 包全绿**（EXIT=0）。提交 `e1114d5`。
- 偏差：planner 无 extras 注入通道，kb_search/kb_add 目前只给 worker/pentest；kb-insights 用前 500 条本地聚合（数据量大后需 SQL 聚合）。
- 下一步：M3-03 CVE 与 Sploitus。

## 轮次 16 · 2026-11-21
- 动作：M3-03。重写 `kb/cve_sync.go`（替换二开 reconstructed 胶水）：NVD API 2.0 分页同步、每页落库后写 `cve.sync.json` 断点、ctx 取消返回可续跑状态、兼容 cve/cves 两种库；`server/cve_api.go` 新增 status/search/sploitus/sync/autosync；`cve_search` 工具进 BuiltinToolSeeds 并注入 worker/pentest（描述里明确「指纹匹配≠可利用」）。
- 结果：中断→续跑→幂等、只读挂载检索、fixture 命中、sploitus/sync/autosync 共 5 个用例全绿；干净库全量 **22 包全绿**（EXIT=0）。提交 `7b6d0d3`。
- 偏差：autosync 仅持久化开关，实际调度留给 M5 定时任务；Sploitus 线上网络路径用 stub 验证（离线环境不触网）；cve_search 只给 worker/pentest（planner 无 extras 通道）。
- 下一步：M3-04 情报页。

## 轮次 17 · 2026-11-21
- 动作：M3-04。新增 `/function/intel`：知识库（检索+详情+入库+分页）、CVE 情报（检索+同步进度条+开始/中断）、Sploitus 在线检索三块，空/加载/错误态齐全，CVE 结果标注「线索，非确证」；侧栏新增「情报」。后端补 `POST /api/kb/add`（手工入库，幂等）与 `POST /api/cve/sync/stop`（取消 ctx、断点保留可续跑）。
- 结果：`npm run build` 通过并生成 `/function/intel`；手工入库可检索、同步可中断 2 个新用例通过；干净库全量 **22 包全绿**（EXIT=0）。提交 `a536548`。**M3 情报面里程碑全部完成。**
- 偏差：知识库检索分页为客户端分页（后端 search 尚无 offset）；Sploitus 需外网，离线时展示错误态。
- 下一步：M4-01 C2。

## 轮次 18 · 2026-11-21
- 动作：M4-01。二开版 `c2` 整包带 Reconstructed 标记，按导入政策**不拷贝**，改为按协议设计自研：`c2/`（AES-256-GCM 信封、帧编解码、token 握手、会话/任务管理器、Python 载荷模板）；`db/c2.go` 台账（监听器/会话/任务/事件，token 加密落库、无主密钥 fail-closed）；迁移 `0007_c2.sql`；`server/c2_server.go` 运行时（默认关闭、显式启用后按台账起监听器、会话上下线落库）+ API（status/enable/listeners/sessions/tasks/启停）+ 3 个 agent 工具（未启用明确拒绝）。
- 结果：9 个用例全绿（帧/信封/握手/派发/停止/坏 token/加密台账/状态机/默认关闭）；干净库全量 **23 包全绿**（EXIT=0）。提交 `c415314`。
- 偏差：仅实现 TCP 反向监听（HTTP beacon 回调留待后续）；C2 前端页面未做（登记 M5）；非回环绑定的范围校验依赖 RoE/Guard 在工具层已有的约束。
- 过程问题：迁移计数断言需同步 7；共享测试库残留再次导致 `TestCompanyScopeAttribution` 误报，重建库后全绿。
- 下一步：M4-02 WebShell 接线。

## 轮次 19 · 2026-11-21
- 动作：M4-02。新增 `server/session_ops.go`（登记/执行/读取/列表/清理内核，工具与 API 共用）、`server/webshell_api.go`（6 个路由）、`server/webshell_tools.go`（generate_webshell）、`agent/webshelltoolmeta.go`；内网中枢页新增 WebShell 页签（生成、登记、列表、执行/读取、清理）。清理动作先写 host_ledger（channel=webshell、artifact=true、cleanup「删除已上传 WebShell」）再删会话并同步移除内存注册表。
- 结果：登记→执行→读取→列表→清理全链路用例通过（清理后再执行必失败、secret 不外泄、gate 拒绝登记）；`npm run build` 通过；全新库全量 **23 包全绿**（EXIT=0）。提交 `85836c1`。
- 偏差：清理只落台账+删会话，目标侧马体文件的实际删除需后续通过会话执行（避免管理页直接对目标发命令）。
- 下一步：M4-03 代理池最小闭环。

## 轮次 20 · 2026-11-21
- 动作：M4-03。新增 `proxypool` 包（URL 校验、出口 IP 探测、活动代理+全局兜底管理、「活动→兜底→直连」三级回退，失败累计 fail_count）；`db/proxy_pool.go` + 迁移 `0008`（URL 加密落库、同 scheme+host+port 去重）；`server/proxy_pool_api.go` 8 个路由（列表/批量录入/探测/切换/回退/全局配置/出口自检/删除），活动代理与全局配置持久化 settings；Sploitus 出站注入代理池 client（`kb.SetSploitusHTTPClient`）；内网中枢页新增代理池页签。
- 结果：探测可用性、切换改变出口 IP、坏死自动回退全局/直连、URL 密文+去重共 4 组用例全绿；`npm run build` 通过；全新库全量 **24 包全绿**（EXIT=0）。提交 `4d47718`。**M4 后渗透面里程碑全部完成。**
- 偏差：mihomo 适配器按规格后置；目前仅 Sploitus 出站接入代理池 client，其余出站路径（CVE 同步等）后续统一接入。
- 下一步：M5-01 Agent 分级路由。

## 轮次 21 · 2026-11-21
- 动作：M5-01。新增 `agent/intentbudget.go`（五档分类 recon 900/enum 1800/brute 2400/exploit 2400/general 1800 + 纪律提示）、`agent/frontier.go`（默认上限 8 的并发限制）、`agent/relay.go`（角色接力表 + 幂等账本）、`agent/taskbus.go`（同任务广播总线）；worker system 注入预算纪律；engine 在 `runWorkerStep` 满额拒派、`runIntent` 用预算 ctx 作为硬上限（耗尽按 exhausted 收尾）、终态广播并做幂等接力事件。
- 结果：分类/边界、frontier 满拒派、接力幂等、广播收发 4 组用例全绿；全新库全量 **24 包全绿**（EXIT=0）。提交 `84b8e78`。
- 偏差：接力只发事件（planner 决策派发，不自动建意图）；frontier 上限固定 8（settings 动态化留待后续）；预算五档通过 ctx 超时+提示双约束实现，未引入独立 watchdog。
- 下一步：M5-02 清理与保留。

## 轮次 22 · 2026-11-21
- 动作：M5-02。新增 `db/retention.go` + `0009_retention.sql`（retention_audit + 索引）：策略默认 activity 30d / server_logs 14d / C2 30d，可经 settings 覆盖并夹到 1..3650；`RetentionPreview` 只算到期条数；`RunRetentionCleanup` 单事务删到期 activity/server_logs/c2_tasks/c2_events/dead c2_sessions 并写审计，dry-run 只回 counts。API：status/policy/cleanup(dry_run)/cleanup-list（按 host_ledger artifact 行生成目标侧清理清单）。
- 结果：dry-run 前后数据不变且无审计、真实清理只删到期行（新行存活）、审计字段正确、非法策略回落 2 组用例全绿；全新库全量 **24 包全绿**（EXIT=0）。提交 `b5f4329`。
- 偏差：清理为手动 API（未加定时调度）；清理清单暂无前端页（接口已就绪）。
- 下一步：M5-03 发布收口（CHANGELOG/README/部署文档、v0.4.0 tag、release workflow）。

## 轮次 23 · 2026-11-21（M5-03 发布，总目标收口）
- 动作：M5-03。CHANGELOG 增补 0.4.0 全量变更（M0–M5 分组、安全默认值、已知限制）；README 新增二开能力与默认开关表；新增 `docs/deploy-v0.4.0.md`（升级步骤、0004–0009 迁移、设置键、C2 启用流程、代理池/清理运维、回滚与排障）。核对 `.github/workflows/ci.yml`（go build/vet + 迁移 + 全量测试、web build）与 `release.yml`（五平台交叉编译 + GitHub Release + Docker 多架构）。
- 结果：全部 24 个 PR 完成，记录三处一致（completed=24 / pending=0）；本地 CI 等脚本全绿；打 **v0.4.0** tag 并推送，触发 release workflow。
- 下一步：总目标达成，goal 标记 complete。

## 发布验证 · 2026-11-21
- main push 触发 `ci` workflow：**success**（go build/vet + 迁移 + 全量 24 包测试 + web build）。
- v0.4.0 tag 触发 `release` workflow：frontend、五平台 binaries、GitHub Release 全部 **success**；docker 推送因仓库未配置 DOCKERHUB secrets 失败 → 已把 docker job 改为 `continue-on-error`（可选步骤，不阻塞发布）。
- 复验（tag 移至 `9c48ba9`）：`ci` run 44866944144 **success**；`release` run 44866949571 **success**（frontend、linux/darwin amd64+arm64、windows amd64、GitHub Release 均 success；docker 失败但 continue-on-error 不阻塞）。**总目标达成。**

## 轮次 24 · 2026-11-21（后续任务 P0 完成，v0.4.1）
- 动作：P0-2/P0-3。新增 scripts/smoke/api_smoke.py（14 个无 LLM/无外网用例）与 ui_smoke.mjs；CI 增加 gofmt 与 API 冒烟步骤；未知路由改为 Go 静态服务返回 404 + 自定义中文 404 页；系统配置 6 个开关补 aria-label、任务页补 h1。新增 dev-docs/BACKLOG.md 作为后续任务唯一状态源。
- 结果：tsc/build:static 通过；API 冒烟本地 14/14；404/开关/h1 用 Playwright 实测通过；CI run 44909962033 success（go+web 含新门禁）。
- P0-1：CHANGELOG 定版 [0.4.1]，tag v0.4.1 → commit 237a26f；release run 44910352144 success（linux/darwin amd64+arm64、windows amd64、frontend、GitHub Release；产物含 SHA256SUMS）。提交 88c485a、237a26f。
- 下一步：P1-4 统一定时调度器（retention 自动清理 / CVE autosync 调度 / 代理池健康巡检）。


## 轮次 25 · 2026-11-21（P1-4 统一定时调度器）
- 动作：新增 server/maintenance.go（maintainer）：后台单循环每分钟 tick，热读 settings；任务=保留自动清理、CVE 自动同步（复用 cveSyncStart 抽出的 launchCVESync）、代理池健康巡检；新增 GET /api/scheduler/status、POST /api/scheduler/run/{name}；支持 ARTEX_SCHEDULER_TICK_SECONDS；默认 retention/CVE 关、代理池巡检开（空池跳过）。
- 结果：单元测试 4 个（settings 默认/到期调度/禁用跳过/RunNow）通过；集成实测 status 默认值+next_run、retention 手动执行删除 40 天前日志并写审计 actor=scheduler、空池巡检跳过、未知任务 400、运行后 last_result/last_run/next_run 回填。
- 下一步：P1-5 保留/清理前端页（复用 scheduler/retention API）。


## 轮次 26 · 2026-11-21（P1-5 保留/清理前端页）
- 动作：新增 web/src/app/(main)/system/retention/page.tsx（策略编辑、到期预览、dry-run、真实清理确认、审计、目标侧清理清单、调度状态与手动执行）；api.ts/types.ts 增加 retention/scheduler 接口与类型；侧栏系统组新增「数据保留」。
- 结果：tsc/build:static 通过；Playwright 实测 7 项全过（页面加载、策略值 30/14/30、调度 3 任务且 retention/proxy 启用、dry-run、真删确认、手动执行调度任务、侧栏入口），0 console error；截图 /tmp/artex-ui-p15-shots/retention*.png。
- 下一步：P1-6 C2 完善（前端页 + listener 删除 API + HTTP beacon）。


## 轮次 27 · 2026-11-21（P1-6 C2 完善）
- 动作：迁移 0010（c2_listeners.kind tcp/http + 任务索引）；c2/payload.go 新增 HTTP Python/Bash/PowerShell 模板；server/c2_http.go 实现 HTTP beacon 服务（token 路径、会话 upsert、queued 任务原子领取、结果回写、事件）、DELETE listener、会话下发任务、植入端生成；c2_server.go 运行时/启停/路由/kind 接入；前端 /function/c2 管理页与侧栏解禁。
- 结果：后端集成 13 项全过（非法 kind、创建/启动/端口、beacon 上线、错误 token 404、下发→领取→回传 success、植入端内容、tcp 生成 400、删除停端口、会话/任务）；go test ./c2 ./server 通过；fresh DB 后 go test ./db 全绿；Playwright 11/11，0 console error；截图 /tmp/artex-ui-p16-shots/c2-page.png。
- 下一步：P1-7 WebShell 物理清理 + 凭据 UI。


## 轮次 28 · 2026-11-21（P1-7 WebShell/凭据闭环）
- 动作：迁移 0011 sessions.artifact_path；registerCore 接收目标路径并校验绝对路径/非法字符；cleanupShell 改为返回结构化结果：有路径时 rm -f 后 test ! -e 校验，失败保留会话并写 host_ledger（OK=false），成功删除会话；凭据新增 POST /api/intranet/credentials、GET /{id}/secret（明文读取写日志）、DELETE /{id}；前端内网中枢「WebShell」加路径登记/路径列/清理提示，「凭据」改为可新增/显示/复制/删除。
- 结果：Go 测试（物理删除真实 temp 文件、相对路径拒绝）+ fresh DB 全量 db/server/c2 均 ok；Playwright 6/6：凭据创建→显示明文→删除、WebShell 路径登记→清理提示「已物理删除并校验…（已校验文件不存在）」且文件消失，0 console error；截图 /tmp/artex-ui-p17-shots/{credentials,webshell-cleanup}.png。
- 偏差：明文读取仅记服务端日志，未落审计表（P2-13 安全审计统一补）。
- 下一步：P1-8 知识库补全（服务端分页、编辑/删除、planner extras、SQL insights）。


## 轮次 29 · 2026-11-21（P1-8 知识库补全）
- 动作：新增 kb/manage.go（ListPage 服务端分页、Update、Delete 含 FTS/正文文件清理、Insights SQL 聚合）；kb API 增 GET /api/kb/entries、PUT/DELETE /api/kb/entry，kb-insights 改用 Insights；agent/planner 增加 SetExtraContextProvider 与 appendPlannerExtras，server/planner_extras.go 注入 KB 三档统计与 CVE 语料量；情报页 KB 增加「全部条目」分页浏览、编辑（复用入库表单）、删除与统计。
- 结果：Go 单测（分页/分类过滤/更新/删除/insights/注入 helper）通过；fresh DB 下 kb/db/server/agent 全绿；API 集成 13 项（分页、category、更新、insights、删除、非法参数）全过；Playwright 5 项（统计 25、20/5 分页、翻页、编辑保存、删除至 24）全过、0 console error；截图 /tmp/artex-ui-p18-shots/kb-browse.png。
- 下一步：P1-9 代理池增强（mihomo 适配器、统一出站、UI 健康展示）。


## 轮次 30 · 2026-11-21（P1-9 代理池增强）
- 动作：新增 proxypool/mihomo.go（Clash/mihomo proxies 列表解析，零 YAML 依赖；http/socks5 导入、ss/vmess 等如实跳过）与单测；kb 增加 SetCVEHTTPClient，server 代理池初始化时把 Sploitus + CVE 同步统一接入代理池回退链；新增 POST /api/proxy-pool/import-mihomo（支持 yaml 文本或订阅 URL）；列表返回认证标记、健康/失败统计、统一出站消费者与未探测时不返回 last_checked；内网中枢代理池 UI 增加导入卡、健康摘要、认证列、最近检查列与跳过原因。
- 结果：proxypool 单测通过；fresh DB 下 proxypool/kb/db/server/agent 全绿；API 集成 8 项（导入 2 跳 1、重复 2、认证标记、健康/统一出站、探测失败状态回写、非法 YAML 400）全过；Playwright 4 项（导入、健康 0、认证/跳过原因、探测失败、删除）全过、0 console error；截图 /tmp/artex-ui-p19-shots/proxypool.png。
- 下一步：P1-10 SSH/SMB 会话登记 + 隧道真机验证（chisel/suo5）。


## 轮次 31 · 2026-11-21（P1-10 SSH/SMB 会话与真机验证）
- 动作：register_session 扩展 kind=ssh/smbexec（schema/目录/运行时同步）；新增 registerSSHCore/registerSMBCore（RoE 门、secret 加密落库、setHost/Touch）；load 按 kind 解密恢复 HTTPShell/SSHShell/SMBShell；POST /api/intranet/sessions 统一登记；内网中枢会话页新增登记表单（SSH/SMB/WebShell）；修复 SSHShell 写文件 base64 -d 重定向语义错误与读文件 base64 换行解码。
- 结果：SSH 协议级测试（内置 sshd：登录/执行/读写/坏密码/门禁）通过；SMB 登记+恢复+无 wmiexec 明确报错通过；真实 chisel 二进制 loopback 隧道 e2e 通过（server/client 进程、socks 入口、经隧道 HTTP 取回 marker、台账 alive）；用户提供的真实 VPS 上 SSH 会话执行 hostname/uname、远端文件读写通过。全量 session/server/tunnel/agent 测试通过。
- 偏差：suo5 真机未验证（无二进制/payload）；SMB 无 Windows 真机（仅登记/错误路径）；VPS 反向回连隧道受 NAT/ssh -R 路径限制，实验未闭环（chisel 以本机 loopback 真机验证）。
- 下一步：进入 P2 验证与加固（P2-11 LLM 全链路 E2E 与治理强制场景）。


## 轮次 32 · 2026-11-22（P2-11 LLM 全链路 E2E + 治理强制场景）
- 动作：新增 scripts/acceptance/llm_e2e.py 可重复验收脚本（标准库，跑在已启动服务上）；用 artex_accept 库中真实 opencode-go 配置（非流式）执行：登录→激活配置→/api/llm/test 真实连通→创建任务→要件「Bash 执行 echo ARTEX-P2-LLM-OK」→轮询终态→按 main/plan/intent 会话拉活动历史核对标记与 usage tokens→登记全局 deny 规则→创建 seed_first_intent 治理任务→校验拦截历史 denied 且 worker 未真实执行。
- 结果（三轮真实运行，最终全绿）：LLM 连通回复 OK；任务 #13 status=done；标记命中 plan 与 intent:40；usage 202535 tokens；拦截规则 #23 命中 worker Bash 调用 status=denied（matched 2、denied 2、executed=false），脚本 EXIT=0。
- 发现（转 P2-13）：planner 自己的直接工具调用未挂 Guard（全局拦截规则对 planner 不生效）；当前验收用 seed_first_intent 把治理场景固定到 worker 路径。
- 下一步：P2-12 全量 race / soak / 升级回滚演练 / 备份恢复。


## 轮次 33 · 2026-11-22（P2-12 全量 race / soak / 升级回滚 / 备份恢复）
- 动作：新增 acceptance 脚本与 README。全量 go vet ./... 通过；go test -race ./... 首轮暴露 14 处数据竞争（agent 运行时钩子被测试/热更新并发写、memory.Store degraded/vecOK 并发读写），改为 agent hooks RWMutex + Set/Get，memory 状态 atomic.Bool；修复后全量 race 全绿（server 67s）。
- 升级回滚演练（scripts/acceptance/upgrade_rollback.sh，真实进程 + PG）：v0.4.1 旧版建任务 → pg_dump(262KB)+data tar 备份 → 新版启动（migrations 9→11、任务持久、/api/tasks 等 6 个核心 API 全 200）→ soak 45s×8 workers 13425 请求 0 错误 health 200（RSS 49952→50288KB）→ 恢复备份 + 旧版回滚（任务可见、schema 9）→ 旧数据再次升级（schema 11、任务可见）。RESULT PASS 25 步。
- 下一步：P2-13 安全审计（含上轮记录的 planner 工具调用未过 Guard 缺口、C2/proxy/会话密钥静置加密与轮换、非回环校验、审计完整性、RoE/Scope 边界）。


## 轮次 34 · 2026-11-22（P2-13 安全审计）
- 动作：① C2 非回环校验（c2.ValidateBindHost + 创建/启动双重复核 + settings.c2_allow_non_loopback）；② master key 轮换：db.RotateSecretKeys 事务内重加密四类密文 + cmd/artex-rotate-key CLI（dry-run/原子替换 key）；③ host_ledger 审计哈希链（迁移 0012、写路径锁表串行、VerifyHostLedger + GET /api/security/audit/verify）；④ planner Guard 缺口修复：agent.Planner.SetHooks + 任务 Guard hooks 接线 + 拦截任务上下文；⑤ 安全验收脚本 scripts/acceptance/security_audit.py。
- 结果：go vet/build/gofmt 全绿；db/c2/guard/server 测试通过；轮换单测（含 dry-run/错误 key 回滚）与哈希链篡改/并发测试通过；真实 artex_accept 上 CLI dry-run+正式轮换（2 条代理）成功，重启后代理 URL 可解密、迁移 12；security_audit.py 在轮换前后均 EXIT=0（凭据/代理/C2 token gcm1、无明文、非回环拒绝/开关恢复、审计 ok）；真实 LLM E2E EXIT=0（任务 done、标记命中 plan+intent、207041 tokens；worker 治理 denied executed=false；planner 治理 new_denied=1 agent_name=planner executed=false）。
- 下一步：P2-14 可观测性（/metrics、结构化日志、清理/同步任务监控），随后 P3。


## 轮次 35 · 2026-11-22（P2-14 可观测性）
- 动作：新增 server/obs.go（手写 Prometheus 文本指标、HTTP 指标中间件含 SSE Flush 透传、回环/Token 抓取控制、JSON 结构化日志与访问日志、observability status handler）；Server 接入 metrics/startedAt 与 /metrics 路由；Manager.TaskStatusCounts、Engine.SumActiveLLMCalls；维护调度器接入结构化日志；新增 scripts/acceptance/observability_check.py。
- 结果：go build/vet/gofmt 全绿、server 包测试通过（指标 handler/远程拒绝/Token 放行/中间件计数/JSON 日志/status 单测）；真实服务以 ARTEX_LOG_FORMAT=json 启动，验收脚本 EXIT=0：/metrics 基础/构建信息/Go runtime/计数增长（3→8）/任务总数/3 个调度任务指标/DB 连接池，observability-status 全字段，结构化 http_request 日志命中；/metrics 远程抓取含 Token 通过、无 Token 403。
- 下一步：P3-15 LLM 配置文档 → P3-16 前端打磨 → P3-17 Docker 镜像与 demo seed，最终补丁 tag。


## 轮次 36 · 2026-11-22（P3-15 LLM 配置 / 运维手册 / FAQ 文档）
- 动作：编写 docs/llm-config.md、docs/ops-manual.md、docs/faq.md；README 与 docs/deploy-v0.4.0.md 增加文档索引与延伸阅读。内容基于真实运行经验（opencode-go zen 需 session_header_key + 非流式、/api/llm/test 需完整字段、C2 回环默认拒绝与开关、代理池/清理调度、master key 轮换、审计校验、/metrics 回环限制等）。
- 结果：三份文档共 456 行；代码块与 token 占位符检查通过（无残留 @F@/@C@）；无代码改动。
- 下一步：P3-16 前端打磨（C2 Soon 页替换、暗色对比、加载骨架/错误边界、401/403 体验、移动端细节）。


## 轮次 37 · 2026-11-22（P3-16 前端打磨）
- 动作：新增 components/error-state.tsx、components/page-skeleton.tsx、components/api-error-toaster.tsx、app/error.tsx、(main)/error.tsx、app/loading.tsx、(main)/loading.tsx；api.ts http() 统一网络/5xx/403/401 体验（401 带 next 回跳）；登录页支持 ?next=；proxy 与主布局跳登录带 next；暗色主题提升对比度；内网中枢加载态改骨架；C2 页面确认已是真实功能页。
- 验证：tsc --noEmit 通过、npm run build:static EXIT=0、Go embed 构建通过；Playwright 13 项全 PASS：暗色 5 页截图、401→/login/?next=%2Ffunction%2Ftasks%2F、登录后回跳 /function/tasks/、403 toast「无权限：无权限-e2e」、加载骨架 role=status 可见、移动端 390px 四页 scrollWidth==390 无溢出。
- 下一步：P3-17 Docker 镜像发布启用（secrets）/ demo seed 数据 / 安装体验，然后补丁 tag 收口。


## 轮次 38 · 2026-11-22（P3-17 Docker 镜像 / demo seed / 安装体验 + v0.4.2 收口）
- 动作：① .github/workflows/release.yml 的 docker job 改为 GHCR（GITHUB_TOKEN + packages: write，多架构 amd64/arm64，ghcr.io/wantong-lab/artex），移除失败的 DockerHub secrets 依赖；docker-compose.yml 默认镜像改 ghcr；② 新增 scripts/demo/seed_demo.py（HTTP API 幂等种子）并 COPY 进镜像 /app/seed_demo.py；③ README 新增「5 分钟体验：灌入 demo 数据」与 GHCR 可见性说明，install.sh 启动后提示；④ CHANGELOG 拆出 [0.4.2] 并写入 P1–P3 全部条目与 P3-17。
- 验证：seed 在全新库实跑 SEED1=0/SEED2=0（2 任务 paused、4 KB、资产写入，二次执行幂等）；release.yml YAML 解析、docker compose config 通过；本地 docker build 与镜像内二进制验证；tag v0.4.2 后将由 Release 工作流产出五平台 zip + GHCR 镜像。
- 下一步：打 v0.4.2 tag → 等 Release 工作流（binaries/release/docker）成功 → 校验 Release 资产与 GHCR 镜像 → 目标收口。


## 轮次 39 · 2026-11-22（v0.4.2 发布收口，P0–P3 完成）
- 发布：tag v0.4.2 → Release 工作流 44929501199 全绿（frontend、darwin/linux/windows 五平台 binaries、release、docker）；Release 资产 5 个 zip + SHA256SUMS；docker job 改用 GITHUB_TOKEN 推 GHCR 成功，ghcr.io/wantong-lab/artex:v0.4.2 / :latest 为多架构 OCI index（本地 docker manifest inspect 验证）。
- 最终验证：go build/vet/test ./... 全绿（全新 artex_final 库）；本地 docker build 成功，镜像内 artex -h 与 /app/seed_demo.py --help 可执行；demo seed 在全新库两次运行均 EXIT=0（幂等）。
- 结论：P0（发布收口）、P1（7 项能力）、P2（4 项验证加固）、P3（3 项体验文档）全部完成，BACKLOG 17/17 勾选；目标完成。


## 轮次 40 · 2026-11-22（v0.4.3 补测与修复）
- 补测：①升级脚本 0012 适配并重跑 upgrade_rollback.sh → RESULT PASS steps=25；②Docker Compose 全流程（本地镜像 + PG16 + /setup + 容器内 demo seed + Playwright 验证 [DEMO] 任务/KB + 优雅停止）；③llm_failover.py 坏 provider→好 provider 真实转移 + 流式 2.8s/非流式 3.3s；④C2 HTTP beacon 真链路测试；SMB 真机测试（Windows 192.168.112.133：自动回退 atexec、复合命令、文件读写删）；⑤UI 冒烟扩展并入 CI ui job；⑥全仓 go test -race。
- 修复：SMB atexec 回退（RPC/DCOM 被拦场景）、atexec 复合命令引号、文件读写 PowerShell 化、impacket 噪声过滤；升级脚本迁移数 12；UI 冒烟路由延迟处理。
- 结论：6 项建议中 5 项完成并有实测证据；隧道跨主机留作已知限制（chisel 单二进制需与平台同架构，macOS→Linux VPS 不适用）。下一步提交、CI 验证并打 v0.4.3。


## 轮次 41 · 2026-11-22（v0.4.3 发布）
- CI 44990406383 全绿（go/web/ui 三 job）；tag v0.4.3 → Release 44990610317 全绿，5 平台 zip + SHA256SUMS 已发布，GHCR ghcr.io/wantong-lab/artex:v0.4.3/:latest 多架构 manifest 验证通过。
- 补测结论：升级（0012）PASS 25 步、Compose 全流程 PASS、LLM 失败转移+流式 PASS、C2 beacon E2E PASS、SMB 真机 PASS（atexec 回退）、UI 冒烟入 CI PASS、全仓 race PASS；隧道跨主机为已知限制。

## 轮次 42 · 2026-11-22（跨 OS/架构隧道 + v0.4.4）
- 实现：Plan 增 target_os/target_arch/client_binary（向后兼容）；Manager 增 chiselClientDir（ARTEX_CHISEL_CLIENT_DIR 或 data/tools/chisel-clients）；Deploy 前探测目标平台并选目标端二进制；Linux 保持 setsid/nohup/AUTH env；Windows 走平台临时 HTTP 下载 + WMI Win32_Process.Create 拉起（atexec 下 Start-Process 会等到超时）、目录/巡检/taskkill 适配、SOCKS 入口在目标侧；server/tunnel_tools.go 注入 client 目录。
- 验证：单元测试（平台名/架构、探测、二进制选择、Windows 命令、路径改写）通过；旧同机 loopback chisel E2E PASS；新 Windows 真机 E2E PASS（platform macOS/arm64 → Windows 192.168.112.133，SMB 会话，SOCKS5 经 Windows 访问平台 LAN HTTP 返回 marker）；go build/vet/gofmt 通过。目标机火绒会秒删 chisel 官方二进制，测试改用 -trimpath -ldflags "-s -w" 重新构建的 client；目标账号/临时目录/进程已清理。
- 发布：提交 148bb81 + CI 修复 3e95efb；CI 45000922837 全绿（go/web/ui）；tag v0.4.4 → Release 45001123212 成功，Release 资产 5 平台 zip + SHA256SUMS 非 draft，GHCR ghcr.io/wantong-lab/artex:v0.4.4 / :latest 多架构 OCI index 验证通过。

## 轮次 43 · 2026-11-22（公网 VPS 隧道验证 + v0.4.5 修复）
- 问题：公网 VPS 目标无法访问平台随机 bootstrap 端口（NAT 只映射 39443/39444）；SSH Exec 等后台 chisel 子进程导致拉起 20s 超时（客户端实际已回连）。
- 修复：新增 ARTEX_TUNNEL_STAGE_PORT 固定下载端口；Unix spawn 改为后台块 stdout/stderr 全量重定向 + pid 文件回读。
- 验证：公网 VPS E2E PASS（macOS 平台 → 39.89.211.55:39444 → Linux VPS 113.20.3.124；SOCKS5 经 VPS 访问本地 HTTP，公网出口 IP=113.20.3.124）；loopback E2E 回归 PASS；VPS 临时进程/目录已清理。
- 发布：提交 10faddf；CI 45025109590 全绿；tag v0.4.5 → Release 45025422076 成功，5 平台 zip + SHA256SUMS 已发布，GHCR ghcr.io/wantong-lab/artex:v0.4.5 OCI index 校验通过。

## 轮次 44 · 2026-11-22（公网 C2 HTTP/TCP 真机验证）
- HTTP：服务启用 C2（c2_allow_non_loopback=true），HTTP 监听器 0.0.0.0:39443（公网映射 39.89.211.55:39443）；VPS 113.20.3.124 运行 Python beacon 植入端上线，平台下发 id/hostname/whoami，结果 uid=0(root)/wantong/root；测试后停止/删除监听器并清理植入端。
- TCP：服务 TCP 监听器 0.0.0.0:39444（公网映射 39.89.211.55:39444）用 settings c2_token 生成植入端，VPS 上线会话 active；另以 c2.Manager 公网 E2E 验证加密信封 exec 返回同一组证据。
- 记录：新增 scripts/acceptance/c2_public_http.py、c2/c2_tcp_public_vps_test.go；未改产品代码。
