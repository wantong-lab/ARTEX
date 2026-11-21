# chainskel — 反问决策链骨架

平台内置的「场景→判据→转向」决策骨架与反证判据，回答的是**模型在受阻时怎么想**，
而工具回答的是**怎么做**。来源见 `dev-docs/upstream-imports.md`（逐字节复制，未改动）。

## 组成

| 文件 | 作用 |
|---|---|
| `chainskel.go` | 9 类骨架的 `go:embed` 装载、标签规整/校验、`ForIntent` 按意图路由、`Section` 截断（8000 rune） |
| `meta_rules.go` | planner / worker 的 meta 铁律常量（代码固定尾，不可被 DB 正文覆盖） |
| `verifier.go` | 反证 checklist：通用 3 条 + 按 vulnclass 的 7 类专项 |
| `pivot.go` | 停滞时从对应骨架抽「转向」建议；`RefsGuideForIntent` 给 L4 原文路径 |
| `*.md` | 9 类骨架原文（web/ad/app/priv/pwn/recon/rev/pivot/meta） |

## 路由规则

- 意图 payload 有 `chain_tags` 时取其合法值（最多 2 个）；无 tag 时回退 `summary` 关键词匹配。
- 匹配不中、骨架缺失、读取失败一律静默返回空串，**绝不阻断 agent 启动**。
- 注入块带 `<chains-skeleton>` 分区标记：平台内置可信内容，与 `<untrusted-data>` 区分。

## 接线状态

- M1-01：仅落地包与单测（本提交）。
- M1-02：接进 `workerSystem` / `plannerSystem` 固定尾与 `renderIntentTask`（按意图注入骨架）。

## 维护约定

- 新增类别必须同时：加 `<category>.md`、更新 `Categories`、在 `keywordRules` 补关键词、补单测。
- 阈值（单意图 2 类、8000 rune 截断）改动需同步 README 与 ROADMAP。