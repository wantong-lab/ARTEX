# C2 入站中继反向模式（平台主动连 VPS）

## 背景

现有 ingress 模式要求 VPS 主动回连平台 chisel server，因此平台必须有公网可达入口。手机热点/家庭 NAT/CGNAT 环境下 VPS 无法访问平台 39443/39444，现有方式 B 不可用。

## 目标

- 平台只做出站连接，即可把 VPS 回调端口反向转发到平台 C2 loopback；
- 目标仍然只回连 VPS；
- 不新增数据库迁移；
- 给 worker 明确的 A/forward/reverse 决策规则。

## 非目标

- 不替代 SSH 会话本身；
- 不做自动公网探测/自动切换（先显式 mode）；
- 不做 443 前置代理（P2-A 暂不做）。

## 拓扑

~~~text
目标植入端
  -> VPS:回调端口（如 39444）
  -> VPS chisel server（--reverse）
  <- 平台 chisel client（平台主动出站 -> VPS:控制端口 39443）
  -> 平台 127.0.0.1:C2 listener
~~~

## 数据模型

Plan 新增：

- reverse：true 表示反向；
- remote_auth_file：VPS 上的 authfile 路径；
- 复用 listen_port（VPS 回调）、platform_chisel_port（VPS 控制端口）、target_port（平台 C2 loopback）、c2_listener_id、c2_callback_host、node_label。

不新增 DB 迁移；deploy_params 向后兼容。

## 部署契约

1. 通过已有 VPS 会话把 chisel 二进制写入 VPS（reverse 场景平台/VPS 不可互访，跳过 HTTP 下载，直接 WriteFile）；
2. 写 VPS authfile；
3. VPS 启动 chisel server --reverse --authfile；
4. 平台启动 chisel client（AUTH 走环境变量）连 VPS 控制端口；
5. 平台拨 VPS 回调端口做验证；
6. 任一步失败按现有 rollback 清理双方进程/文件/台账。

## 工具/API/UI

- c2_ingress_deploy 增加 mode=forward|reverse（默认 forward）；
- POST /api/c2/ingress 增加 mode；
- C2 页面“网络模式”下拉：
  - 常规：VPS 回连平台（平台有公网）；
  - 反向：平台主动连 VPS（热点/NAT）；
- reverse 时 platform_host 忽略，platform_chisel_port 表示 VPS 控制端口（默认 39443）。

## worker 决策规则

1. 有 VPS + 平台有公网映射 -> forward；
2. 有 VPS + 平台无公网（NAT/热点） -> reverse；
3. 无 VPS + 目标能访问平台 -> 平台 listener + 植入端（不用 ingress）；
4. 无 VPS + 平台不可达 -> 明确报告无法建立回调，不盲目部署。

该规则写入 TunnelToolSeeds 的 c2_ingress_deploy 描述，worker 可枚举到工具。

## 安全

- 平台 C2 listener 仍只绑 127.0.0.1；
- VPS 控制端口 39443 建议限制来源 IP + chisel auth；
- 回调端口 39444 对目标开放；
- auth 不进进程命令行（客户端 AUTH env，服务端 authfile）。

## 测试计划

- 单元：Reverse plan 参数（--reverse、R: 转发、authfile）；
- 真机：平台热点 -> VPS 反向隧道 -> 本地 HTTP 服务（已验证 200）；
- 待补：完整 C2 listener + 植入端反向链路；
- 回归：forward ingress、P0/P1 现有链路。

## 实施记录（v0.4.9）

- 已加 PlanIngressReverse、Manager.deployReverse、mode 分支、UI 模式选择、worker 工具目录与决策描述；
- 手动 chisel 反向链路验证：平台出站 -> VPS 39443 -> VPS 39444 -> 本地 HTTP 200；
- 未做完整 C2 植入端反向 E2E（需目标可达环境）。提交 b1f2372；本机二进制已重建/重启 0.4.9。
