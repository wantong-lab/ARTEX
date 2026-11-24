# 出口代理控制面（P0 精简版）

## 状态

本稿取代上一版偏平台化的完整控制面草稿。P0 只补当前 MVP 真正缺的能力，不新增 policy、binding、event、endpoint 等表，不重写 proxy_pool，不做多实例、多引擎、多租户。

## 背景

v0.4.9 已有固定代理、代理池、Clash 订阅 mihomo、egress_mode。

当前问题：

- 订阅运行时只保存在 Server.proxySub 内存，重启丢失；
- API handler 直接启动 mihomo，没有锁、PID 归属、孤儿清理；
- 只看端口，没有真实出口健康检查；
- 坏订阅没有 last-good 回滚；
- pool 无活动代理时 EffectiveProxy 回退直连；
- 订阅拉取如果走正在配置的代理池，会形成 bootstrap 循环；
- mihomo 配置未清洗危险字段。

## 目标

- 订阅源持久化，重启后恢复或明确 degraded；
- 受管 mihomo 单实例、可停止、无孤儿；
- 只对 target 流量 fail-closed，control 流量不依赖 pool；
- 订阅刷新失败保留 last-good；
- 出口解析有统一接口，Agent/MITM/自更新/控制流量不再各自读 global_proxy；
- 复用 proxy_pool，不迁移旧数据；
- 清洗危险 mihomo 配置；
- 现有页面/API 增量扩展。

## 非目标

P0 不做：

- egress_policies、egress_bindings、egress_events、egress_endpoints；
- 任务/会话级 sticky、rotate、ban detection；
- chain、多级代理；
- DNS/IPv6 泄漏策略；
- 多引擎、多主机、leader election；
- 按 ASN/地区路由；
- 完整审计、指标、SLO；
- 自动下载 mihomo；
- 单页 UI 重构。

## 核心设计

- 固定代理和远程代理池继续使用现有 proxy_pool；
- 受管订阅启动后，本地 mixed 口仍登记为一条 proxy_pool 条目；
- 新增一张 egress_sources，只保存受管订阅源；
- 不新增 endpoint 表；endpoint 就是 proxy_pool 行；
- 不做 policy 表；只用 settings 里的 egress_mode 和两个新开关；
- 单进程假设，进程内互斥，不处理多副本。

## 数据模型

egress_sources：

| 字段 | 说明 |
|---|---|
| id | 主键 |
| name | 显示名 |
| kind | subscription |
| desired_state | active / disabled |
| observed_state | disabled / starting / ready / degraded / failed |
| config_enc | 加密订阅 URL/token |
| config_hash | URL 摘要 |
| last_good_config_enc | 最近成功 Clash YAML（加密） |
| last_good_hash | 最近成功 hash |
| managed_proxy_id | proxy_pool 条目 ID |
| managed_port | 本地 mixed 端口，重启优先复用 |
| etag | 可选 HTTP 缓存 |
| last_modified | 可选 HTTP 缓存 |
| last_fetch_at | 最近拉取 |
| last_error | 脱敏错误 |
| revision | 乐观并发 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

约束：

- P0 只允许一个 subscription source；
- 活跃 source 不能直接删除，先停用；
- revision 用于 API 更新冲突。

## settings

沿用：

- egress_mode = direct / fixed / pool
- global_proxy
- proxy_active_id
- proxy_global_url

新增：

- egress_control_plane_enabled，默认 false；关闭时保持 v0.4.9 行为；
- egress_fail_closed，默认 true；只影响 target 的 pool 模式；
- egress_subscription_refresh_minutes，默认 360，0 表示关闭自动刷新。

## Source 生命周期

状态：

- disabled：用户停用；
- starting：正在启动或刷新；
- ready：引擎/条目可用；
- degraded：当前出口仍可用但最近刷新/探测失败；
- failed：无可用出口。

规则：

- 启动失败不删除 source，只标 degraded/failed 和 last_error；
- 服务启动时如果 enabled 且 desired_state=active，后台恢复，不阻塞 HTTP；
- 停止时 kill mihomo、删除 managed proxy_pool 条目、清空 active（若指向它）。

## Resolver

新增统一接口，概念如下：

- EgressTarget：Agent 打目标、MITM upstream；
- EgressControl：订阅拉取、自更新、Sploitus/CVE 同步。

Resolution 包含：

- ProxyURL
- CACert
- Source
- Blocked

约定：

- Blocked=true 时调用方必须拒绝出站，不能直连；
- ProxyURL 为空且 Blocked=false 才表示显式 direct；
- CA 只在 capture on 的 MITM 场景使用。

target 解析：

