# 出口代理控制面（Egress Control Plane）

## 背景/问题

当前 v0.4.9 的出口能力是能跑通的 MVP：

- 固定代理 / 代理池 / Clash 订阅 mihomo 三种出口并存；
- 订阅 mihomo 运行时状态保存在 Server.proxySub 内存里；
- 启动/停止由 API handler 直接 exec.Command；
- EffectiveProxy 在 pool 无活动代理时回退直连；
- proxy_pool 表混合用户录入的远程代理和 mihomo 派生的本地代理；
- 没有任务/会话级绑定、自动刷新、快照回滚、审计和真实出口健康检查。

一次成功不等于可运营。红队场景的失败代价是出口泄漏或任务中断，系统设计的要求是**可恢复、可观测、可回滚、失败可预期**。

## 目标

1. 出口源（source）、端点（endpoint）、策略（policy）、绑定（binding）分层；
2. 用期望状态 + reconciler 管理引擎，不靠 API handler 直接拉进程；
3. 订阅支持快照、校验、原子切换、last-good 回滚、定时刷新；
4. 出口解析 fail-closed：不可用时阻断，不静默直连（除非策略显式允许）；
5. 进程可监督：单实例、进程组、启动/就绪/存活探针、退避重启、孤儿清理；
6. 任务/会话级 sticky/rotate/binding 与出口归因；
7. 事件、指标、审计可用；
8. 兼容现有 egress_mode/global_proxy/proxy_pool 数据与 API。

## 非目标

- 首期不重写所有代理协议客户端（vless/hysteria2 仍交给 mihomo/sing-box 等引擎）；
- 首期不实现多租户计费/配额；
- 首期不做跨主机分布式引擎调度；
- 不自动下载未固定哈希的引擎二进制。

## 设计原则与不变量

必须满足：

1. 任一时刻每个 source 最多一个受管引擎实例；
2. 订阅新配置未通过校验/健康检查前，不替换 last-good；
3. 敏感策略下，出口不可用时任务必须失败，不能直连；
4. 配置/令牌不进入日志、命令行参数和公开 JSON；
5. 所有出口变更写事件；
6. 引擎进程退出后由 reconciler 收敛，而不是靠人工重启；
7. API 返回 observed state 与 conditions，不返回假成功。

## 总体架构

~~~text
                 +----------------------------+
                 | 控制面 DB / API / UI       |
                 | sources / policies / bindings|
                 +-------------+--------------+
                               | desired state
                               v
                 +----------------------------+
                 | Egress Reconciler          |
                 | watch + periodic resync    |
                 +-------------+--------------+
                               |
             +-----------------+------------------+
             v                 v                  v
      Subscription      Engine Supervisor    Health/Probe
      Pipeline          (mihomo/sing-box)    (exit IP/latency)
             |                 |                  |
             +-----------------+------------------+
                               v
                 +----------------------------+
                 | Egress Resolver            |
                 | task/session -> endpoint   |
                 | fail-closed policy         |
                 +-------------+--------------+
                               v
                       Agent / MITM / tools
~~~

控制面只写期望状态；数据面由 supervisor 和 resolver 负责。

## 数据模型

新增表（示意，最终以 migration 为准）：

### egress_sources

| 字段 | 说明 |
|---|---|
| id | 主键 |
| name | 名称 |
| kind | fixed / pool / subscription |
| desired_state | active / disabled |
| config_enc | 加密配置（URL、token、YAML 等） |
| config_hash | 配置摘要 |
| last_good_config_enc | 最近一次成功配置 |
| last_good_hash | 最近成功摘要 |
| last_fetch_at / last_error | 拉取状态 |
| created_at / updated_at | 时间 |

### egress_endpoints

| 字段 | 说明 |
|---|---|
| id | 主键 |
| source_id | 归属 source |
| origin | static / managed |
| url_enc | 加密端点 URL |
| status | unknown/active/failed/disabled |
| exit_ip / asn | 最近探测 |
| latency_ms / fail_count | 健康 |
| last_checked | 时间 |

同一 source + url hash 唯一，避免重复条目。

### egress_policies

| 字段 | 说明 |
|---|---|
| id | 主键 |
| scope | global / task / session |
| scope_id | 任务/会话 ID，global 为 0 |
| mode | direct / fixed / pool / chain |
| fallback | deny / direct / fixed |
| leak_policy | fail_closed / allow_direct |
| dns_policy | proxy_dns / system_dns / block |
| ipv6_policy | proxy / block |
| rotation | manual / on_ban / interval |
| sticky | none / task / session / target |

### egress_bindings

任务/会话 -> source/policy 的显式绑定，带 sticky_key 与优先级。

### egress_events

