# Clash 订阅导入代理池

## 背景

代理池“从 mihomo / Clash 配置导入”目前页面只有 YAML 文本框；后端 /api/proxy-pool/import-mihomo 已支持 url 字段，但 UI 没有入口，用户无法直接粘贴 Clash 订阅地址。

## 目标

- 代理池页面增加“Clash 订阅地址”输入与“拉取订阅并导入”按钮；
- 复用现有 import-mihomo API，不新增存储结构；
- 保留 YAML 文本导入；
- 导入结果继续显示新增/重复/跳过（仅 http/https/socks5 可导入）；
- URL 只允许 http/https，限制响应大小与超时，错误明确返回。

## 非目标

- 不做订阅定时刷新/自动更新（后续可加 proxy provider）；
- 不导入 vmess/trojan/ss 等需要 mihomo 内核转发的节点；
- 不改变代理池加密/去重/活动代理逻辑。

## 总体方案

- 前端 components/proxy-pool-panel.tsx 增加 subscriptionURL state 与输入框/按钮；
- 按钮调用 api.proxyPoolImportMihomo({ url, name_prefix })；
- 后端 proxyPoolImportMihomo 增加 URL scheme/host 校验，保持 4MB body 限制和 20s HTTP timeout；
- 结果复用现有 imported_count/duplicates/skipped/errors 渲染。

## 接口

POST /api/proxy-pool/import-mihomo
- yaml: string（与 url 二选一）
- url: string（http/https 订阅地址）
- name_prefix: string

响应不变：ok/imported_count/duplicates/skipped/errors。

## 安全

- 只允许 http/https，拒绝非法 URL；
- 订阅拉取走代理池 client（可走活动代理/兜底/直连）；
- 响应体最多 2MB，HTTP client 20s 超时；
- 不记录订阅 URL 中的凭据；错误返回只含状态码/网络原因。

## 兼容性

- API 字段不变；旧 YAML 导入不受影响；
- 订阅 URL 仅本次导入，不落库。

## 测试

- web build；
- 后端 URL 校验单测（可选）；
- 用 http/https 订阅测试导入结果；
- 确认 YAML 导入回归。

## 实施记录（v0.4.9）

- 代理池面板新增 Clash 订阅 URL 输入 + “拉取订阅并导入”；复用 import-mihomo API；
- 后端新增 validateSubscriptionURL（仅 http/https），保留 YAML 导入；
- 订阅不落库，仅本次拉取；结果复用新增/重复/跳过渲染；
- go build、validateSubscriptionURL 单测、web build 通过。
