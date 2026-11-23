# 设计稿：C2 入站回调中继（C2 Ingress Relay）

- 日期：2026-11-22
- 状态：已实施（P0 v0.4.7 / P1 v0.4.8）
- 关联：v0.4.6 C2→chisel 数据面、v0.4.5 公网 VPS 隧道、v0.4.4 跨 OS 隧道
- 关键词：C2 ingress、reverse callback、chisel local forward、VPS 节点、Windows 目标

## 1. 背景与问题

真实拓扑：

~~~text
ARTEX 平台（家庭/内网，公网端口映射到平台）
   │  chisel（平台 server，VPS client）
   ▼
VPS 测试节点（公网）
   │  测试流量（SOCKS/portfwd）
   ▼
目标（如 192.168.112.133 Windows）
~~~

当前能力：

- 平台 → VPS 的出站数据面：chisel reverse SOCKS/portfwd 已可用；
- C2 监听器：监听在平台，目标植入端回连平台地址；
- C2→隧道：v0.4.6 支持用 C2 会话作为控制面部署 chisel 数据面。

缺口：目标被拿下后，C2 植入端必须回连 VPS 节点，不能回连平台地址；当前没有“VPS 侧监听、平台侧收连接”的隧道模式，也没有把 C2 listener 环回绑定与回调地址分离的产品化入口。

## 2. 目标与非目标

### 目标

1. 目标植入端只回连 VPS 公网地址；平台 C2 listener 只绑 127.0.0.1，不暴露平台地址。
2. 复用现有 chisel 链路与 C2 runtime，不新增第二套隧道协议。
3. 支持 TCP/HTTP 两类 C2 监听器。
4. VPS 节点、C2 listener、隧道三者有台账/健康/清理；多 VPS 可归属。
5. 与现有 work 任务 MITM/SOCKS 数据面共存：平台经 VPS 出网测试，目标经 VPS 回调，两条方向互不干扰。

### 非目标（P0/P1）

- 不实现 C2 socks5 内置（数据面仍由 tunnel SOCKS 承担）；
- 不做 VPS 侧 TLS 终止/域名管理（P2 加固）；
- 不做 C2 全链路多节点自动故障转移（P2）；
- 不改变现有直连 C2 监听器行为。

## 3. 总体方案

新增一个隧道方向：ingress（入站中继），由平台 chisel server + VPS chisel client 组成：

~~~text
target implant ──TCP/HTTP──> VPS:39444
                              │  chisel client local forward
                              │  0.0.0.0:39444:127.0.0.1:<platform_c2_port>
                              ▼
                     platform chisel server
                              │
                              ▼
                    127.0.0.1:<platform_c2_port>  (C2 listener, loopback only)
~~~

chisel 客户端本地转发语法已用本机二进制确认：

~~~text
chisel client <platform-chisel-url> 0.0.0.0:39444:127.0.0.1:<c2_port>
~~~

与现有 --reverse 模式的区别：

- 现有 socks/portfwd：平台侧监听，VPS 侧发起回连，数据从平台进入 VPS；
- 新 ingress：VPS 侧监听，数据从 VPS 进入平台；二者共用同一条 chisel 连接（由 VPS client 主动连平台 server）。

## 4. 详细设计

### 4.1 数据模型

tunnels.kind 扩展一个值：

~~~text
ingress
~~~

迁移 0013：重建 tunnels.kind CHECK，加入 ingress（保持 socks/portfwd 不变）。

tunnel.Plan 新增字段：

| 字段 | 含义 |
|---|---|
| kind=ingress | 入站中继模式 |
| listen_host / listen_port | VPS 侧监听地址/端口（目标回连入口） |
| target_host / target_port | 平台侧环回目标（默认 127.0.0.1:<c2_port>） |
| c2_listener_id | 关联 C2 listener（用于生命周期/展示） |
| c2_callback_host | 返回给植入端的 VPS 公网地址 |
| platform_chisel_port | 平台 chisel server 端口（现有 ListenPort 语义） |

deploy_params 保存完整 Plan；via_session_id 仍记录部署所用 VPS 会话。

P0 不新增独立表：用一条 tunnel 记录表示一个 VPS ingress；C2 listener 与 tunnel 的关联通过 deploy_params 的 c2_listener_id 与工具返回结果维持。P1 可增加 c2_ingress 表做节点/健康台账。

### 4.2 C2 listener 绑定策略

