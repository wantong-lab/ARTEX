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
