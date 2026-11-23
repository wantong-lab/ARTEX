# Clash 订阅代理（mihomo 内核）

## 背景

很多 Clash 订阅链接返回的是 vless/hysteria2 等节点，不能直接作为 HTTP/SOCKS5 代理 URL 加入代理池。当前“订阅 URL 导入”只能解析 Clash YAML 且仅收 http/socks5，用户的实际订阅测试失败。

二开版本用 mihomo 内核把订阅转换成本地 mixed 端口。本设计在主线补同样能力。

## 目标

- 出口代理池支持“Clash 订阅 → 本地 mihomo → 127.0.0.1:mixed-port”；
- 自动识别 base64 订阅并请求 Clash.Meta 格式（flag=meta/clash）；
- 支持启动/停止本地 mihomo，启动后自动登记一个本地 HTTP 代理条目并设为活动代理；
- mihomo 二进制不存在时给明确安装提示；
- 不把订阅 URL（含 token）落库/写日志。

## 非目标

- 不自动下载 mihomo 二进制（由部署方放到 data/tools/mihomo 或 PATH）；
- 不做订阅定时刷新（后续可加）；
- 不改变代理池对远程 http/socks5 条目的现有能力；
- 不自动切换 egress_mode（用户仍在系统配置选择 fixed/pool）。

## 总体方案

### 二进制查找

按顺序：
1. ARTEX_MIHOMO_PATH；
2. data/tools/mihomo/mihomo-<os>-<arch>[.exe]；
3. 可执行文件目录下 data/tools/mihomo/...；
4. PATH 中 mihomo / verge-mihomo / clash-meta；
5. macOS Clash Verge 自带 verge-mihomo。

### 订阅拉取

- GET 原 URL，30s 超时，最大 4MB；
- 若响应含 proxies: 直接使用；
- 否则按 base64/非 YAML 处理，依次尝试给 URL 追加 flag=meta、flag=clash、target=clash，取第一个含 proxies: 的响应；
- 仍失败则返回检测到的协议/格式说明。

### mihomo 启动

- 用 yaml.v3 读取 Clash 配置；
- 覆盖 mixed-port=空闲端口、allow-lan=false、mode=rule、log-level=warning；
- 写 data/proxy-sub/config.yaml (0600)，启动 mihomo -d <dir> -f <config>；
- 等待本地端口可连接；
- 通过 ProxyStore 创建 http://127.0.0.1:<port> 条目（名称 clash-sub），设为活动代理；
- 同步 proxy pool egress；不自动改 egress_mode。

### 停止

- kill mihomo 进程；
- 删除运行时创建的本地代理条目；
- 若它是活动代理，清空 active_id；
- 同步 egress。

## 接口

- GET /api/proxy-pool/subscription → 状态/二进制/本地代理/错误
- POST /api/proxy-pool/subscription {url} → 启动
- DELETE /api/proxy-pool/subscription → 停止

## 安全

- 订阅 URL 只内存保存，不写日志/设置；
- 配置含节点凭据，文件 0600，目录 0700；
- 只启动本地回环 mixed 端口，allow-lan=false；
- 不自动下载/执行任意 URL 提供的二进制。

## 兼容性

- 原有 import-mihomo 保留，用于直接导入 http/socks5 节点/YAML；
- 本地代理条目是普通代理池条目，现有切换/兜底/探测逻辑可用；
- 重启后 mihomo 不会自动恢复（首版），页面会显示未运行；后续可加开机自启。

## 测试

- 手工验证：用户订阅 flag=meta → mihomo → 本地 mixed port → curl 出口 IP 200；
- Go 单测：订阅格式探测/URL 变体构造；
- web build；
- 启停后本地条目与活动代理清理。

## 风险

- mihomo 版本差异（配置字段/协议支持）；
- 订阅链接 token 泄漏（不落库/不日志）；
- 进程孤儿（首版仅显式停止/服务退出不保证回收，文档标注）。

## 实施记录（v0.4.9）

- 后端新增 server/proxy_subscription.go：mihomo 二进制发现、订阅拉取/自动尝试 flag=meta、YAML 配置生成、进程启停、本地代理条目登记与活动代理设置。
- API：GET/POST/DELETE /api/proxy-pool/subscription。
- 前端代理池页新增“Clash 订阅代理（mihomo 内核）”卡片，支持启动/停止与状态显示。
- 实测用户订阅：原始链接为 base64 vless/hysteria2，自动请求 flag=meta 后启动 mihomo；本地 mixed 口 curl 出口 IP 200；egress_mode=pool 时 effective_proxy 指向本地口；停止后进程/条目/配置清理。
- mihomo 二进制：本机使用 Clash Verge 自带 verge-mihomo；其他环境设置 ARTEX_MIHOMO_PATH 或放 data/tools/mihomo。