- ingress 场景强制 C2 listener bind_host=127.0.0.1；
- 现有 c2_allow_non_loopback 不参与 ingress；loopback 是安全默认；
- listener 端口由平台自动分配（或显式指定）；
- 每个 VPS ingress 建议一个独立 listener/token，便于 C2 会话归属到节点；
- HTTP 与 TCP 都支持；HTTP 的 beacon URL 使用 VPS 回调地址。

### 4.3 植入端回调地址

- HTTP：沿用 GET /api/c2/implant?listener_id=...&base_url=http://<vps>:<port>；P1 在 c2_ingress_deploy 返回 base_url；
- TCP：新增生成能力（工具/内部函数），使用 c2.PythonImplant(callback_addr, runtime_token, key, id)；回调地址来自 ingress，而不是 listener bind 地址；
- 回调地址必须经过授权确认：只允许 ingress 记录的 vps host/port，不接受任意 URL。

### 4.4 工具/接口

P0 新增 agent 工具：

c2_ingress_deploy

参数：

| 参数 | 必填 | 说明 |
|---|---|---|
| session_id | 是 | 到 VPS 的立足点会话（SSH/WebShell/C2） |
| c2_listener_id | 是 | 已创建的 C2 listener（必须 loopback） |
| vps_port | 是 | VPS 侧监听端口（目标回连端口） |
| vps_host | 否 | VPS 公网地址；默认从 session/用户输入/环境推断，缺失则报错 |
| platform_chisel_port | 否 | 平台 chisel server 端口；默认按现有映射/配置 |

流程：

1. 校验 C2 runtime 启用、listener 存在、bind 为 loopback、listener 已/可启动；
2. 启动 listener；
3. 生成 ingress Plan（kind=ingress，VPS listen host/port，target 127.0.0.1:c2_port）；
4. 经 session 部署 chisel client（固定 listen_port=平台 chisel 端口，不启动平台反向监听）；
5. 校验 VPS 端口监听/隧道 alive；
6. 返回 tunnel_id、listener_id、callback_host、callback_port、base_url（HTTP）、token 状态。

c2_ingress_teardown

参数 tunnel_id；停止隧道 → 停止关联 C2 listener → 清理台账/进程；幂等。

### 4.5 生命周期与健康

启动检查：

- C2 listener status=running；
- tunnel state=alive；
- VPS listen 端口存在（session Exec ss -ltn / Windows netstat -ano）；
- 可选：平台从公网 dial VPS:port 成功。

巡检：

- 复用 tunnel Manager 健康巡检；
- 每轮确认 VPS 端口仍监听、chisel client 进程存活；
- C2 session 的 remote_addr 记录中继来源（VPS 连接），用于归属。

清理：

- 停 tunnel（kill VPS client / 平台 server）；
- 停 C2 listener；
- 删除 ingress 关联；
- Windows 目标侧植入端由测试/任务自行清理，平台不主动杀目标进程。

### 4.6 安全与 OPSEC

- 平台 C2 listener 只绑 127.0.0.1，公网只暴露 VPS 回调端口；
- VPS 中继是纯 TCP，不解析/记录 C2 内容；
- token 仍是 C2 握手凭据；HTTP beacon token 出现在 URL 路径，建议 VPS 侧限制来源 IP/速率；
- 平台 chisel server 仍用 authfile，AUTH 不进入命令行；
- 文档禁止把 VPS 回调端口无差别对公网开放；优先目标出口 IP 白名单；
- 日志只记录 listener/tunnel id、VPS host/port、session id，不记录 token/植入端内容。

### 4.7 多 VPS 节点

- 每个 VPS 一条 ingress + 一个独立 loopback C2 listener/token；
- 工具返回 callback_addr，植入端显式绑定该节点；
- 平台按 listener_id 归属 C2 会话，避免多个 VPS 共用一个 listener 时无法区分 remote node；
- P1 可增加节点名/标签。

## 5. 兼容性

- 现有 socks/portfwd 计划、部署、健康、清理逻辑不变；
- 现有 C2 直连 listener（非 loopback）行为不变；
- tunnel_deploy 保持对 session_id 的兼容；ingress 用独立工具，不改原 kind 语义；
- 需要新增迁移 0013；旧数据 kind 值不受影响。

## 6. 测试计划

### 单元

- ingress Plan 生成：client args 为本地转发、不含 --reverse；server args 正常；
- VPS/平台端口方向不混淆；
- C2 callback 地址生成只取 ingress 记录；
- teardown 幂等。

### 真机 E2E（用户指定拓扑）

