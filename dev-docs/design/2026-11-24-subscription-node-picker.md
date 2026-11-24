# 出口订阅节点选择 / 延迟探测 / 接管开关

## 背景

当前 Clash 订阅代理只把 mihomo 当成单一本地 mixed 口，节点列表不可见；用户无法像 Clash 客户端一样查看节点、手动选择或测延迟。出口模式还需要跳到系统配置切换，步骤多。

## 目标

- 订阅卡增加展开箭头，展示从 last-good Clash 配置解析出的节点；
- 节点显示类型、名称、延迟；
- 支持手动选择节点、自动模式；
- 节点支持延迟探测；
- 代理池页右上角增加接管出口开关：开=egress_mode=pool，关=direct；
- 保持现有 API/页面兼容，不新增独立页面。

## 非目标

- 不做完整 Clash 客户端 UI（分组、规则、订阅刷新历史）；
- 不做按任务/目标节点绑定；
- 不做节点排序/搜索/收藏；
- 不自动下载 mihomo。

## 总体方案

### 节点来源

- 订阅成功时把 Clash YAML 存 last-good；
- GET /api/proxy-pool/subscription/nodes 从 last-good 解析 proxies，返回 name/type/server/port；
- 不单独建节点表，避免与 mihomo 配置双写。

### mihomo 控制面

- 启动时在原 sanitize 后生成：
  - proxy-groups: ARTEX-AUTO (url-test) + ARTEX-SELECT (select)
  - rules: MATCH,ARTEX-SELECT
  - external-controller: 127.0.0.1:<随机端口>
  - secret: <随机值>
- 外部控制器只监听 loopback，secret 只内存保存；
- 默认选择：
  - source.selection_mode=auto -> ARTEX-AUTO；
  - manual -> source.selected_node。
- 选择节点：PUT /proxies/ARTEX-SELECT {name}；
- 延迟：GET /proxies/<name>/delay?url=...&timeout=3000。

### 数据模型

egress_sources 增加：

- selected_node TEXT DEFAULT ''
- selection_mode TEXT DEFAULT 'auto' CHECK ('auto','manual')

migration 0016 兼容已有行。

### API

- GET /api/proxy-pool/subscription/nodes
- POST /api/proxy-pool/subscription/select {mode: auto|manual, name?}
- POST /api/proxy-pool/subscription/probe {name}

### UI

- 订阅卡标题右侧加展开箭头；
- 展开显示：
  - 模式按钮：自动 / 手动
  - 节点列表卡片：名称、类型、延迟、使用按钮、测延迟
- 页面右上角加接管出口开关：
  - on -> settings.egress_mode=pool
  - off -> settings.egress_mode=direct
  - 显示当前 mode，避免用户误以为订阅自动接管。

### 生效逻辑确认

- egress_mode=direct：target 直连，订阅/pool 不生效；
- egress_mode=fixed：固定 global_proxy；
- egress_mode=pool：使用 proxy_pool active（含订阅本地 mixed 口）。
- 开关只改 egress_mode，不改订阅状态。

## 安全

- controller 只绑 127.0.0.1；
- secret 随机、仅内存；
- 节点名称来自订阅，按不可信文本渲染（React 转义）；
- 延迟探测 URL 固定。

## 兼容

- 旧配置没有 selected_node/selection_mode 时默认 auto；
- 旧 mihomo 配置重启后按 auto 选择；
- API 失败不影响现有出口。

## 测试

- 节点解析；
- 手动选择后 mihomo 组切换；
- 自动模式；
- 延迟探测；
- 接管开关 direct/pool；
- web build/E2E。

## 风险

- mihomo 版本对 external-controller/delay API 差异；
- 节点名特殊字符需 URL 编码；
- 控制器端口冲突。

## 实施记录（v0.4.9）

- 0016 migration：egress_sources 增加 selected_node/selection_mode。
- mihomo 配置生成 ARTEX-SELECT + ARTEX-AUTO，MATCH ARTEX-SELECT；external-controller 仅 127.0.0.1 + 随机 secret。
- API：GET nodes、POST select、POST probe；节点名 URL 编码后调用 mihomo delay/group。
- 前端：订阅卡节点展开、自动/手动、测延迟、使用；页面右上角“接管出口”开关切 egress_mode pool/direct。
- 实测：节点 43 个；手动选择成功；延迟探测返回 117ms；自动/手动模式切换成功。
- web build + go build 通过。
