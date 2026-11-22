# FAQ 与常见故障

按主题收录实战中出现过的问题。每条给出现象 → 原因 → 处理。

## LLM

### Q1：测试连接返回 403 “Request not allowed”，但 key 看起来没问题？
多为网关强制会话头或模型权限限制。opencode-go zen 需要 `session_header_key=x-opencode-session`；部分免费模型对 IP/地区有限制。补齐会话头、换模型、或确认账号权限后再测。

### Q2：测试连接 200，但任务第一轮就报 400/404？
测试只发一次简单 chat 请求，任务会带工具 schema/长上下文。常见原因：
- format 与端点点型不匹配（anthropic 端点配成 openai）；
- 端点不支持 function calling；
- base_url 多写了 `/chat/completions`。
先看 `/api/llm/records` 的原始错误，再降 max_tokens、换支持工具调用的模型。

### Q3：任务一直“规划中”，没有输出？
provider 在重试/熔断或 SSE 断流。查 `/api/llm/pool` 与任务 `llm_failover_state`；把 profile 改成 `streaming=false`；必要时 `POST /api/llm/pool/reset` 后重试。

### Q4：日志里反复 “empty response retry”？
模型完成但无 content block（常见于不支持工具调用的模型或网关丢包）。empty 层默认重试 2 次；换模型/开非流式；必要时把 empty.attempts 调高（上限 20）。

### Q5：429 / rate limit 频繁？
降低任务 workers、提高 connect 层间隔、开启失败转移链；免费额度场景避免并发探活。

### Q6：API key 存在哪里？如何轮换？
存于数据库 llm_profiles.api_key（读取接口只回 hint）。当前 `artex-rotate-key` 不覆盖 LLM key；轮换直接删除/重建 profile 或更新 api_key，然后重新测试连接。

### Q7：环境变量配了 key，为什么还是走 UI 配置？
一旦 DB 有激活 profile，运行时以 DB 为准；环境变量只作兜底。删掉/停用 profile 或清空激活配置才回落到 env。

## C2

### Q8：创建监听器返回 400 “不是回环地址”？
默认只允许 127.0.0.1/::1。对外监听需显式 `PUT /api/settings {"c2_allow_non_loopback":true}`，并确认授权范围与防火墙。

### Q9：监听器创建成功但 implant 不上线？
- 目标 → 平台方向的 `bind_host:bind_port` 不通（NAT/防火墙/安全组）；
- token/密钥不匹配（重建监听器后旧 implant 失效）；
- 回连地址填的是平台不可达地址。
用目标机 curl/telnet 验证；必要时用隧道或端口映射。

### Q10：token 丢了怎么办？
token 只在创建时返回一次。删除监听器重建，重新生成 implant。

## 代理池

### Q11：探测一直 failed，但本地 curl -x 可以通？
probe_url 默认探测 `http://127.0.0.1:18081/ip`，该 URL 不可达会导致误判；也检查代理是否需要认证（URL 里带 user:pass）。

### Q12：导入 mihomo 配置 0 条？
仅支持 http/https/socks5，ss/vmess 等会跳过；检查 YAML 结构与 type 字段。

### Q13：设置了全局出口，为什么目标流量没变？
全局出口只作用于平台出站（情报同步/漏洞检索等）；目标流量走 MITM 录制代理或任务级代理，两者独立。

## 保留清理与调度

### Q14：清理后目标机痕迹还在？
平台清理的是平台侧留存；目标侧 artifact 需按 `GET /api/retention/cleanup-list` 的 cleanup_cmd 执行。清理动作会写 host_ledger 供复核。

### Q15：调度任务到点不跑？
看 `/api/scheduler/status` 的 enabled/next_run_unix/running/last_error；确认设置已开启、tick 已到、上次任务未超时（10 分钟）。可 `POST /api/scheduler/run/{name}` 手动验证。

## 部署 / 升级 / 迁移

### Q16：启动后/health OK 但页面 404？
二进制未用 `-tags embedui` 构建（Release 包默认内嵌）。本地开发请同时跑 `next dev`，或构建 embedui 版本。

### Q17：迁移数量不对/启动时报 schema 错误？
查看 `select version from schema_migrations order by version`，当前应到 0012_audit_chain。不要手工改表；用 pg_dump 备份后从迁移失败点排查。

