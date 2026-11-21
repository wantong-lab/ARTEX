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