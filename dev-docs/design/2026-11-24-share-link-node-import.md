# 分享链接节点导入（vmess/ss/trojan/vless）设计

## 背景

当前主线的 Clash 订阅只接受 mihomo/Clash YAML，手工代理只接受 http/https/socks5 URL。二开版本支持直接粘贴 vmess/ss/trojan/vless 分享链接和 base64 订阅内容。


## 模块归属

- 本功能属于现有“出口 IP 代理池”模块，不新增顶级页面或侧边栏入口；
- 前端统一放在 /function/proxy-pool，与 Clash 订阅、手工代理、节点选择共用同一页面；
- 后端复用 egress_sources、proxy_subscription supervisor、proxy pool status/control；
- 同一时间只允许一个 active 来源（subscription / links / gateway 三选一），避免多个 mihomo 入口和出口歧义。

## 目标

- 支持粘贴单条或多条分享链接；
- 支持整段 base64 订阅文本自动解码；
- 解析为 mihomo proxy map，复用现有订阅 mihomo 进程；
- 导入后节点可被节点选择、自动/手动、延迟探测复用；
- 支持启动、停止、服务重启恢复。

## 非目标

- 不做完整 Clash 规则/策略组兼容；
- 不支持 vmess 之外的私有协议；
- 不做浏览器分享链接/二维码导入；
- 不做远程 URL 抓取以外的来源。

## 总体方案

### 解析层

新增独立包：

    proxypool/uri

提供：

    ParseShareLinks(raw string) ([]map[string]any, []ParseIssue, error)

支持：

- vmess://base64 JSON
- vmess://uuid@host:port（较少见）
- ss://base64(method:pass@host:port)
- ss://base64(method:pass)@host:port
- trojan://password@host:port?...
- vless://uuid@host:port?...
- 整段 base64 文本自动解码
- 行注释与机场信息节点过滤

输出统一为 mihomo proxy map，至少包含：

    name / type / server / port
    uuid 或 password / cipher
    tls / servername / skip-cert-verify
    network / ws-opts / grpc-opts / reality-opts
    flow / client-fingerprint

解析失败按条记录，不因单条失败中断整批。

### 存储

egress_sources 增加一种 kind：

    links

特性：

- config_enc 加密保存原始分享链接文本；
- last_good_config_enc 保存解析后的 Clash YAML；
- 与 subscription kind 共用唯一索引；
- 不额外建节点表，避免与 mihomo 配置双写。

### 启动链路

当前订阅启动是：

    URL -> fetch -> validate -> buildMihomoConfig -> mihomo

新增来源加载抽象：

    loadSourceConfig(ctx, src) (rawClashYAML string, fetchedAt time.Time, err error)

- subscription：HTTP 拉取 + meta 格式探测；
- links：读取 last-good YAML，不联网。

startProxySubscription 改为按 source kind 选择 loader；节点列表、选择、探测、刷新、守护、封禁轮换全部复用现有逻辑。

### API

    POST /api/proxy-pool/nodes/import
    body: { links: string }
    resp: { count, skipped, errors, started }

    GET /api/proxy-pool/subscription
    增加 source_kind=links

    GET /api/proxy-pool/subscription/nodes
    复用

    DELETE /api/proxy-pool/subscription
    停止并禁用

### UI

代理池页“高级：直接导入”区域：

- 保持 YAML/节点文本输入；
- 新增“导入为订阅节点”按钮；
- 导入成功后替换/新增 links source，并自动启动；
- 节点展开仍从代理池 clash-sub 行进入。

原“直接导入 http/socks5 到代理池条目”保留，二者语义不同：

- 分享链接导入 -> mihomo 节点源；
- 直接导入 -> 代理池静态条目。

## 安全

- 限制输入 8MB；
- 不执行节点内容；
- 不记录 uuid/password 明文到日志；
- 输出渲染按不可信文本处理；
- mihomo external-controller 继续 loopback + secret；
- 导入不触发任意文件读取。

## 兼容

- 旧 subscription source 不受影响；
- 无 links source 时行为不变；
- 解析协议按 mihomo 支持版本校验，不支持的字段丢弃并记录 warning；
- Windows/Linux 复用同一解析层。

## 测试

- 每种协议至少 2 个真实分享链接样本；
- base64 整段文本；
- 混合有效/无效行；
- 重复导入去重；
- 导入后启动 mihomo、节点选择、延迟探测；
- 服务重启恢复 links source；
- web build。

## 实施阶段

1. 解析包 + 单测；
2. links source 存储/迁移；
3. source loader 抽象 + 启动链路；
4. API/UI；
5. E2E 与文档。

## 风险

- 分享链接字段不完整导致 mihomo 拒绝配置：导入时按协议做最小字段校验；
- 机场信息节点混入：沿用名称过滤，但不静默丢弃真实节点；
- 节点量过大：限制 500 条/8MB。

## 实施记录（v0.4.9）

- proxypool/uri.go 实现 vmess/ss/trojan/vless 分享链接与 base64 整段解析；
- 0018 迁移扩展 egress_sources.kind 支持 links/gateway；
- 新增 UpsertSource/GetSubscription 泛化、startProxySubscriptionLoaded 来源加载抽象；
- POST /api/proxy-pool/nodes/import；
- 代理池页新增分享链接输入卡；
- E2E：导入 vless+ss 两条链接，mihomo 启动、source_kind=links、节点数 2。
