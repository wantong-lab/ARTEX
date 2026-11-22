# 后续任务板（P0–P3）

> 单一状态源。每项完成后更新本文件、CHANGELOG 与 dev-docs/PROGRESS.md，并提交推送。
> [x]=已完成，[~]=进行中，[ ]=待开始。

## P0 发布收口与质量门

- [x] P0-1 v0.4.1 补丁发布：CHANGELOG 定版、打 tag、release 五平台构建与 Release 成功。
- [x] P0-2 验收脚本入仓 + CI 门禁：scripts/smoke/api_smoke.py（14 用例）、ui_smoke.mjs（手动）、CI 增加 gofmt 与 api smoke。
- [x] P0-3 UI 小修：系统配置 6 个开关补 aria-label；任务页补 h1；未知路由返回 404（自定义中文 404 页 + Go 静态服务 404 语义）。

## P1 能力补全（对齐 ROADMAP 偏差）

- [x] P1-4 统一定时调度器：retention 自动清理、CVE autosync 实际调度、代理池健康巡检（settings 可控）。
- [x] P1-5 保留/清理前端页：策略编辑、预览、dry-run、审计、目标侧清理清单。
- [x] P1-6 C2 完善：前端页（监听器/会话/任务）、listener 删除 API、HTTP beacon、多语言 implant（DNS 可拆）。
- [x] P1-7 WebShell/凭据闭环：目标侧马体物理删除+校验；凭据注入/复用 UI。
- [x] P1-8 知识库补全：服务端分页、条目编辑/删除、planner extras 注入 kb/cve、insights SQL 聚合。
- [x] P1-9 代理池增强：mihomo 适配器、其余出站统一接入、UI 认证延迟/健康度。
- [x] P1-10 SSH/SMB 会话登记 + 隧道真机验证（chisel/suo5）。

## P2 验证与安全加固

- [x] P2-11 LLM 全链路 E2E + 治理强制场景（真实 LLM 全链路 + 全局拦截规则 worker 强制；预算/拒派/接力由 agent M5 单测覆盖）。
- [x] P2-12 全量 race / soak / 升级回滚演练 / 备份恢复。
- [x] P2-13 安全审计：C2/proxy/会话密钥静置加密与轮换、C2 非回环校验、审计完整性、RoE/Scope 边界。
- [x] P2-14 可观测性：/metrics、结构化日志、清理/同步任务监控。

## P3 体验与文档

- [x] P3-15 文档：LLM 配置（opencode-go + session_header_key）、C2/代理池/清理运维手册、FAQ。
- [x] P3-16 前端打磨：C2 Soon 页替换、暗色对比、加载骨架/错误边界、401/403 体验、移动端细节。
- [x] P3-17 Docker 镜像发布启用（secrets）、demo seed 数据、安装体验。

## 变更记录

- 2026-11-21：建立任务板；P0-2、P0-3 完成（提交后回填 commit）。

- 2026-11-21：P0 全部完成。提交：88c485a（P0-2/P0-3）、237a26f（v0.4.1 定版）。CI run 44909962033 success；release run 44910352144 success（五平台 zip + SHA256SUMS，非 draft/预发布）。下一步 P1-4 统一定时调度器。