- mode=direct：空 proxy；
- mode=fixed：global_proxy；空则 direct；
- mode=pool：proxy_active_id 对应条目；无活动或 failed 时：
  - fail_closed=true：Blocked；
  - fail_closed=false：direct（兼容旧行为）。

control 解析：

- global_proxy 非空则用 global_proxy；
- 否则 direct；
- 不使用 pool，避免订阅拉取依赖代理池。

MITM：

- capture off：Agent 使用 target ProxyURL；
- capture on：Agent 使用本地 MITM，MITM upstream 使用 target ProxyURL；
- target blocked 时 MITM 必须拒绝转发；实现成本高则 P0 拒绝开启 capture，并在 status 说明。

调用点迁移：

- Agent ProxyAddr/ProxyCACert；
- traffic/MITM upstream；
- selfupdate；
- session/browser MCP；
- web search/CVE/Sploitus 改走 control。

EffectiveProxy 只保留给 UI 显示，不作为实际出站依据。

## Supervisor

单进程、单受管 source：

- 互斥锁，保证只有一个 supervisor；
- PID 文件记录 pid、进程启动时间、二进制 hash；
- Unix 用进程组；停止 kill 进程组；
- 端口优先复用 managed_port；不可用才分配新端口；
- 配置目录 data/egress/<source>/，配置 0600；
- 快照 current.yaml/last_good.yaml，原子 rename；
- 启动探针：进程 + 本地端口；
- 就绪探针：经本地代理请求探测 URL 拿到 exit IP；
- 存活探测每 60s，连续 3 次失败重启；
- 重启退避 1s/2s/4s/8s，最多 3 次，之后 failed；
- 孤儿清理只杀 PID 文件且 start time/exe hash 匹配的进程；
- 停止 SIGTERM，5s 超时 SIGKILL。

不做 watch、leader、多实例协调。

## 订阅管线

拉取：

- 从 config_enc 解密 URL；
- 走 control 出口，避免 bootstrap；
- 超时 30s，最大 4MB；
- 非 Clash YAML 时依次尝试 flag=meta、flag=clash、target=clash；
- 失败保留 last-good，标 degraded。

清洗：

- 必须存在非空 proxies；
- 删除 tun、redir-port、tproxy-port、external-controller、secret、proxy-providers、rule-providers、script、profile、interface-name；
- 覆盖 mixed-port、allow-lan=false、mode=rule、log-level=warning；
- YAML 大小/深度/锚点限制。

原子切换：

1. 新配置先在新端口起 mihomo；
2. 新端口就绪探测；
3. 创建/更新 proxy_pool 条目并设为 active；
4. 更新 source 的 managed_proxy_id/managed_port/last_good；
5. 停旧实例、删旧条目；
6. 失败则停新实例，旧实例/条目保留。

端口变化时重建 Agent，保证环境变量指向新端口。

刷新：

- 手动 API；
- 自动定时，带 jitter；
- 并发刷新用 revision/CAS 防重；
- 失败不清空 last-good。

## API/UI

保留现有：

- GET /api/proxy-pool
- POST /api/proxy-pool/subscription
- DELETE /api/proxy-pool/subscription
- GET /api/proxy-pool/subscription

新增：

- POST /api/proxy-pool/subscription/refresh

GET subscription 返回：

- enabled
- observed_state
- desired_state
- managed_proxy_id
- managed_port
- last_fetch_at
- last_error
- egress_mode
- effective_proxy
- fail_closed

不新增 /api/egress 全量 CRUD。

UI 只改现有出口代理池页的 Clash 订阅卡：

- 状态、本地代理、最后刷新、错误；
- 刷新/停止/启动；
- 当前实际出口。

不重做页面，不新增 binding 面板。

## 安全

- 订阅 URL 加密存储，不进日志/事件；
- last_good YAML 加密或 0600 文件；
- 危险 mihomo 配置字段固定清洗；
- mihomo 只监听 127.0.0.1，allow-lan=false；
- PID 文件校验 start time + exe hash，避免杀错；
- mihomo 二进制只接受显式路径/固定目录，可选 ARTEX_MIHOMO_SHA256；
- 日志不打印订阅 URL、节点 URL、密码；
- 订阅 URL 仅允许 http/https，超时/大小限制；内网订阅是明确允许场景。

## 兼容与迁移

- 不迁移 proxy_pool；
- 不新增 endpoint/policy/binding/event 表；
- egress_mode/global_proxy/proxy_active_id 不变；
- 旧 MVP 内存订阅不迁移，重启后重新启动即可；
- egress_control_plane_enabled=false 时行为与 v0.4.9 一致；
- 开启后，pool 无活动条目且 fail_closed=true 才阻断；可关闭 fail_closed 恢复直连；
- 回滚：关闭 flag、停止 source、删除 egress_sources 行；proxy_pool 不受影响。

