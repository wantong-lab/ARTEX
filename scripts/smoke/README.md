# 冒烟测试

- api_smoke.py：无 LLM、无外网依赖的后端冒烟，CI 每次 push/PR 都会执行。
  本地：启动服务后运行
  ```bash
  SMOKE_BASE=http://127.0.0.1:18899 python3 scripts/smoke/api_smoke.py
  ```

- ui_smoke.mjs：真实浏览器 UI 冒烟，已纳入 CI 的 `ui` job（静态导出 + 内嵌二进制 + Chromium）。
  覆盖登录、6 个关键路由、内网中枢 7 个页签、404、加载骨架、401 跳转带回跳、403 无权限 toast、
  暗色/移动端无横向溢出与控制台错误。
  ```bash
  cd scripts/smoke && npm ci && npx playwright install --with-deps chromium
  # 启动内嵌前端的服务并初始化管理员后：
  SMOKE_BASE=http://127.0.0.1:18892 SMOKE_PASSWORD='Smoke-Artex-2026!' \
    NODE_PATH=scripts/smoke/node_modules node scripts/smoke/ui_smoke.mjs
  ```
  本机已有 Chromium 时可额外设 `CHROME_PATH` 指向可执行文件，跳过浏览器下载。
