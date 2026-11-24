# 代理池作战能力补齐（二开对齐 P1）

## 背景

二开版本具备封禁自动换 IP、节点耗尽保护、mihomo 守护、Agent 控制工具和 load-balance 自动组；主线当前只有手动/自动节点选择与延迟探测。

## 目标（本轮）

- 封禁信号触发自动换节点：403/429 立即切，连续失败可切；
- 5 分钟窗口节点耗尽保护，暂停自动换 IP 并记录错误；
- mihomo 30s 守护，崩溃退避重启；
- 自动组改为 load-balance + consistent-hashing，减少出口 IP 漂移；
- 提供 Agent 工具 proxy_pool_control（status/auto_on/auto_off/rotate/pin/unpin）；
- UI 增加“封禁自动换 IP”状态/开关。

## 非目标（后续）

- 分享链接解析 vmess/ss/trojan/vless（本轮不做）；
- 住宅网关 gateway/SID 模式（后续独立设计）；
- 全量协议兼容与 Clash 客户端级规则。

## 数据模型

egress_sources 增加：

- auto_rotate BOOLEAN NOT NULL DEFAULT TRUE

migration 0017。运行时只在内存维护 ban_window/ban_strikes，不落库。

## 行为

- selection_mode=auto + auto_rotate=true：ARTEX-SELECT 指向 ARTEX-AUTO；
- manual 选择：auto_rotate=false，固定节点；
- 403/429：若 auto_rotate，切到下一个节点（排除当前），更新 selected_node，保留 auto_rotate；
- 5 分钟内切换次数 >= 节点数：auto_rotate=false，last_error=节点池疑似耗尽；
- 守护：30s 检查 mihomo 进程；退出后按 5/15/30/60s 退避重启，连续失败 5 次停止；
- 自动组：load-balance + consistent-hashing（同一目标稳定出口）。

## API

- POST /api/proxy-pool/subscription/auto-rotate {enabled}
- GET status 增加 auto_rotate、ban_strikes、ban_window_seconds
- Agent 工具 proxy_pool_control

## 安全

- 自动换 IP 只在 subscription 运行且 auto_rotate 时生效；
- 手动 pin 时禁止自动切换；
- 耗尽后 fail-closed 逻辑不变；
- 守护重启不重新下载订阅，成功启动后按 last-good。

## 测试

- 单测：节点耗尽计数、rotation 选择、auto_rotate 开关；
- E2E：模拟 403/429 调用内部 hook，状态 selected 变化、耗尽后停止；
- mihomo 进程 kill 后 30s 内恢复；
- web build。

## 风险

- 403 可能是业务鉴权而非出口封禁，自动切换可能误判；
- 固定窗口轮换策略依赖节点数；
- 守护重启与手动 stop 竞态需用 proxySubMu 串行。

## 实施记录（v0.4.9）

- 0017 migration：egress_sources.auto_rotate。
- ARTEX-AUTO 改为 load-balance + consistent-hashing；ARTEX-SELECT 仍可手动 pin。
- 403/429 通过 traffic MITM Response hook 进入 noteProxyPoolBan，自动切下一个节点并累计 ban_strikes；5 分钟窗口达到节点数后 auto_rotate=false + last_error。
- mihomo 30s 守护：进程/端口失活后按 5/15/30/60s 退避重启，连续 5 次停止。
- API：POST /api/proxy-pool/subscription/auto-rotate；status 增加 auto_rotate/ban_strikes/current_node。
- Agent 工具 proxy_pool_control 已 seed 到 tools 表，默认绑定 worker/mainagent/planner/auto/pentest。
- UI 节点行增加“封禁自动换 IP”开关。

E2E：

- MITM 访问 httpbin 403 → ban_strikes=1，current_node 切换；
- kill mihomo 子进程 → 守护在下一轮恢复运行；
- auto-rotate 关闭/开启 API 200；
- tools 表存在 proxy_pool_control 行。

未完成：

- 分享链接 vmess/ss/trojan/vless 解析；
- 住宅 gateway/SID 模式；
- 403 也可能是业务鉴权，当前按封禁处理，后续可加域名/状态白名单。