## 测试

单元：

- ResolveEgress 的 direct/fixed/pool、有/无 active、fail_closed true/false；
- target/control 分流；
- mihomo 配置清洗；
- PID 归属；
- 订阅 flag 候选。

集成：

- 启动 source -> proxy_pool 条目 -> active；
- kill mihomo -> 重启/降级；
- 坏订阅刷新 -> last-good 保留；
- 新端口切换 -> Agent 重建；
- 应用重启 -> source 恢复；
- fail_closed 时 target 不直连；
- capture on/off 两种 MITM upstream。

E2E：

- vless/hysteria2 订阅启动、出口探测、刷新、停止；
- control 拉取不受 pool 故障影响。

## P0 切片

P0-1 持久化与恢复

- egress_sources migration；
- 状态/managed_proxy_id/port/last_good；
- 启动恢复/停止；
- 验收：重启恢复或 degraded，无重复条目。

P0-2 Supervisor

- PID 归属、进程组、端口复用、探针、退避、孤儿清理；
- 验收：kill 后恢复；应用退出无孤儿；并发启动只有一个实例。

P0-3 Resolver/fail-closed

- target/control 解析；
- 迁移调用点；
- capture on/off；
- 验收：pool 无 active 且 fail_closed 时 target 阻断，control 不受影响。

P0-4 订阅刷新/原子切换

- refresh、清洗、last-good、新端口切换；
- 验收：坏订阅不破坏旧出口；端口变化后 Agent 使用新端口。

P0-5 状态/UI/安全

- status API、页面卡、日志脱敏、可选 SHA；
- 验收：状态可解释，日志无 token。

## 验收标准

1. 重启后 source 自动恢复或明确 degraded；
2. 并发 start 只产生一个 mihomo；
3. 应用退出后没有本平台启动的孤儿 mihomo；
4. pool 无 active 且 fail_closed=true 时 target 不直连；
5. control 拉取不受 pool 故障影响；
6. 坏订阅刷新后 effective_proxy 保持旧值；
7. 端口变化后 Agent/MITM 使用新端口；
8. 危险配置字段被拒绝；
9. 日志/API 不出现订阅 token；
10. flag=false 时回归 v0.4.9 行为。

## 风险

- MITM blocked 需要改 traffic 接口，不能只改字符串；
- Windows 进程组/PID 归属需单独验证；
- mihomo 版本差异导致配置兼容问题；
- 端口切换触发 Agent 重建，需避免抖动；
- fail_closed 默认开启可能增加任务失败，需要 UI 明示。

## 延后

以下等真实需求再设计：

- policies/bindings/events；
- sticky/rotate/ban detection；
- 多订阅源、多引擎、多主机；
- DNS/IPv6/chain/ASN；
- 完整审计、指标、SLO；
- 单页出口与代理重构。

## 实施记录（P0，2026-11-24）

已实现：

- db/migrations/0015_egress_sources.sql + db/egress_sources.go：单订阅源表、URL/last-good 加密、revision、状态字段。
- 订阅启动/停止/刷新持久化；服务启动按 egress_control_plane_enabled 恢复 desired_state=active 的 source。
- Supervisor 最小实现：proxySubMu 串行启动、PID 文件、端口优先复用、每端口独立配置目录、启动前清理上次受管目录、进程退出/停止清理。
- Resolver：EgressTarget/EgressControl；pool 无 active 且 fail_closed=true 时 Blocked；control 固定/直连，不走 pool；traffic MITM 增加 blocked 失败关闭。
- 配置清洗：删除 tun/external-controller/secret/proxy-providers/rule-providers/script/profile/interface-name；为避免 geodata 下载，删除 dns/proxy-groups/rules 并用第一个节点 MATCH。
- API/UI：GET/POST/DELETE /api/proxy-pool/subscription、POST refresh；状态增加 source/observed/desired/managed_proxy/fail_closed/control_plane；页面订阅卡支持刷新；系统设置增加 fail-closed/控制面开关。
- 测试：go build/vet、resolver/sanitize 单测；本机真机 E2E：启动 -> 本地出口 200 -> 刷新新端口 -> 重启恢复 -> 停止清理。

未完成/偏差：

- 自动定时刷新未启用（仅手动 refresh；egress_subscription_refresh_minutes 未消费）。
- Windows 进程组/PID 归属未验证；当前 Unix ps 清理。
- 源删除 API 未做；停止即 desired=disabled。
- last-good 回滚路径已保存但尚未做故障注入 E2E。
- 控制面 flag 默认 false；旧行为可回退。

