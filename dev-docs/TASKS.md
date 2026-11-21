# 任务状态板（唯一状态真相）

> 状态取值：待开始 / 进行中 / 已完成 / 阻塞 / 跳过。每轮结束后更新，并在 `PROGRESS.md` 追加一条流水。

| ID | 里程碑 | 分支 | 状态 | commit | 验收 |
|---|---|---|---|---|---|
| M0-01 | M0 | `chore/m0-01-governance` | 已完成 | `8deb132` `77740cf` | 文档与 CI 入库 |
| M0-02 | M0 | `feat/m0-02-migrations` | 已完成 | `7e192c8` | build/vet 通过；db 包（含真实 PG 幂等用例）绿 |
| M0-03 | M0 | `chore/m0-03-schema-check` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M1-01 | M1 | `feat/m1-01-chainskel` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M1-02 | M1 | `feat/m1-02-prompt-tails` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M1-03 | M1 | `feat/m1-03-stuck` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M1-04 | M1 | `feat/m1-04-honeydetect` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M1-05 | M1 | `feat/m1-05-nudges` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M2-01 | M2 | `feat/m2-01-import-session` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M2-02 | M2 | `feat/m2-02-session-tools` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M2-03 | M2 | `feat/m2-03-tunnel` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M2-04 | M2 | `feat/m2-04-cred-scope` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M2-05 | M2 | `feat/m2-05-ui-intranet` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M3-01 | M3 | `feat/m3-01-import-kb-memory` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M3-02 | M3 | `feat/m3-02-kb-api` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M3-03 | M3 | `feat/m3-03-cve` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M3-04 | M3 | `feat/m3-04-ui-intel` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M4-01 | M4 | `feat/m4-01-c2` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M4-02 | M4 | `feat/m4-02-webshell` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M4-03 | M4 | `feat/m4-03-proxypool` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M5-01 | M5 | `feat/m5-01-agent-routing` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M5-02 | M5 | `feat/m5-02-cleanup` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M5-03 | M5 | `docs/m5-03-release` | 待开始 | — | 见 ROADMAP 第 10 节 |

## 阻塞登记

| 日期 | PR | 阻塞原因 | 需要谁 | 状态 |
|---|---|---|---|---|
| — | — | 暂无 | — | — |