source/endpoint/task/kind/severity/message/created_at。用于审计、时间线和指标。

### 兼容

现有 proxy_pool 继续保留为 legacy 视图/存储；迁移时导入为 legacy-pool source 的 static endpoints。现有 egress_mode/global_proxy/proxy_global_url 映射为 global policy，避免破坏旧库。

## 生命周期与健康

Source 状态机：

~~~text
disabled -> pending -> starting -> ready
                         |  ^        |
                         v  |        v
                      degraded <- failed
~~~

reconcile 流程：

1. 读 desired_state；
2. 订阅：fetch -> validate -> snapshot -> diff；
3. 生成不可变 config snapshot；
4. 启动/更新引擎；
5. 启动探针（端口 + 配置加载）；
6. 就绪探针（经代理请求 exit IP）；
7. 发布 endpoints；
8. 失败保留 last-good，source 标 degraded/failed 并写 event。

健康检查：

- 启动：进程存活 + 本地端口；
- 就绪：经代理访问固定探测地址，拿到 exit IP；
- 存活：定时 exit IP/延迟探测；
- 连续失败阈值后 rotate/重启/降级；
- 引擎退出：reconciler 按退避重建，超过预算标 failed。

## 订阅管线

~~~text
URL/token
  -> fetch (ETag/If-Modified-Since, timeout, size limit, SSRF guard)
  -> adapter (clash-yaml / base64 / sing-box / provider)
  -> validate (proxies 非空、协议支持、YAML 边界)
  -> snapshot (不可变 + hash)
  -> diff
  -> engine reload / restart
  -> readiness probe
  -> commit last_good
  -> event
~~~

失败策略：

- 拉取失败：保留 last-good，source 标 degraded；
- 新配置解析失败：不切换，写 error event；
- 新配置引擎起不来：回滚 last-good；
- 订阅 token 过期：状态显式提示，不阻塞旧出口。

刷新：手动 + 定时（默认 6h，带 jitter）；配置在 DB 加密保存，日志不输出 URL/token。

## Supervisor

- 引擎二进制注册表：版本、SHA256、capability（支持的协议/配置字段）；
- 每 source 单实例锁；
- 独立进程组，停止时 kill 整个组；
- 端口租约分配，释放/复用；
- 配置目录 data/egress/<source>/<revision>/，原子切换 current；
- 启动/就绪/存活探针；
- 指数退避重启（1s/2s/4s...上限 1m，预算内）；
- 启动时扫描孤儿进程/临时目录并清理；
- 优雅停止：停止接新请求 -> drain -> SIGTERM -> 超时 SIGKILL；
- 降权/沙箱：独立用户（可选）、最小环境变量、禁止 shell 拼接；
- 不使用 /Applications/Clash Verge.app 作为生产默认；只作为显式开发 fallback，并记录来源。

## Resolver 与 fail-closed

Resolver 输入 task/session/tool 上下文，输出 endpoint + policy：

- global policy 决定默认模式；
- task/session binding 覆盖；
- sticky 命中固定 endpoint；
- rotate 在失败/封禁后切换；
- 无可用 endpoint 且 leak_policy=fail_closed 时返回错误，调用方阻断；
- direct 只有在 policy 显式允许时可用；
- 现有 Manager.EffectiveProxy() 先委托 resolver，保留旧行为兼容开关。

MITM、Agent env、自更新、Sploitus/CVE 都必须走同一 resolver；禁止各自读 global_proxy。

## API

声明式接口（建议）：

~~~text
GET    /api/egress/status
GET    /api/egress/sources
POST   /api/egress/sources
PUT    /api/egress/sources/{id}
DELETE /api/egress/sources/{id}
POST   /api/egress/sources/{id}/refresh
POST   /api/egress/sources/{id}/rotate
GET    /api/egress/events
GET/PUT /api/egress/policies
GET/PUT /api/egress/bindings
~~~

返回 observed_state + conditions + last_error + last_fetch_at，不返回明文 secret。

兼容：

- 现有 /api/proxy-pool/subscription 保留为 source=legacy-subscription 的适配器；
- 现有 /api/proxy-pool entries 映射 static endpoints；
- egress_mode 保留为 global policy 的兼容字段。

## UI

单页「出口与代理」：

- 当前生效路径：mode -> source -> endpoint -> exit IP/ASN；
- source 列表：状态、上次刷新、错误、健康；
- 操作：启动/停用、刷新订阅、立即轮换、测试出口；
- 事件时间线；
- 任务/会话绑定（高级）；
- 不再出现两个订阅输入框、两个全局代理的重复入口。

## 安全与 OPSEC

