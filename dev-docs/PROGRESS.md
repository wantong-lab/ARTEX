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