# 统一出口代理（direct / fixed / pool）

## 背景

当前主线有两个相近但割裂的出站代理配置：

- 系统配置 global_proxy：Agent 目标流量 / MITM / 更新；
- 内网中枢代理池：活动代理 + 兜底，但只注入 Sploitus/CVE 的 HTTP client；
- 代理池页“全局兜底代理” proxy_global_url 又是第三份配置。

二开版本的产品语义是“全局代理”和“出口 IP 代理池”二选一。本设计把主线收敛为单一出口模式，避免“配了但不知道谁生效”。

## 目标

1. 用一个 egress_mode 明确当前出口：直连 / 固定代理 / 代理池；
2. 代理池接管时，Agent、MITM、更新、情报请求走同一条出口；
3. 保留旧配置兼容，不破坏已有 global_proxy / 代理池条目；
4. UI 明确显示当前生效出口，二选一不再靠猜。

## 非目标

- 不把代理池变成 mihomo 本地进程（主线仍是 HTTP/SOCKS5 条目池）；
- 不做按任务代理（仍由 flow/隧道机制处理）；
- 不做代理订阅格式大改。

## 数据模型

settings：

- egress_mode：direct | fixed | pool；空值按旧逻辑推断（global_proxy 非空 => fixed，否则 direct）。
- global_proxy：fixed 模式的固定代理，保留原 key。
- proxy_active_id：代理池当前活动代理。
- proxy_global_url：代理池兜底代理（pool 模式第二优先级），保留原 key。

## 出口解析

~~~text
direct:  ""
fixed:   global_proxy
pool:    active proxy URL
           -> proxy_global_url
           -> ""
~~~

- 关闭流量捕获：Agent bash/WebFetch 注入 effective proxy；
- 开启流量捕获：effective 作为 MITM 上游，Agent 仍走 MITM；
- 更新/自更新使用 effective proxy；
- 代理池活动切换/兜底变化时，服务端同步 effective 并刷新 MITM/任务代理/Agent。

## 接口

- GET /api/settings 增加 egress_mode、effective_proxy、pool_active_proxy。
- PUT /api/settings 接受 egress_mode；切换模式触发 Agent 重建。
- 代理池 switch/fallback/global/delete 后调用统一 egress 同步。
- 代理池页增加“当前出口状态”提示；系统配置页做模式选择，fixed 模式下才允许编辑固定代理。

## 生命周期

1. 启动：加载 egress_mode；初始化代理池；把 active/fallback 同步给 Manager；
2. 设置变更：持久化 -> ApplyEgress（MITM 上游/任务代理/浏览器/会话） -> 重建 Agent；
3. 代理池切换：SetPoolEgress(active, fallback) -> ApplyEgress -> 重建 Agent；
4. 池不可用：回落 proxy_global_url，再回落直连，不静默写回固定代理。

## 兼容性

- 未写 egress_mode 的旧库：global_proxy 非空视为 fixed；
- API 仍保留 global_proxy 字段；
- 旧代理池页的兜底配置继续有效，语义变为 pool 的第二级。

## 安全

- 代理 URL 继续校验 scheme/host，拒绝非法 scheme；
- 代理凭据不写日志；
- 出口模式变更写审计/事件（后续可接 c2_events 或 settings audit）。

## 测试计划

- 单元：effective resolver 三种模式与 fallback；
- API：settings 读写、切换模式、代理池 sync；
- 构建：go build/vet、web build；
- 真机：fixed 代理出口探测、pool 活动切换后出口 IP 变化；
- 回归：流量捕获开/关、Agent 工具、更新。

## 实施阶段

1. Manager 出口解析 + settings 读写；
2. 代理池同步 + 重建；
3. UI 模式选择/状态；
4. 文档/验收。

## 实施记录（v0.4.9）

- settings 新增 egress_mode=direct/fixed/pool；未设置时按 global_proxy 推断（旧库兼容）。
- Manager 新增 EffectiveProxy 解析：direct→空、fixed→global_proxy、pool→活动代理→池兜底→空；ProxyAddr/更新使用 effective。
- 代理池 switch/fallback/global/delete 后同步 Manager，并在 pool 模式重建 Agent。
- 系统配置“出口代理”增加模式选择与当前生效出口显示；fixed 模式才允许编辑固定代理。
- 代理池页文案明确：是否接管由出口模式决定；兜底代理为 pool 第二级。
- 校验：go build/vet、web build、EffectiveProxy 单元测试通过；未做真实代理出口 E2E。