- secrets 使用现有 master key 派生密钥加密（类似 proxy_pool），不落明文；
- 订阅 URL 不写日志/事件 message；
- 防 SSRF：限制 scheme、可选 allowlist、禁止 link-local/loopback（按产品策略）；
- YAML 大小/深度/锚点限制；
- 引擎二进制固定版本/SHA，禁止自动下载；
- 进程最小权限、独立 runtime 目录、文件 0600/0700；
- 所有出口变更审计；
- fail-closed 默认；
- 代理/节点 URL 脱敏展示；
- 任务删除/归档时清理 binding 与 runtime，不删除审计事件。

## 兼容性与迁移

阶段 1：

- 新增表、双写/迁移脚本；
- 现有 proxy_pool 行导入为 legacy endpoints；
- 现有 mihomo 运行时迁移为一个 subscription source；
- 旧 API 适配新模型；
- 不改变 UI 外部链接。

阶段 2：

- 新 UI 上线，旧代理池页保留只读/跳转；
- 定时刷新/事件/任务绑定开启。

## 测试计划

单元：

- policy resolver 全组合（direct/fixed/pool/chain、fallback、sticky）；
- 订阅格式 adapter、base64、YAML 边界；
- supervisor 状态机/退避/端口租约。

集成/故障注入：

- 引擎启动失败、端口占用、配置非法；
- 订阅超时/403/超大响应/格式变化；
- 引擎崩溃、kill -9、应用重启；
- last-good 回滚；
- fail-closed 泄漏测试（无 endpoint 时禁止直连）。

E2E：

- 固定代理/订阅 mihomo/任务绑定/rotate；
- 重启恢复；
- 事件/指标。

## P0 实施拆解

P0-1 数据与兼容层

- 新增 egress_sources/endpoints/policies/bindings/events migrations；
- legacy proxy_pool 导入；
- 旧 egress_mode 映射到 global policy；
- 验收：旧数据可见、旧 API 兼容。

P0-2 Supervisor

- server/egress_supervisor.go；
- 单实例锁、进程组、端口租约、配置快照、启动/退避/停止/孤儿清理；
- 验收：并发启动只有一个实例；kill 后自动恢复；应用退出无孤儿。

P0-3 解析器与 fail-closed

- server/egress_resolver.go；
- global policy + task/session binding；
- 真实 exit IP 健康检查；
- 无端点时按 policy 阻断；
- 验收：pool 不可用不直连；fixed/pool/sticky 生效。

P0-4 订阅管线与 last-good

- 订阅 fetch/format/validate/snapshot/diff/rollback；
- 手动刷新 + 定时刷新；
- 验收：坏订阅不影响现有出口。

P0-5 事件/状态/观测

- egress_events、metrics、API status/conditions；
- 验收：启动/失败/刷新/切换可追溯。

P0-6 UI 最小闭环

- 单页 source/status/refresh/test/stop；
- 兼容旧代理池页跳转；
- 验收：不靠日志能定位当前出口与异常。

## P1/P2

P1：

- 任务/会话 sticky/rotate/ban detection；
- 多订阅源、多个引擎（mihomo/sing-box）；
- 端口/流量指标；
- 审计权限。

P2：

- 多主机 engine 调度；
- 按目标 ASN/地区路由；
- 策略审批流；
- 代理 burn 台账。

## 验收标准

1. 应用重启后：source 自动恢复或明确 degraded，无孤儿进程；
2. 引擎崩溃：退避重启，事件可查；
3. 订阅刷新失败：last-good 保持，旧流量不断；
4. pool 无可用端点且 fail_closed：任务收到错误，不直连；
5. fixed/pool/sticky 与 task binding 行为有测试；
6. 订阅 token/节点凭据不出现在日志/API 明文；
7. 旧 proxy_pool/egress_mode API 兼容；
8. UI 一眼能看出当前实际走哪条出口。

## 风险

- 进程监督跨平台差异（Windows job object/Unix process group）；
- 订阅格式碎片化；
- 引擎版本差异；
- 迁移期双模型一致性；
- fail-closed 可能让任务更频繁失败，需要明确产品预期；
- 沙箱/降权在不同部署形态落地成本不同。

## 备选方案

- 保留 handler 直接 exec（拒绝：无法恢复/回滚/审计）；
- 只做 UI 合并（拒绝：运行时问题不变）；
- 完全依赖外部 Clash 客户端（拒绝：平台无法保证 fail-closed 与归因）；
- 自动下载 mihomo（拒绝：供应链风险，改为固定版本/哈希注册表）。

## 实施记录

- 本文为 proposed/设计稿；当前 MVP 实现为 server/proxy_subscription.go + /api/proxy-pool/subscription。
- MVP 保留可用；P0 按上述顺序替换控制面，不一次性重写。
