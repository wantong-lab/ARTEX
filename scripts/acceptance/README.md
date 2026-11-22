# 验收脚本（acceptance）

本目录脚本用于对**已构建/已启动**的 ARTEX 做端到端验收，均只依赖标准工具（curl/python3/docker/Go），不引入额外服务。

## LLM 全链路 E2E（P2-11）

`llm_e2e.py`

对运行中的服务执行：登录 → 确认激活的 LLM 配置真实连通 → 创建任务并完成 planner→worker→Bash 标记执行 → 核对活动历史与 token 用量 → 登记全局 deny 拦截规则并验证 worker 工具调用被拦截且未执行。

```bash
# 服务已用 ARTEX_PG_DSN 启动、且已配置可用的 LLM profile
python3 scripts/acceptance/llm_e2e.py --base http://127.0.0.1:18905 --password 'Accept-Artex-2026!'
```

可选环境变量：`ARTEX_LLM_API_KEY`/`ARTEX_LLM_BASE_URL`/`ARTEX_LLM_MODEL`/`ARTEX_LLM_SESSION_HEADER`（提供时自动创建/更新 profile 并激活）；`--skip-governance` 跳过治理场景。

## 升级 / 回滚 / 备份恢复 / soak（P2-12）

`upgrade_rollback.sh`

真实进程 + 真实 PostgreSQL 的升级演练：

1. 旧版 `v0.4.1` 二进制起服务，初始化管理员并创建任务；
2. `pg_dump -Fc` + `tar` 备份数据库与 data 目录；
3. 新版（当前 HEAD，`-tags embedui`）在同一 DB/data 上启动：校验迁移数、任务持久化、核心 API；
4. soak：多并发持续请求核心接口（默认 45s × 8 workers），统计错误并检查健康/RSS；
5. 恢复备份 DB/data 后旧版二进制回滚启动，校验任务仍可见；
6. 旧数据上再次启动新版，校验迁移可重放、任务仍可见。

准备：

```bash
# 旧版发布包（GitHub Release v0.4.1 darwin-arm64 示例）
gh release download v0.4.1 --pattern 'artex-0.4.1-darwin-arm64.zip' --repo wantong-lab/ARTEX
unzip artex-0.4.1-darwin-arm64.zip -d /tmp/artex-v041

# 当前 HEAD 内嵌前端二进制
cp -R web/out/. server/webui/dist/
CGO_ENABLED=0 go build -tags embedui -o /tmp/artex-current ./cmd/artex

# 运行（docker 内 PostgreSQL 容器默认 artex-migtest）
OLD_BIN=/tmp/artex-v041/artex-0.4.1-darwin-arm64/artex NEW_BIN=/tmp/artex-current \
  bash scripts/acceptance/upgrade_rollback.sh
```

脚本输出 `PASS ...`/`FAIL ...` 与最终 `RESULT PASS|FAIL`；退出码即验收结果。

## 全量 race

```bash
docker exec artex-migtest psql -U artex -d postgres -c 'DROP DATABASE IF EXISTS artex_race WITH (FORCE)' -c 'CREATE DATABASE artex_race'
CGO_ENABLED=1 ARTEX_PG_DSN='postgres://artex:artex@127.0.0.1:55432/artex_race?sslmode=disable' \
  go test -race ./... -count=1
```

要求 `go vet ./...` 与 race 全绿；数据竞争修复见 CHANGELOG（agent hook 同步、memory 状态原子化）。


## 安全审计（P2-13）

`security_audit.py`

对运行中的服务校验：凭据/代理池/C2 token 落库为 gcm1 密文且无明文、C2 非回环默认拒绝与显式开关、settings 关闭恢复、会话 secret 密文格式、审计哈希链校验端点。轮换本身由 `go test ./db -run TestRotateSecretKeys` 与 `cmd/artex-rotate-key` 覆盖（需停服务执行）。

```bash
python3 scripts/acceptance/security_audit.py --base http://127.0.0.1:18905 --db artex_p213
```


## 可观测性（P2-14）

observability_check.py

对运行中的服务校验 /metrics 指标集合与请求计数增长、/api/observability/status JSON、以及 ARTEX_LOG_FORMAT=json 时的结构化访问日志；服务需以 JSON 日志模式启动并传入 --log-file。

python3 scripts/acceptance/observability_check.py --base http://127.0.0.1:18907 --log-file /tmp/artex-obs.log