~~~text
ARTEX 平台 ──chisel──> VPS 113.20.3.124
VPS:39444 (ingress) ──> 127.0.0.1:<platform_c2_port>
目标 Windows 192.168.112.133 ──HTTP beacon──> 39.89.211.55:39444
~~~

步骤：

1. 平台创建 loopback HTTP C2 listener 并启动；
2. 平台经 VPS SSH 会话部署 ingress（VPS listen 39444 → platform C2）；
3. 生成 HTTP PowerShell 植入端，base_url 填 VPS 回调地址；
4. 在 133 Windows 执行植入端；
5. 断言 C2 会话 active、remote_addr 为 VPS 中继连接；
6. 下发 whoami/hostname 任务并收到结果；
7. 清理 Windows 植入端、VPS ingress、platform listener。

### 回归

- 现有 tunnel socks/portfwd E2E；
- C2 TCP/HTTP 直连测试；
- v0.4.6 C2→tunnel E2E。

## 7. 实施阶段

- P0（本稿对应）：ingress kind + Plan + chisel local forward + loopback listener + c2_ingress_deploy/teardown + TCP/HTTP 回调生成 + Windows 133 E2E。
- P1：UI 入口、节点标签/台账表、健康巡检、多 VPS 管理。
- P2：TLS/域名、源 IP ACL、故障转移。

## 8. 风险与限制

- VPS 公网回调端口是新增暴露面，必须按授权范围使用；
- Windows 133 需要能访问 VPS 回调端口（出网策略/防火墙需确认）；
- 平台 chisel server 必须能被 VPS 访问（家庭公网映射）；否则需反向角色设计；
- 中继增加一跳，时延/稳定性受 VPS 影响；
- HTTP beacon 轮询延迟高于 TCP 长连接。

## 9. 验收标准

- 目标只配置 VPS 回调地址，不出现平台地址；
- 平台 C2 listener 仅监听 127.0.0.1；
- 133 Windows 上执行 HTTP 植入端后，平台能看到 active C2 会话并完成任务；
- 清理后 VPS 无监听/进程残留、平台 listener 停止；
- 设计稿、实现、测试、CHANGELOG/记录同步。

## 10. 参考

- chisel client 本地转发语法（本机 --help 实测）：local-host:local-port:remote-host:remote-port
- v0.4.6 C2→tunnel 设计：server/c2_session_adapter.go、server/c2_tunnel_e2e_test.go

## 11. 实施记录（P0）

- 提交：2a5fe54（feat），503c9d7（迁移测试修正）；迁移 0013（tunnels.kind 增加 ingress）。
- 代码：tunnel.PlanIngress、Manager 部署/校验/回滚支持；server/c2_ingress_tools.go（c2_ingress_deploy / c2_ingress_teardown）；C2 listener 强制 loopback；HTTP PowerShell/TCP Python 植入端回调地址由 ingress 生成。
- 验证：Windows 133 目标（HTTP PowerShell 植入端）经 VPS 公网回调到平台 loopback C2 listener；C2 会话 active（remote=127.0.0.1 中继），whoami/hostname 任务成功；整链路 20.6s PASS。发布：tag v0.4.7，Release 45037673828 成功；CI 45037375661 全绿。
- 环境说明：测试 VPS（113.20.3.124）云防火墙只放行 22/80/443，39444 入站不可达；测试临时停 1Panel openresty 使用 443 完成验证，测试后已恢复。生产需在 VPS 放行所选回调端口，或用 80/443 前置代理。

## 12. P1 设计（v0.4.8）

- 节点标签：tunnel.Plan/PlanOpts 增加 node_label，随 deploy_params 持久化，列表/UI 展示，用于多 VPS 归属。
- 列表 API：GET /api/c2/ingress，按 tunnels.kind=ingress 查询，联表读取 C2 listener 状态；返回 callback_addr、listener_id/status、tunnel state、node_label、last_check、healthy。
- 创建 API：POST /api/c2/ingress，复用 c2_ingress_deploy 的核心服务函数（自动创建 loopback listener、PlanIngress、部署、返回植入端）。
- 停止 API：POST /api/c2/ingress/{tunnel_id}/teardown，停隧道并停关联 listener。
- UI：C2 页新增“入站中继”卡片，支持创建（VPS 会话 id、VPS host/port、listener 类型、node_label、平台 chisel host/port）、列表与停止。
- 健康口径：tunnel state=alive 且 listener status=running 视为 healthy；decisive 探活仍由 tunnel 巡检承担，列表展示最近 last_check。
- 多 VPS：每个 ingress 独立 loopback listener + node_label；P2 再考虑节点级健康聚合/故障转移。

