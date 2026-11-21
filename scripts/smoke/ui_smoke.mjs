// UI 冒烟（本地/手动）：构建并内嵌前端后启动服务，再执行：
//   SMOKE_BASE=http://127.0.0.1:18892 SMOKE_PASSWORD=... node scripts/smoke/ui_smoke.mjs
// 依赖 playwright 模块与 Chromium；可设置 NODE_PATH / CHROME_PATH。
import { chromium } from "playwright";

const BASE = process.env.SMOKE_BASE ?? "http://127.0.0.1:18892";
const PASSWORD = process.env.SMOKE_PASSWORD ?? "Smoke-Artex-2026!";
const ROUTES = ["/dashboard/", "/function/tasks/", "/function/intel/", "/function/intranet/", "/system/llm/", "/system/settings/"];

const fails = [];
const ok = (name, cond, detail = "") => { console.log((cond ? "PASS" : "FAIL") + " | " + name + " " + detail); if (!cond) fails.push(name); };
const BT = String.fromCharCode(96);

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

const resp = await page.goto(BASE + "/definitely-not-a-real-page-xyz/", { waitUntil: "networkidle" });
ok("unknown-route-404", resp?.status() === 404, "status=" + (resp?.status() ?? "?"));

ok("no-console-errors", consoleErrors.length === 0, JSON.stringify(consoleErrors.slice(0, 3)));
await browser.close();
console.log("UI_SMOKE_TOTAL_FAIL=" + fails.length);
process.exit(fails.length ? 1 : 0);
