# C2 入站中继运行手册（P2-C/P2-D/P2-E）

## 1. 适用场景

目标机器只能出网、不能直接访问 ARTEX 平台时，用一台 VPS 作为回调入口：

~~~text
目标植入端
  -> VPS 公网回调端口（如 39444）
  -> VPS 侧 chisel client 本地转发
  -> 平台 chisel server（如 39.89.211.55:39443）
  -> 平台 127.0.0.1:<C2 listener>
~~~

平台侧 C2 listener 只绑 127.0.0.1；目标只拿到 VPS 回调地址，不暴露平台地址。

## 2. 前置条件

1. 平台 chisel 端口已映射到公网（默认 39443），VPS 能访问；
2. VPS 回调端口已放行（默认 39444）：
   - 1Panel 主机防火墙：允许 TCP 39444；
   - 云厂商安全组/网络 ACL：同样允许 TCP 39444；
   - 规则注意掩码，0.0.0.0/0 或目标出口 IP；
3. VPS 到平台的 chisel 服务可达，平台 C2 已启用；
4. 平台已有到 VPS 的会话（intranet sessions 表中的 session id）；
5. 目标出网可访问 VPS 回调端口。

## 3. 部署

### 3.1 UI

C2 管理 -> 入站中继：

1. 选择 VPS 会话（或手填 session id）；
2. 填写 VPS 公网 host、回调端口（如 39444）；
3. 选择 listener 类型：HTTP beacon / TCP；
4. 可选节点标签（多 VPS 归属）；
5. 填写平台 chisel host/port；
6. 选择植入端语言，创建；
7. 复制生成的植入端脚本到目标执行。

### 3.2 Agent 工具

~~~text
c2_ingress_deploy(
  session_id=<VPS 会话 id>,
  vps_host=<VPS 公网 IP>,
  vps_port=39444,
  c2_listener_kind=http,
  node_label=vps-hk-1,
  platform_host=<平台公网 IP>,
  platform_chisel_port=39443,
  implant_lang=powershell
)
~~~

返回 tunnel_id、callback_addr、implant。

### 3.3 REST

~~~text
POST /api/c2/ingress
GET  /api/c2/ingress
POST /api/c2/ingress/{tunnel_id}/teardown
POST /api/c2/ingress/{tunnel_id}/rebuild
GET  /api/c2/ingress/events?limit=20
~~~

## 4. 健康与重建

- 服务启动后每 30s 巡检：tunnel 状态、C2 listener 状态、VPS 回调端口 TCP 可达性；
- 结果写入 c2_ingress 台账（status/healthy/last_health_at/last_error）；
- 页面显示健康、最近健康时间、最近事件；
- 指标：/metrics 的 artex_c2_ingress_* 与 artex_c2_beacon_*；
- 节点异常时可点“重建”，按原会话/节点重新部署；
- 停止：停隧道 + 停关联 loopback listener，台账标记 stopped。

## 5. 排障

| 现象 | 优先检查 |
|---|---|
| 目标连不上 VPS:39444 | 1Panel 防火墙、云安全组、规则掩码、运营商/代理节点 |
| VPS 端口可连但无 C2 会话 | chisel 是否连上平台、平台 39443 映射、tunnel 状态 |
| listener 启动失败 | 端口占用、bind 必须是 127.0.0.1、C2 是否启用 |
| 页面健康为异常 | 看 last_error：tunnel/listener/callback 哪一项 |
| 页面无数据 | C2 页刷新；GET /api/c2/ingress；旧 ingress 可能显示 unknown |
| 页面无法打开（1Panel） | 8443/代理节点/证书；与本功能无关 |

## 6. 清理

1. 页面停止中继；
2. 确认 VPS 无 chisel/监听残留；
3. 目标侧删除植入端脚本/进程；
4. 平台侧确认 listener stopped、c2_ingress 记录 stopped；
5. 回收 VPS 防火墙临时规则。

## 7. 安全边界

- C2 listener 只绑 127.0.0.1，VPS 只暴露回调端口；
- 植入端 token 内嵌脚本，妥善保管；不要提交到仓库；
- 当前未内置额外 ACL/限速（P2-B 按用户确认暂不做）；如需要请在 VPS 侧加 allowlist/限速；
- 443 前置反代（P2-A）未实现；VPS 必须能放行回调端口；
- 日志/事件不写 token 或命令内容。

## 8. 验收

~~~bash
# 真机 Windows E2E（环境变量见 scripts/acceptance/README.md）
scripts/acceptance/c2_ingress_direct.sh
~~~

页面验收：

- 创建/列表/停止/重建可用；
- 健康/最近健康/最近事件显示；
- 断掉 VPS 回调端口后，30s 内健康变异常并产生 warn 事件；
- 恢复端口后可重建/恢复。
