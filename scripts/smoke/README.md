# 冒烟测试

- api_smoke.py：无 LLM、无外网依赖的后端冒烟，CI 每次 push/PR 都会执行。
  本地：启动服务后运行
  SMOKE_BASE=http://127.0.0.1:18899 python3 scripts/smoke/api_smoke.py
- ui_smoke.mjs：真实浏览器 UI 冒烟（需内嵌前端构建 + Playwright/Chromium）。
  SMOKE_BASE=http://127.0.0.1:18892 SMOKE_PASSWORD=... node scripts/smoke/ui_smoke.mjs
  未纳入 CI（浏览器安装成本高），发布前手动执行。
