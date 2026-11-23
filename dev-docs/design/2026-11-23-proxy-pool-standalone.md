# 出口代理池独立页面

## 背景

代理池当前藏在「内网中枢 → 代理池 Tab」里，但它已经是全平台统一出口的一部分（egress_mode=pool），继续埋在后渗透页面里不符合实际使用路径，也容易和全局代理混淆。

二开版本把代理池作为独立功能入口；本设计将主线也拆成独立页面。

## 目标

- 新增独立路由 /function/proxy-pool 和侧边栏入口「出口代理池」；
- 从内网中枢移除代理池 Tab；
- 页面复用现有代理池 API，不改后端；
- 页面顶部显示当前出口模式/实际出口，并链接到系统配置；
- 保留录入、探测、切换、兜底、导入、删除、出口自检能力。

## 非目标

- 不改变代理池存储/API；
- 不把代理池改成 mihomo 本地进程；
- 不改变 egress_mode 语义（见 unified-egress-proxy 设计）。

## 信息架构

- 侧边栏「立足点与后渗透」组新增「出口代理池」；
- 内网中枢只保留会话/隧道/凭据/主机台账/范围审批/WebShell；
- 出口代理池页面：
  - 顶部：当前出口模式（direct/fixed/pool）与实际出口；
  - 操作：录入代理、池兜底、出口自检、mihomo 导入；
  - 列表：地址/认证/状态/出口 IP/延迟/失败/最近检查/探测/切换/删除。

## 组件拆分

- 新组件 web/src/components/proxy-pool-panel.tsx：从内网中枢 page 抽出 ProxyPoolBlock；
- 新页面 web/src/app/(main)/function/proxy-pool/page.tsx 引用该组件；
- 内网中枢删除 TabKey/TABS/渲染/组件与相关类型导入。

## 兼容性

- API 不变，书签 /function/intranet 仍可用；
- 旧代理池数据不变；
- 系统配置出口模式页与代理池页互相链接/说明。

## 测试

- web npm run build；
- 页面能加载代理池、录入/切换/探测；
- 内网中枢无代理池 Tab；
- 系统配置出口模式仍生效。

## 实施记录（v0.4.9）

- 新增 /function/proxy-pool 页面与侧边栏「出口代理池」；
- 代理池 UI 从 IntranetPage 抽到 components/proxy-pool-panel.tsx；
- 内网中枢移除代理池 Tab；
- 页面顶部显示 egress_mode / effective_proxy 并链接系统配置；
- API/数据未改动；web build 通过。