- 2026-11-21：P1-4 完成（server/maintenance.go + cve launch 复用 + /api/scheduler/*）。集成验证：默认任务表、retention 手动执行删到期行并写 actor=scheduler 审计、空池巡检跳过、未知任务 400、状态 last/next 回填。

- 2026-11-21：P1-5 完成（/system/retention：策略/预览/dry-run/真删确认/审计/清理清单/调度状态+手动执行）。Playwright 7 项交互全过、0 console error。

- 2026-11-21：P1-6 完成（迁移 0010 kind；HTTP beacon + token 路径；c2_http.go 会话/任务复用；DELETE listener；POST session task；GET implant 三语言；/function/c2 前端页）。后端集成 13 项全过；Playwright 11 项 E2E 全过（含真实 beacon 领取/回传、端口关闭），0 console error；db 全量测试 fresh DB 全绿。

- 2026-11-21：P1-7 完成（迁移 0011 artifact_path；清理先 rm 后 test ! -e 校验，失败保留会话；凭据 POST/GET secret/DELETE + UI 新增/显示/复制/删除）。Go 测试（物理删除删文件、相对路径拒绝）+ 全量 db/server/c2 全绿；Playwright 6/6（凭据增显删、WebShell 路径登记、物理清理校验）、0 console error。

- 2026-11-21：P1-8 完成（kb/manage.go 分页/更新/删除/SQL Insights；kb API 三个新路由；planner extras 注入 KB/CVE；情报页全部条目分页+编辑+删除）。Go 单测 + kb/db/server/agent 全绿；API 集成 13 项、Playwright 5 项（分页 20/5、编辑、删除、统计）全过。

- 2026-11-21：P1-9 完成（proxypool/mihomo.go 解析依赖零 YAML 库；import-mihomo API；Sploitus+CVE 统一出站；UI 认证/健康/延迟/最近探测 + 导入结果）。API 集成 8 项、Playwright 4 项、单测与全量测试全绿。

- 2026-11-21：P1-10 完成（SSH/SMB 登记 + 恢复 + UI；SSH 协议级测试；真实 chisel 二进制 loopback e2e；真实 VPS SSH 会话执行/读写）。偏差：suo5 二进制/payload 未提供未真机；SMB 无 Windows 目标仅验证登记/错误路径；VPS 反向回连隧道受 NAT/ssh -R 实验性验证未闭环，chisel 真机以本机 loopback 验证。

- 2026-11-22：P2-11 完成。新增 scripts/acceptance/llm_e2e.py（登录→激活真实 LLM→连接测试→创建任务→planner/worker Bash 执行标记→活动历史/token 用量核对→治理拦截规则下发 worker 工具调用并确认未执行）。真实运行：任务 done，标记命中 plan+intent 会话，usage 202535 tokens；拦截规则命令 status=denied、executed=false。发现待办：planner 的直接工具调用未经过 Guard（全局拦截规则不覆盖 planner），已记入 P2-13 安全审计项。

- 2026-11-22：P2-12 完成。全量 go vet ./... + go test -race ./...（fresh DB）全绿；修复 agent 钩子与 memory 状态真实数据竞争（RWMutex / atomic.Bool）。新增 scripts/acceptance/upgrade_rollback.sh：真实 v0.4.1 旧版→建任务→pg_dump+data 备份→新版升级（schema 9→11、任务持久、6 个核心 API 200）→soak 45s×8 workers 13425 请求 0 错误（RSS 49952→50288KB）→恢复备份+旧版回滚（任务可见、schema 9）→旧数据再升级（schema 11、任务可见），RESULT PASS 25 步。

- 2026-11-22：P2-13 完成。① 静置加密复核 + master key 轮换 CLI（dry-run/正式在 artex_accept 上轮换 2 条代理 URL；重启后代理 URL 正常解密，迁移 12）；② C2 非回环默认 400 拒绝、开启 c2_allow_non_loopback 后放行并恢复默认；③ host_ledger 哈希链（0012 迁移）+ /api/security/audit/verify，篡改/并发单测通过；④ planner 工具调用接入 Guard：修复前 planner 直接执行 Bash 绕过拦截，修复后真实 LLM 治理任务由 planner 发起的 Bash 调用被打上 agent_name=planner 的 denied 记录且未执行；⑤ RoE/Scope 由 guard 既有三态单测覆盖（strict 拦截越界、loopback 放行）。安全验收脚本 security_audit.py 在轮换前后两次 EXIT=0。

- 2026-11-22：P2-14 完成。新增 /metrics（HTTP 请求计数/耗时/在途、任务计数、DB 连接池、LLM 在途、维护调度任务 enabled/running/interval/last_run/last_error、Go runtime；默认回环抓取 + ARTEX_METRICS_TOKEN 跨机抓取）、/api/observability/status JSON 汇总、ARTEX_LOG_FORMAT=json 结构化访问日志与调度事件；observability_check.py 实跑 EXIT=0（计数 3→8、3 个调度任务指标、DB 连接、JSON http_request 日志命中）。

- 2026-11-22：P3-15 完成。新增 docs/llm-config.md（205 行：三种格式、profile 全字段、4 个实战配方含 opencode-go+session_header_key+非流式、流式/非流式、失败转移/轮询/五层重试、用量记录、代理、排障表、安全边界）、docs/ops-manual.md（155 行：C2 启停/回环约束/植入与会话/排障、代理池录入探测切换/全局出口/mihomo 导入、保留清理 dry-run/cleanup-list/调度与监控、升级回滚密钥轮换、安全审计）、docs/faq.md（96 行/24 条实战问答）；README 与 deploy 文档补充索引。

- 2026-11-22：P3-16 完成。① C2 Soon 已被 P1-6 的 456 行真实页面替换，侧栏无 soon/disabled 项（实跑确认页面可用）；② 暗色对比：muted-foreground/border/input/ring/图表色板调整；③ 新增 ErrorState + app/error.tsx + (main)/error.tsx、PageSkeleton + app/loading.tsx + (main)/loading.tsx，内网中枢三处加载态改骨架；④ API 错误体验：网络/5xx toast、403 统一「无权限」toast、401 跳 /login?next= 且登录后回跳；⑤ 移动端 390px 四页无横向溢出。Playwright 13 项全 PASS（5 张暗色截图、401 next 回跳、403 toast、骨架可见、4 页移动端零溢出），tsc/静态构建通过。

- 2026-11-22：P3-17 完成。① Release 的 docker job 从 Docker Hub（secrets 未配置导致 v0.4.1 该 job 失败）改为 GHCR + GITHUB_TOKEN（permissions: packages: write），多架构 linux/amd64+arm64 推送 ghcr.io/wantong-lab/artex，compose 默认镜像同步；② 新增幂等 demo 种子 scripts/demo/seed_demo.py（2 个已暂停 [DEMO] 任务、4 条 KB、4 个示例资产，example.test/192.0.2.0/24），镜像内置 /app/seed_demo.py；③ README 增加「5 分钟体验：灌入 demo 数据」、安装脚本提示、GHCR 可见性说明；④ 本地 docker build 与 seed 实跑验证。

- 2026-11-22：**P0–P3 全部完成，v0.4.2 发布收口**。Release 工作流 44929501199 成功（frontend/5 平台 binaries/release/docker 全绿）；GitHub Release v0.4.2 非草稿，资产 artex-0.4.2-{darwin,linux}-{amd64,arm64}.zip + windows-amd64.zip + SHA256SUMS；GHCR 多架构镜像 ghcr.io/wantong-lab/artex:v0.4.2 与 :latest 可拉取（OCI index 含 amd64/arm64）；最终 go build/go vet/go test ./... 全绿；本地 docker build 与镜像内 artex -h、/app/seed_demo.py 验证通过。

- 2026-11-22（v0.4.3 补测）：针对上轮测试审计的 6 项建议逐项落地：①v0.4.1→v0.4.2 升级脚本适配 0012 并重跑 25 步 PASS；②Docker Compose 全流程（setup/seed/UI/优雅停止）实测通过；③LLM 坏 provider 真实失败转移 + 流式/非流式探测 EXIT=0；④C2 HTTP beacon 真链路测试入库；SMB 真机测试在 192.168.112.133 通过（wmiexec RPC 不可达→atexec 自动回退，修复文件读写/复合命令/噪声过滤）；隧道跨主机因 chisel 单二进制绑定平台架构，macOS→Linux VPS 无法在同一路径同时运行 server/client，暂未验证（已知限制）；⑤UI 冒烟扩展（暗色/移动端/骨架/401/403/页签/404）并接入 CI `ui` job；⑥全仓 go test -race 重跑 RACE_EXIT=0。