### P1 实施记录（v0.4.8）

- Plan/PlanOpts 增加 node_label；DB 增加 ListIngress。
- REST：GET/POST /api/c2/ingress、POST /api/c2/ingress/{id}/teardown；列表返回 callback/listener/state/healthy/last_check。
- UI：C2 管理页新增“入站中继”卡片（创建/列表/停止/复制植入端）。
- 校验：go build/vet、web npm run build 通过；P0 真机 E2E 保持通过（Windows 133 → VPS ingress → 平台 loopback C2）。发布：提交 2da92c0，CI 45040387113 全绿，tag v0.4.8，Release 45040620312 成功。
- 补充（2026-11-23）：用户放行 VPS 39444 后，Windows 133 → VPS:39444 → 平台 loopback C2 直连 E2E PASS（19.2s，whoami/hostname 成功），不再依赖 443 临时方案。

## 13. P2 设计（生产加固与多节点，待排期）

### 13.1 目标

把 C2 ingress 从“能用”做到可长期运行、可审计、可多节点管理；不改变 C2 直连模式，不内置 C2 socks5。

### 13.2 P2-A HTTPS/443 前置反代（暂不做）

> 2026-11-23 用户确认：可单独放行 39443/39444，不需要 443 前置反代；本章保留为备选设计，不进入本期实现。

问题：部分 VPS 云防火墙只允许 22/80/443，不能直接放行 39444。

要做的内容：

1. Ingress 支持本地绑定与公网基址分离：
   - 新增 vps_bind_host（默认 0.0.0.0；反代时填 127.0.0.1）。
   - 新增 public_base_url（如 https://ddv.008359.xyz）和 public_path_prefix（如 /c2）。
   - c2_ingress_deploy、REST、UI 增加对应字段。
   - HTTP 植入端生成改用 public_base_url，不再固定 http://vps:port。
   - TCP ingress 不走 HTTP 反代，仍要求直连端口。
2. openresty/nginx 站点反代模板：
   - 在 1Panel 站点 proxy 目录加 c2.conf，把 /c2/ 转发到 127.0.0.1:<vps_bind_port>。
   - 只放行 POST /c2/beacon/，限制 body 大小、超时、请求速率。
   - access log 不记录完整 token（只记录指纹）。
3. 证书与域名：复用现有站点证书；新子域名走 1Panel 证书申请；检查续期。
4. 健康与回滚：平台侧探测 public_base_url 路径；配置前备份、nginx -t、reload，失败回滚。
5. 验收：Windows 133 用 HTTPS 植入端经 443 反代上线；39444 直连回归不退步；错误路径/token 被拒。

### 13.3 P2-B 访问控制与安全加固（暂不做）

> 2026-11-23 用户确认：回连 VPS 与 VPS 到平台的数据交互已加密（TCP 信封 / chisel TLS 控制链路），本期不做额外 ACL/限速；保留为后续可选加固。

1. 源 IP ACL：openresty allow/deny 目标出口 IP，或按任务动态白名单。
2. Token 防重放/限速：每 IP 请求速率、失败次数限制，配合 fail2ban。
3. 请求约束：只允许 POST beacon 路径、Content-Type application/json、body 上限。
4. TLS：强制 HTTPS、HSTS、隐藏 Server 版本。
5. 日志脱敏：access log 不记录完整 token，只记录指纹。
6. 凭据管理：listener token 不出现在 UI 明文/日志；植入端下载加短时一次性授权。
7. 可选 WAF：对 C2 beacon 路径精确放行，避免误拦。

### 13.4 P2-C 多 VPS 节点健康与故障处理（已实施 v0.4.9）

1. 节点台账：建议新增 c2_ingress 表（tunnel_id/listener_id/vps_host/vps_port/node_label/last_health_at/status/last_error），比只靠 tunnels.deploy_params 更易查询。
2. 健康巡检：定时探测 VPS 回调端口（TCP connect / HTTP HEAD）；校验 listener 状态、tunnel 状态、chisel 进程；写入 health 并产生事件。
3. 生命周期：节点下线/重启后的重建策略；停止顺序幂等；节点失联保留记录，不误删 C2 会话。
4. 多节点语义：目标绑定固定节点，不自动切任意节点；故障转移需重新生成植入端或切换入口，P2 只做手动重建/迁移。
5. UI/API：节点列表、健康状态、最近错误、事件时间线；创建时选节点/会话；批量停止需谨慎。

### 13.5 P2-D 可观测性与告警（已实施 v0.4.9，外发告警待接 notifier）

