# 任务状态板（唯一状态真相）

> 状态取值：待开始 / 进行中 / 已完成 / 阻塞 / 跳过。每轮结束后更新，并在 `PROGRESS.md` 追加一条流水。
>
> 分支说明：远端 PR 尚未合并，里程碑按**堆叠**方式推进——当前开发分支为 `feat/m0-02-migrations`，自 M0-02 起依次包含后续提交；表中「分支」列是合并时应使用的名称。

| ID | 里程碑 | 分支 | 状态 | commit | 验收 |
|---|---|---|---|---|---|
| M0-01 | M0 | `chore/m0-01-governance` | 已完成 | `8deb132` `77740cf` | 文档与 CI 入库 |
| M0-02 | M0 | `feat/m0-02-migrations` | 已完成 | `7e192c8` | build/vet 通过；db 包（含真实 PG 幂等用例）绿 |
| M0-03 | M0 | `chore/m0-03-schema-check` | 已完成 | `0a8374b` | 纯逻辑扫描 + PG 表/列校验通过；漏表用例可检出 |
| M0-04 | M0 | `chore/m0-04-test-baseline` | 已完成 | `5910df5` | 空库+全部迁移后 `go test -p 1 ./...` 连续两次 15 包全绿 |
| M1-01 | M1 | `feat/m1-01-chainskel` | 已完成 | `7aed114` | 逐字节复制 + sha256 一致；17 个用例通过（9 类骨架/标签/截断/静默降级） |
| M1-02 | M1 | `feat/m1-02-prompt-tails` | 已完成 | `c8d1747` | 固定尾不可被 DB 覆盖、意图骨架注入、chain_tags 校验；全量 16 包全绿；蜜罐纪律已于 M1-04 补齐 |
| M1-03 | M1 | `feat/m1-03-stuck` | 已完成 | `e5db440` | 三检测器 + steer 接线；10 个单测；全量 16 包全绿 |
| M1-04 | M1 | `feat/m1-04-honeydetect` | 已完成 | `e972c91` | 复制包 sha256 一致；0003 迁移；评分+fact+planner 纪律；全量 17 包全绿 |
| M1-05 | M1 | `feat/m1-05-nudges` | 已完成 | `7fdad77` | 提醒阈值/静默、UA 稳定分配、stderr 前置；全量 17 包全绿 |
| M2-01 | M2 | `feat/m2-01-import-session` | 已完成 | `8d2ca82` | 4 包 + 4 db 文件 + 0004 迁移；包测试全绿；全量 21 包全绿 |
| M2-02 | M2 | `feat/m2-02-session-tools` | 已完成 | `2e74c05` | 四工具 + RoE 门 + gcm1 加密 + host_ledger + 16KB 截断 + 陈旧会话判死；httptest 假马全链路；21 包全绿。偏差：ssh/smb 会话与后台巡检延后 |
| M2-03 | M2 | `feat/m2-03-tunnel` | 已完成 | `a332e93` | 隧道三工具 + 任务级 MITM 热切换；4 用例 + 全量 21 包全绿。偏差：未改 traffic/traffic.go（上游已有 SetUpstreamProxy）；chisel 真机部署未端到端验证 |
| M2-04 | M2 | `feat/m2-04-cred-scope` | 已完成 | `15482b7` | RoE 三态+提取+off/warn/strict、pending_scope 审批即时生效；7 单测+PG 用例；21 包全绿。偏差：pending_scope HTTP API 与任务移交延后 M2-05 |
| M2-05 | M2 | `feat/m2-05-ui-intranet` | 已完成 | `c416901` | API + 五页签 + 侧栏分组 + 移交；next build 与 21 包全绿。偏差：页签为 会话/隧道/凭据/台账/审批（拓扑与投递延后）；未做 mock 渲染验证 |
| M3-01 | M3 | `feat/m3-01-import-kb-memory` | 已完成 | `7717328` | memory 包 + pgvector/关键词降级 + Close 空转修复 + reflector 沉淀 + worker 召回注入；memory/reflector 7 用例；干净库 22 包全绿。偏差：迁移编号 0006（非规格 0005） |
| M3-02 | M3 | `feat/m3-02-kb-api` | 已完成 | `e1114d5` | 5 个 API + kb_search/kb_add + 漏洞入库员（幂等+脱敏）；6 用例；干净库 22 包全绿。偏差：planner 未注入 kb 工具 |
| M3-03 | M3 | `feat/m3-03-cve` | 已完成 | `7b6d0d3` | 可续跑同步（断点 30ms 级验证）+ 5 个 API + cve_search 工具；5 用例；干净库 22 包全绿。偏差：autosync 仅持久化开关（调度归 M5）；Sploitus 线上路径用 stub 验证 |
| M3-04 | M3 | `feat/m3-04-ui-intel` | 已完成 | `a536548` | 情报页三块（知识库/CVE/Sploitus）+ kb add API + 同步中断；next build 与 22 包全绿。偏差：检索分页为客户端分页；Sploitus 需外网 |
| M4-01 | M4 | `feat/m4-01-c2` | 已完成 | `c415314` | 自研 c2 包（信封/帧/管理器）+ 0007 台账 + 运行时/API + 3 工具；9 用例；干净库 23 包全绿。偏差：整包 Reconstructed 按政策重写；仅 TCP 反向；C2 前端留 M5 |
| M4-02 | M4 | `feat/m4-02-webshell` | 已完成 | `85836c1` | 生成/登记/执行/读取/清理闭环 + 内网中枢 WebShell 页签；全链路用例；next build 与 23 包全绿。偏差：清理走 host_ledger 记录并删会话，目标侧落地清除仍需人工/后续 agent 执行 |
| M4-03 | M4 | `feat/m4-03-proxypool` | 已完成 | `4d47718` | 代理池录入/探测/切换/回退 + 出站联动 Sploitus + 中枢页签；4 组用例；next build 与 24 包全绿。偏差：mihomo 适配器后置；仅 Sploitus 出站接入代理 client |
| M5-01 | M5 | `feat/m5-01-agent-routing` | 已完成 | `84b8e78` | 五档预算分类+超时、frontier=8 拒派、接力幂等、任务广播；4 组用例；24 包全绿。偏差：接力只发事件由 planner 决定派发，不自动建意图 |
| M5-02 | M5 | `feat/m5-02-cleanup` | 待开始 | — | 见 ROADMAP 第 10 节 |
| M5-03 | M5 | `docs/m5-03-release` | 待开始 | — | 见 ROADMAP 第 10 节 |

## 已知问题（基线测试债）

| 编号 | 现象 | 根因 | 处置 |
|---|---|---|---|
| T-01 | 空库报 `relation llm_usage does not exist` | `llm_records`/`llm_usage` 由代码运行时建表 | 已修复：`0002_llm_observability.sql` 收敛进迁移 |
| T-02 | agent 用例 `task scope missing` | 断言的是已下线的 `coverage.scope` 结构 | 已修复：改为校验 `task_scope(company)` 行 + 覆盖度分母 |
| T-03 | server 用例 TempDir `directory not empty` | `Server.New` 后台协程在用例返回后仍写文件 | 已修复：`testTempDir` 忽略清理错误，server 测试统一替换 |

> 验收：空库 + 应用全部迁移后，`go test -p 1 ./...` 连续两次 15 包全绿（`5910df5`）。A/B 对照确认这三项与 M0-02 迁移改动无关。

## 阻塞登记

| 日期 | PR | 阻塞原因 | 需要谁 | 状态 |
|---|---|---|---|---|
| — | — | 暂无 | — | — |