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