# 贡献与开发规范

## 环境

- Go 1.26.3
- Node.js 22（`web/`）
- PostgreSQL 15 + pgvector（可选，没有时数据库用例自动跳过）

## 本地命令

```bash
go build ./... && go vet ./... && go test ./...
cd web && npm ci && npm run build
# 指定测试数据库：
ARTEX_PG_DSN='postgres://artex:artex@127.0.0.1:5432/artex_test?sslmode=disable' go test ./...
```

## 分支与提交

- 分支：`<type>/<里程碑>-<序号>-<简述>`，如 `feat/m1-01-chainskel`
- 提交：Conventional Commits，scope 用模块名（`agent`、`db`、`server`、`guard`、`web`）
- 单 PR 单主题；超过 400 行改动先拆
- 提交正文写「为什么 + 验收方式」

## PR 检查清单

- [ ] 构建与静态检查通过
- [ ] 新增逻辑有单测；修 bug 有回归用例
- [ ] 新能力默认关闭或温和模式
- [ ] 迁移幂等，只增不改
- [ ] 复制外部代码已在 `dev-docs/upstream-imports.md` 登记
- [ ] 文档与开关说明已更新

## 测试约定

- 无 `ARTEX_PG_DSN` 时，需要数据库的用例自行 `t.Skip`。
- 纯逻辑包必须可在无数据库、无网络环境下跑通。
- 端口占用、时间、随机数相关用例必须可重复执行。