## LLM 失败转移 + 流式探测（v0.4.3 补测）

`llm_failover.py` 在真实服务上创建一条坏 provider（不可达 base_url），用任务级 profile 链
[坏配置, 好配置] 验证建连失败后自动切到好配置并真实执行命令；随后探测流式 SSE 与非流式。
需要数据库里已有可用激活 profile。

```bash
python3 scripts/acceptance/llm_failover.py --base http://127.0.0.1:18911 --password 'Accept-Artex-2026!'
```

## SMB 真机测试（v0.4.3 补测）

`session/smb_real_host_test.go` 默认跳过；设置 Windows 目标后验证 wmiexec→atexec 自动回退、
复合 cmd 命令、文件写入/读取/删除：

```bash
export PATH="$HOME/anaconda3/bin:$PATH"   # 需要 impacket wmiexec.py/atexec.py
ARTEX_SMB_TEST_HOST=192.168.112.133 ARTEX_SMB_TEST_USER=artexsmb \
  ARTEX_SMB_TEST_PASS='...' \
  go test ./session -run TestSMBRealWindows -v -timeout 240s
```

目标侧准备（本地管理员账号、允许 ADMIN$ 管理共享）与 RPC/DCOM 被防火墙拦截时的 atexec 回退说明见 docs/faq.md。

升级脚本已适配 0012 迁移：`EXPECTED_MIGRATIONS=12`，实跑 25 步 PASS。

## 跨 OS 隧道真机测试（v0.4.4）

tunnel/chisel_windows_e2e_test.go 默认跳过；验证 平台 macOS → Windows 目标的跨 OS/架构隧道：
SMB 会话探测 Windows、上传/拉起目标端 chisel.exe、平台 SOCKS5 经 Windows 访问 LAN HTTP 服务。

    # 平台侧 server 二进制；目标侧客户端布局 <os>-<arch>/chisel[.exe]
    export ARTEX_CHISEL_PATH=/tmp/artex-chisel/darwin-arm64/chisel
    export ARTEX_CHISEL_CLIENT_DIR=/tmp/artex-chisel/clients
    export ARTEX_WIN_SMB_HOST=192.168.112.133 ARTEX_WIN_SMB_USER=artexsmb ARTEX_WIN_SMB_PASS='...'
    export ARTEX_TUNNEL_CALLBACK_HOST=192.168.112.55   # 平台 LAN IP，目标可达
    export ARTEX_PG_DSN='postgres://artex:artex@127.0.0.1:55432/artex_tunnel?sslmode=disable'
    go test ./tunnel -run TestChiselWindowsRealE2E -v -timeout 300s

Windows 目标需要本地管理员账号（SMB 会话）与 ADMIN$ 共享；测试后清理账号/注册表/临时目录。
目标机有火绒等 AV 时，官方 chisel 可能被秒删；用 -ldflags "-s -w" 构建的 client 或加白名单。

## 公网 VPS 跨主机隧道（v0.4.5）

tunnel/chisel_vps_e2e_test.go 默认跳过；验证 平台 macOS → 公网 Linux VPS 的 chisel 隧道：
平台 chisel server 绑 0.0.0.0:39444（公网映射 39.89.211.55:39444 -> 平台:39444），
经 SSH 会话上传/拉起 linux client，平台 SOCKS5 经 VPS 访问 VPS 本地 HTTP 并校验公网出口 IP。

```bash
export ARTEX_VPS_SSH_HOST=113.20.3.124 ARTEX_VPS_SSH_USER=root
export ARTEX_VPS_SSH_KEY_PATH=$HOME/.ssh/id_ed25519_113.20.3.124
export ARTEX_CHISEL_PATH=/tmp/artex-chisel/darwin-arm64/chisel
export ARTEX_CHISEL_CLIENT_DIR=/tmp/artex-chisel/clients
export ARTEX_TUNNEL_CALLBACK_HOST=39.89.211.55
export ARTEX_PG_DSN='postgres://artex:artex@127.0.0.1:55432/artex_vps?sslmode=disable'
go test ./tunnel -run TestChiselVPSRealE2E -v -timeout 320s
```

平台需设 ARTEX_TUNNEL_STAGE_PORT=39443（映射到平台的固定 bootstrap 端口）；测试结束会清理 VPS 上的 chisel 进程/临时目录。
