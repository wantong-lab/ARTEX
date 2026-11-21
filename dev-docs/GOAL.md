# 目标定义（Goal Mode）

## 目标

在当前 ARTEX 仓库（upstream 0.3.15 基线、AGPL-3.0、Next.js 全源码）上完成产品化改造：
把二开版的后端能力（会话/隧道/知识库/记忆/CVE/Sploitus/C2/代理池/WebShell/链路骨架/治理护栏/分级路由）
以「原源包复制 + 还原胶水重写 + 前端重建」的方式落到本仓库，最终发布 `v0.4.0`。

## 完成标准（全部满足才算完成）

1. ROADMAP 中 M0-02 至 M5-03 共 22 个 PR 全部合入 `main`（M0-01 已完成）。
2. 每个 PR 满足 ROADMAP 第 5 节验收门：`go build ./...`、`go vet ./...`、`go test ./...` 全绿，UI 改动 `npm run build` 通过。
3. 新能力默认关闭或 `warn`，有开关可回退；迁移幂等。
4. 复制的外部代码全部登记在 `dev-docs/upstream-imports.md`。
5. 打 tag `v0.4.0`，CHANGELOG 与部署文档更新完毕。

## 范围与非目标

- 范围：本仓库 Go 后端 + `web/` Next.js 前端。
- 非目标：不采用二开反编译 UI；不复制 81 个 `Reconstructed` 文件；不引入二开的分叉 schema 与 dump。

## 每轮执行协议

1. 读 `dev-docs/state.json` 确定 `current_pr`，读 `dev-docs/TASKS.md` 确认状态。
2. 从最新 `main` 建分支 `<type>/<里程碑>-<序号>-<简述>`。
3. 实现 + 跑验收命令；不达标不进下一步。
4. 更新三处记录：`TASKS.md` 该行状态、`state.json`、`PROGRESS.md` 追加一条。
5. 提交（Conventional Commits，正文写为什么+验收）并推送；更新 `state.json` 的 `current_pr` 指向下一个。
6. 全部完成后打 tag、更新 CHANGELOG，目标标记完成。

## 状态源优先级

`TASKS.md`（唯一状态真相） > `state.json`（机器读） > `PROGRESS.md`（流水）。冲突时以 TASKS.md 为准并修复另两份。