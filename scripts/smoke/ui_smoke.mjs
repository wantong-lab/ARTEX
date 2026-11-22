// UI 冒烟（本地/CI）：构建并内嵌前端后启动服务，再执行：
//   SMOKE_BASE=http://127.0.0.1:18892 SMOKE_PASSWORD=... node scripts/smoke/ui_smoke.mjs
// 覆盖：登录、关键路由、内网中枢页签、404、暗色/移动端无横向溢出、
// 加载骨架、401 跳转带回跳 next、403 统一无权限提示、控制台错误。
// 依赖 playwright 模块与 Chromium；可设置 NODE_PATH / CHROME_PATH。
import { createRequire } from "node:module";

// ESM import 不认 NODE_PATH；用 CJS resolver 以便 CI/本地通过
// NODE_PATH=scripts/smoke/node_modules 复用安装好的 playwright。
const requireCJS = createRequire(import.meta.url);
const { chromium } = requireCJS("playwright");

const BASE = process.env.SMOKE_BASE ?? "http://127.0.0.1:18892";
const PASSWORD = process.env.SMOKE_PASSWORD ?? "Smoke-Artex-2026!";
const ROUTES = ["/dashboard/", "/function/tasks/", "/function/intel/", "/function/intranet/", "/system/llm/", "/system/settings/"];

const fails = [];
const ok = (name, cond, detail = "") => { console.log((cond ? "PASS" : "FAIL") + " | " + name + " " + detail); if (!cond) fails.push(name); };

const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
const consoleErrors = [];
page.on("pageerror", e => consoleErrors.push(String(e).slice(0, 160)));
page.on("console", m => { if (m.type() === "error") consoleErrors.push(m.text().slice(0, 160)); });

await page.goto(BASE + "/login/", { waitUntil: "networkidle" });
ok("login-page-load", (await page.locator("#password").count()) === 1);
const cb = page.locator('input[type="checkbox"], [role="checkbox"]').first();
if (await cb.count()) await cb.click({ force: true });
await page.locator("#password").fill(PASSWORD);
await page.waitForTimeout(120);
await page.getByRole("button", { name: "登录", exact: true }).first().click({ timeout: 10000 });
await page.waitForTimeout(1800);
ok("login-ok", !page.url().includes("/login"), page.url());
const token = await page.evaluate(() => localStorage.getItem("artex_token") ?? "");
ok("login-token", token.length > 20);

for (const route of ROUTES) {
  await page.goto(BASE + route, { waitUntil: "networkidle" });
  await page.waitForTimeout(400);
  const main = ((await page.locator("main").innerText().catch(() => "")) || "").trim();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  ok("route " + route, main.length > 5 && overflow <= 4, "len=" + main.length + " overflow=" + overflow);
}

await page.goto(BASE + "/function/intranet/", { waitUntil: "networkidle" });
let tabs = 0;
for (const t of ["会话", "隧道", "凭据", "主机台账", "范围审批", "WebShell", "代理池"]) {
  const b = page.getByRole("button", { name: t, exact: true }).first();
  if (await b.count()) { await b.click(); await page.waitForTimeout(220); tabs++; }
}
ok("intranet-tabs", tabs === 7, "tabs=" + tabs);

const errorsBeforeFaults = consoleErrors.length;

// 加载骨架：延迟 intranet 数据请求，应出现 role=status 的骨架。
await page.route("**/api/intranet/**", async (route) => {
  await new Promise((r) => setTimeout(r, 2500));
  try {
    await route.continue();
  } catch {
    // 页面/路由在延迟期间已被取消：忽略，避免 unhandled rejection 让冒烟失败。
  }
});
await page.goto(BASE + "/function/intranet/", { waitUntil: "domcontentloaded" });
await page.waitForTimeout(700);
const skeletons = await page.locator('[role="status"][aria-label="加载中"]').count();
ok("loading-skeleton", skeletons > 0, "skeletons=" + skeletons);
await page.waitForTimeout(2800); // 放掉延迟路由，避免 unroute 后 continue 报 already handled
await page.unroute("**/api/intranet/**");

// 403：统一无权限 toast。
await page.route("**/api/scheduler/status*", (route) =>
  route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: "无权限-smoke" }) }),
);
await page.goto(BASE + "/system/retention/", { waitUntil: "networkidle" });
await page.waitForTimeout(1200);
const toastText = (await page.locator("[data-sonner-toast]").allInnerTexts()).join(" ");
ok("forbidden-toast", toastText.includes("无权限-smoke"), toastText.slice(0, 120));
await page.unroute("**/api/scheduler/status*");

const resp = await page.goto(BASE + "/definitely-not-a-real-page-xyz/", { waitUntil: "networkidle" });
ok("unknown-route-404", resp?.status() === 404, "status=" + (resp?.status() ?? "?"));

// 401：清凭据访问受保护页 -> /login?next=...
await page.evaluate(() => {
  localStorage.removeItem("artex_token");
  document.cookie = "artex_token=; path=/; max-age=0";
});
await page.goto(BASE + "/function/tasks/", { waitUntil: "networkidle" });
await page.waitForTimeout(1200);
ok("unauthorized-redirect-next", page.url().includes("/login") && page.url().includes("next="), page.url());

ok("no-console-errors", errorsBeforeFaults === 0, JSON.stringify(consoleErrors.slice(0, 3)));

// 移动端：关键页面 390px 无横向溢出。
await ctx.close();
const mctx = await browser.newContext({ viewport: { width: 390, height: 844 } });
const mp = await mctx.newPage();
await mp.goto(BASE + "/login/", { waitUntil: "networkidle" });
await mp.evaluate((tok) => {
  localStorage.setItem("artex_token", tok);
  document.cookie = "artex_token=" + tok + "; path=/";
}, token);
for (const route of ["/dashboard/", "/function/c2/", "/function/intranet/", "/function/tasks/"]) {
  await mp.goto(BASE + route, { waitUntil: "networkidle" });
  await mp.waitForTimeout(500);
  const m = await mp.evaluate(() => ({ overflow: document.documentElement.scrollWidth - window.innerWidth }));
  const mainLen = ((await mp.locator("main").innerText().catch(() => "")) || "").trim().length;
  ok("mobile " + route, mainLen > 5 && m.overflow <= 2, "len=" + mainLen + " overflow=" + m.overflow);
}
await mctx.close();

await browser.close();
console.log("UI_SMOKE_TOTAL_FAIL=" + fails.length);
process.exit(fails.length ? 1 : 0);
