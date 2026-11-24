# 住宅网关代理模式（Gateway / SID）设计

## 背景

二开版本支持住宅 IP 服务商网关：一条网关 URL 通过用户名里的 region/sid 段生成多条住宅出口，支持 SID 轮换换 IP。主线当前只有订阅和静态 http/socks5 条目。

## 目标

- 新增 gateway 来源类型；
- 支持 socks5/http/https 网关 URL；
- 识别 dynamic 模板并生成多条 sid 节点；
- 支持手动/自动选择、延迟探测、SID 轮换；
- 与现有 fail-closed、控制面、守护、封禁轮换兼容。

## 非目标

- 不做服务商账号注册/续费；
- 不做按国家/城市的地理路由编排；
- 不做多网关账号池；
- 不保证所有住宅服务商用户名模板完全兼容。

## 总体方案

### 网关规格

输入：

    socks5://user-region-XX-sid-rand@host:port
    http://user:pass@host:port

解析为：

    scheme / host / port / user / pass / base_user
    dynamic / region

dynamic 判定：

- 用户名包含 region、sid、rand、sess、time、area 等会话段；
- 用户名可重建为 base + sid-<random>。

非 dynamic 网关只生成 1 条固定节点。

### 存储

egress_sources.kind 增加：

    gateway

字段：

- config_enc 加密保存网关 URL；
- last_good_config_enc 保存生成的 Clash YAML；
- selected_node / selection_mode / auto_rotate 复用；
- gateway_count 通过 API 参数控制，默认 5，上限 20。

### 节点生成

生成 mihomo proxy：

    name: gw-<region>-<sid>
    type: socks5 / http
    server / port
    username / password

节点集合写入 Clash YAML proxies，交给现有 buildMihomoConfig 生成：

    ARTEX-AUTO = load-balance consistent-hashing
    ARTEX-SELECT = select
    MATCH,ARTEX-SELECT

### 生命周期

- POST /api/proxy-pool/gateway 保存并启动；
- 启动后注册本地 mixed 口到 proxy_pool；
- 服务重启按 desired_state=active 恢复；
- stop 停止并删除条目；
- rotate 生成新 sid 集合并热切换/重启 mihomo；
- 封禁自动轮换复用现有节点轮换逻辑；
- 守护复用现有 30s mihomo 守护。

### API

    POST /api/proxy-pool/gateway
    body: { url, count }
    resp: { count, dynamic, region, started }

    POST /api/proxy-pool/gateway/rotate
    resp: { count, first_node }

    GET /api/proxy-pool/subscription
    增加 source_kind=gateway, gateway_count, gateway_region, gateway_dynamic

### UI

代理池页新增“住宅网关”卡片：

- 网关 URL 输入（密码掩码）；
- 节点数量；
- 保存并启动 / 轮换 SID / 停止；
- 状态显示 dynamic/region/节点数；
- 节点展开复用代理池行内面板。

### 安全

- 网关 URL 加密落库；
- UI 不回显完整密码；
- 日志脱敏 user/pass；
- 只允许 socks5/http/https；
- 不做任意 URL 代理（避免 SSRF）；网关仅用于 mihomo 配置。

## 兼容

- subscription/links 不受影响；
- gateway source 与 subscription 互斥，同一时间只有一个 active source；
- 旧代理池静态条目保留；
- control plane 只认 active source。

## 测试

- 网关 URL 规范化：默认端口、认证、dynamic 检测；
- SID 重建与去重；
- 生成节点数正确；
- mihomo 启动、选择、延迟；
- rotate 后出口变化；
- 服务重启恢复；
- 密码不落日志。

## 实施阶段

1. 网关解析/节点生成 + 单测；
2. gateway source 存储与 loader；
3. API；
4. UI；
5. rotate/守护/封禁 E2E。

## 风险

- 不同服务商用户名模板差异大：先支持通用前缀/后缀规则，遇到不支持模板明确报错；
- 频繁 rotate 可能触发服务商风控：增加最小间隔和手动确认；
- 住宅出口延迟高：自动组仍按 load-balance，不做延迟最优保证。
