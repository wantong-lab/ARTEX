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