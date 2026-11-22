# 设计稿：C2 入站回调中继（C2 Ingress Relay）

- 日期：2026-11-22
- 状态：已实施（P0，v0.4.7）
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