1. 指标：ingress_total、ingress_alive、ingress_healthy、beacon_requests、beacon_failures（relay_bytes 可选）。
2. 事件：创建/启动/停止/健康失败/重建。
3. 告警：节点失联、listener 停止、chisel 进程退出、连续 beacon 失败。
4. 审计：谁在何时创建/停止 ingress、使用哪个会话/节点/植入端语言。
5. 日志：平台只存元数据，不存 token/命令内容；VPS access log 脱敏。

### 13.6 P2-E UI/文档/验收（已实施 v0.4.9）

1. UI：会话选择器、节点标签、协议/端口、TLS/路径前缀、健康筛选、事件面板。
2. 文档：1Panel/openresty 反代模板、证书申请、防火墙规则、排障（端口/云安全组/代理节点）。
3. 验收脚本：Windows/Linux 目标、HTTP/TCP、443 反代/直连、失败回退。
4. 发布：独立小版本，保持 P0/P1 回归。

### 13.7 优先级与依赖（2026-11-23 调整）

P2-A/P2-B 暂不做。当前优先执行 P2-C（多 VPS 健康台账/巡检/手动重建），再做 P2-D/E。

1. P2-A HTTPS/443 反代（解决端口限制）。
2. P2-B ACL/限速（可与 A 同批）。
3. P2-C 健康台账 + 多节点（依赖 public_base_url 字段）。
4. P2-D 可观测性/告警。
5. P2-E UI/文档完善。

### 13.8 测试矩阵

- 直连：Windows 133 → VPS:39444 → 平台 loopback C2（已有 PASS）。
- 反代：Windows 133 → HTTPS 443/目标路径 → 127.0.0.1:39444 → 平台 loopback C2。
- 失败：错误 token、错误路径、源 IP 不在白名单、VPS 端口未放行。
- 稳定性：8h 心跳、断线重连、VPS chisel 重启、listener 重启。
- 回归：C2 直连 HTTP/TCP、C2→隧道、socks/portfwd。

### 13.9 P2 退出标准

- HTTPS 443 反代链路真机 PASS，且不依赖停用 openresty。
- ingress 本地只绑 127.0.0.1，公网只暴露 443 路径。
- 错误请求/未授权源 IP 被拒，日志无 token 明文。
- 节点健康可查询、失联可告警；停止/重建幂等。

### P2-E 实施记录（v0.4.9）

- UI：入站中继 VPS 会话改为下拉选择（intranet sessions）；新增健康/节点筛选与总数/健康数汇总；保留最近事件时间线。
- 文档：新增 docs/c2-ingress-runbook.md（拓扑、前置条件、1Panel/云防火墙、部署、健康、排障、清理、安全边界、验收）。
- 验收：新增 scripts/acceptance/c2_ingress_direct.sh；acceptance README 补页面验收项。
- 无新增后端接口/迁移。提交 b8f7cda；web build 通过。
- 文档/验收/版本记录齐全。

### P2-C 实施记录（v0.4.9）

- 迁移 0014：新增 c2_ingress 表（tunnel_id 唯一、listener/task、vps_host/port、node_label、status/healthy/last_health_at/last_error、stopped_at）。
- 服务：部署成功写台账；停止时标记 stopped；列表以 c2_ingress 为主，兼容旧 tunnels 记录；新增 POST /api/c2/ingress/{id}/rebuild 手动重建。
- 巡检：服务启动后每 30s 检查 tunnel 状态、C2 listener 状态、VPS 回调端口 TCP 可达性，写回 health/status/last_error；兼容旧 ingress 自动补台账。
- UI：C2 页面入站中继表增加最近健康时间与“重建”按钮。
- 说明：A/B 暂不做（用户可单独放行端口，C2/chisel 链路已加密）。提交 f9903e7。

### P2-D 实施记录（v0.4.9）

- 指标（/metrics）：artex_c2_ingress_total / healthy / unhealthy、artex_c2_beacon_requests_total / failures_total。
- 事件：c2_events 写创建/停止/健康状态变化；GET /api/c2/ingress/events?limit=N；C2 页面入站中继卡展示最近事件。
- 审计沿用 c2_events；未把 token/命令写入事件。
- 外发告警（钉钉/飞书/Webhook）暂未接入 notifier，先以事件 + 指标供外部告警规则消费。
- 提交 1cc778b；本地 go build/vet、fresh DB go test ./...、web build、/metrics 冒烟通过；GitHub Actions 因账单禁用，改推 Gitea wantong/ARTEX。