### Q18：升级后想回滚？
迁移不可逆：停服务 → 恢复备份数据库 + data 目录 → 启动旧二进制。自动化演练见 `scripts/acceptance/upgrade_rollback.sh`。

### Q19：轮换 master key 后所有人都要重新登录？
是。jwt.key 同时用于 JWT 签名，轮换后旧 token 全部失效；重新登录即可，数据库密文已用新 key 重加密。

## 监控与审计

### Q20：/metrics 返回 403？
默认仅回环可抓。跨机抓取设置 `ARTEX_METRICS_TOKEN`，请求带 `X-Metrics-Token`。不要直接暴露公网。

### Q21：/api/security/audit/verify 返回 ok=false？
broken_at 指向第一条断链行：该行或更早的行被修改/删除。保留现场、导出数据库、排查写入路径；不要直接改哈希值。

### Q22：JSON 日志在哪里？会不会泄露 token？
设置 `ARTEX_LOG_FORMAT=json` 后写 stderr。访问日志只记 path，不记 query（避免 token 泄露）；LLM 记录开启时可能含提示词，按敏感数据管理。

## 治理与拦截

### Q23：给任务加了 deny 规则，worker 还是执行了？
确认规则 match_target/match_type 与工具名/入参匹配；P2-13 起 planner/worker/chat 都过 Guard，但被拦截的调用可能在 planner 阶段就以 denied 记录出现。用 `/api/intercept/history` 核对 agent_name 与 tool_input。

### Q24：RoE strict 模式误拦内网目标？
scope 为空/未审批时按 unknown 处理（不拦）；strict 只拦明确越界目标，loopback 永不拦。把资产加入 scope 或走 pending_scope 审批后生效。


## SMB / Windows 会话

### Q25：Windows 目标 SMB 会话一直失败，或 wmiexec 报 Connection reset？
wmiexec 依赖 SMB（445）+ RPC/DCOM（135 + 高位动态端口）。很多加固后的 Windows 只放行 445，
RPC 动态端口被防火墙拦掉，表现为 SMB 协议握手正常、随后 connection reset。
v0.4.3 起 SMB 会话会自动回退到 `atexec.py`（只走 SMB 服务管理器 + 计划任务），
首次失败后同一条会话后续命令直接走 atexec；也可设 `ARTEX_SMB_BACKEND=atexec` 强制使用。
测试回退逻辑见 `session/smb_real_host_test.go`（默认跳过，需显式提供 Windows 目标）。

### Q26：为什么 wmiexec 已能连上，文件读写仍失败？
atexec 由计划任务执行，外层会给命令追加 `> %windir%\Temp\...` 重定向，
命令自身的 `>` 会被最后一个重定向覆盖。ARTEX 对 atexec 后端的文件读写改用
PowerShell `-EncodedCommand`（无重定向、无嵌套引号），大文件分块写入。

### Q27：跨 OS/架构隧道怎么配？Windows 目标为什么 SOCKS 出口在目标侧？
平台侧 ARTEX_CHISEL_PATH 只负责 chisel server；目标侧按 <os>-<arch>/chisel[.exe] 放在 data/tools/chisel-clients/ 或 ARTEX_CHISEL_CLIENT_DIR。Deploy 用会话命令探测目标（Unix uname；Windows %OS%/%PROCESSOR_ARCHITECTURE%；smbexec 回退 Windows/amd64）并上传匹配二进制。
SOCKS5 入口是目标侧反向 socks（R:127.0.0.1:<port>:socks），所以经它访问的流量从目标主机发出；验证时不要指向平台 Mac 的 127.0.0.1，应指向目标能访问的地址。
Windows 目标的 --auth 会出现在目标进程命令行（WMI Create 无环境变量注入能力）；目标已是立足点，可接受，但不要复用高价值口令。

### Q28：Windows 目标上下载 chisel 后文件消失 / 拉起失败？
先确认目标机的 AV/EDR：实测火绒会在几秒内删除下载到 C:\Windows\Temp 的 chisel 官方二进制（.exe 或改名 .dat 都会删），导致随后 Test-Path 为 false、WMI 拉起报 The system cannot find the file specified。
处理：使用可信来源/自行构建的 client 并加 AV 白名单；测试可用 -trimpath -ldflags "-s -w" 重新构建（本项目实测这样能在火绒环境下保留并跑通）。另外确认目标能访问 ARTEX_CALLBACK_ADDR 的回连地址与平台临时下载端口。
