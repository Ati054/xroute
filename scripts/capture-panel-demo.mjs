import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdir, readFile } from "node:fs/promises";
import { extname, isAbsolute, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";

// Public documentation fixtures only. Never reads a router, saved session or subscription.
const root = resolve("out");
const output = resolve("output/playwright/xroute-demo");
await mkdir(output, { recursive: true });
const moduleName = process.env.XROUTE_PLAYWRIGHT_MODULE || process.env.SB_PLAYWRIGHT_MODULE || "playwright";
const { chromium } = await import(isAbsolute(moduleName) ? pathToFileURL(moduleName).href : moduleName);
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2" };
const server = createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url, "http://localhost").pathname);
    const file = resolve(root, `.${pathname === "/" ? "/index.html" : pathname}`);
    if (!file.startsWith(root + sep)) throw new Error("Invalid demo asset path");
    const body = await readFile(file);
    response.writeHead(200, { "content-type": mime[extname(file)] || "application/octet-stream" });
    response.end(body);
  } catch { response.writeHead(404); response.end(); }
});
await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolve); });
const baseURL = `http://127.0.0.1:${server.address().port}`;
const config = JSON.parse(await readFile("internal/controlplane/default-config.json", "utf8"));
config.system.deployment_ready = true;
config.system.networking.addresses_confirmed = true;
config.system.management.allowed_source_cidrs = ["192.0.2.0/24"];
config.transports.forEach(item => { item.enabled = false; });
config.reverse_vless_exits = [];
config.remote_users = [];
config.subscriptions = [{ id: "demo", display_name: "Example provider", enabled: true }];
const nodes = [
  { id: "demo-berlin", label: "Germany · Berlin", country: "DE", protocol: "vless", transport: "ws" },
  { id: "demo-amsterdam", label: "Netherlands · Amsterdam", country: "NL", protocol: "vless", transport: "grpc" },
  { id: "demo-stockholm", label: "Sweden · Stockholm", country: "SE", protocol: "reality", transport: "tcp" },
  { id: "demo-singapore", label: "Singapore", country: "SG", protocol: "vless", transport: "xhttp" },
].map(item => ({ ...item, subscription_id: "demo", enabled: true }));
config.policies = [
  { id: "home", display_name: "Home network", enabled: true, mode: "best", traffic_mode: "vless_with_wan_exceptions", outbounds: nodes.map(item => item.id), locations: [], selection_order: nodes.map(item => item.id) },
  { id: "work", display_name: "Work devices", enabled: true, mode: "priority", traffic_mode: "vless_with_wan_exceptions", outbounds: nodes.slice(0, 3).map(item => item.id), locations: [], selection_order: nodes.slice(0, 3).map(item => item.id) },
];
config.local_clients = ["Desktop", "Laptop", "Phone", "Tablet"].map((name, index) => ({ id: `client-${index}`, name, enabled: true, source_kind: "lan", source_cidrs: [`192.0.2.${10 + index}/32`], policy_id: index < 2 ? "work" : "home" }));
const now = Date.now() / 1000;
const mapNodes = fn => Object.fromEntries(nodes.map((node, index) => [node.id, fn(node, index)]));
const health = {
  runtime_confirmed: true, runtime_selected: nodes[0].id, checked_at: now, reserve_count: 3,
  candidate_labels: mapNodes(node => node.label), candidate_nodes: mapNodes(node => node), shortlist: nodes.map(node => node.id),
  availability_ok: mapNodes(() => true), quality_ok: mapNodes(() => true), last_good_at: mapNodes(() => now),
  median_delay_ms: mapNodes((node, index) => [86, 104, 118, 192][index]),
  daily_stats: mapNodes((node, index) => ({ samples: 1440, median_ms: [89, 106, 121, 196][index], availability_percent: 100, loss_percent: 0 })),
  latency_comparisons: mapNodes((node, index) => ({ active: nodes[0].id, active_delay_ms: 86, candidate_delay_ms: [86, 104, 118, 192][index], active_at: now, candidate_at: now, expires_at: now + 120 })),
};
const status = { state: "healthy", active_revision: "demo", selector_health: { home: health, work: health }, subscriptions: {}, acme: {} };
const overview = { build: { container_version: "development", active_core_name: "Xray" }, active_revision: "demo", draft_revision: "demo", pending_config_change_count: 0, status, routeros: { version: "7", channel: "stable" } };
const envelope = { config, revision: "demo", has_active_configuration: true, pending_config_change_count: 0 };
let browser;
try {
  browser = await chromium.launch({ headless: true, ...(process.env.SB_BROWSER_CHANNEL ? { channel: process.env.SB_BROWSER_CHANNEL } : {}) });
  for (const locale of ["en", "ru", "fa", "zh-Hans", "zh-Hant"]) {
    const errors = [], unexpected = [];
    let authenticated = true;
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await context.route("**/*", async route => {
      const request = route.request(), url = new URL(request.url());
      if (url.origin !== baseURL) { unexpected.push("external request"); await route.abort(); return; }
      if (!url.pathname.startsWith("/api/v1/")) { await route.continue(); return; }
      const path = url.pathname.slice(7);
      let body;
      if (request.method() !== "GET") throw new Error("Documentation capture must not write configuration");
      if (path === "/auth/session") body = { authenticated, username: "admin", csrf_token: "demo-only" };
      else if (path === "/bootstrap-state") body = { overview, draft: envelope, status };
      else if (path === "/overview") body = overview;
      else if (path === "/status" || path === "/selectors") body = status;
      else if (path === "/drafts/current") body = envelope;
      else if (path === "/subscriptions/demo/nodes") body = { nodes };
      else if (path === "/client-telemetry") body = { clients: [] };
      else if (path === "/routeros/discover") body = { routeros: {} };
      else if (path === "/routeros/container-status") body = { container: { running: true, restart_count: 0 } };
      else if (path === "/routeros/container/memory") body = { container: { inventory_available: true, memory_high: "288MiB", memory_max: "320MiB" } };
      else if (path === "/lifecycle") body = {};
      else { unexpected.push(path); body = {}; }
      await route.fulfill({ json: body });
    });
    await context.addInitScript(value => localStorage.setItem("sb-gateway:locale", value), locale);
    const page = await context.newPage();
    page.on("pageerror", error => errors.push(String(error)));
    await page.goto(baseURL, { waitUntil: "networkidle" });
    const nav = page.locator(".sidebar nav button");
    await nav.nth(2).click();
    await page.locator(".policy-card-summary").first().waitFor();
    await page.locator(".policy-quality-table-wrap tbody tr").first().waitFor();
    assert.equal(await page.locator("html").getAttribute("lang"), locale);
    assert.equal(await page.locator("html").getAttribute("dir"), locale === "fa" ? "rtl" : "ltr");
    assert.equal(await page.locator(".brand-name").first().innerText(), "XRoute");
    assert.equal(await page.locator(".language-toggle option").count(), 5);
    // Native language names in the selector intentionally retain their own script.
    if (locale === "en") assert.doesNotMatch(await page.locator("main").innerText(), /[А-Яа-яЁё]/u);
    await page.screenshot({ path: resolve(output, `panel-routing-${locale}.png`), animations: "disabled" });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${locale}: mobile page overflow`);
    await page.screenshot({ path: resolve(output, `panel-routing-${locale}-mobile.png`), animations: "disabled" });

    await page.locator(".mobile-menu").click();
    await page.locator(".sidebar-open .brand-name").waitFor();
    await page.screenshot({ path: resolve(output, `navigation-${locale}-mobile.png`), animations: "disabled" });

    await page.locator(".sidebar-close").click();
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.locator(".policy-card-actions .button-secondary").first().click();
    const editor = page.getByRole("dialog");
    await editor.getByRole("button", { name: "IR", exact: true }).click();
    const iran = editor.locator('input[name="exception_services"][value="ir-local"]');
    assert.equal(await iran.isChecked(), false, "regional rule must be opt-in");
    await iran.check();
    assert.equal(await iran.isChecked(), true);
    if (locale !== "ru") assert.doesNotMatch(await editor.innerText(), /[А-Яа-яЁё]/u, "localized editor must not fall back to Russian");
    await page.screenshot({ path: resolve(output, `region-ir-${locale}.png`), animations: "disabled" });
    await editor.getByRole("button", { name: "CN", exact: true }).click();
    assert.equal(await editor.getByLabel("GeoIP CN", { exact: true }).isChecked(), false);
    await editor.getByRole("button", { name: "IR", exact: true }).click();
    assert.equal(await iran.isChecked(), true, "switching the catalog tab must retain the local selection");
    await editor.locator(".modal-close").click();
    authenticated = false;
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.reload({ waitUntil: "networkidle" });
    await page.locator(".login-card").waitFor();
    assert.equal(await page.locator(".brand-name").innerText(), "XRoute");
    assert.equal(await page.locator(".brand-mark").getAttribute("aria-hidden"), "true");
    await page.screenshot({ path: resolve(output, `login-${locale}.png`), animations: "disabled" });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${locale}: mobile login overflow`);
    await page.screenshot({ path: resolve(output, `login-${locale}-mobile.png`), animations: "disabled" });
    assert.deepEqual(errors, [], `${locale}: browser errors`);
    assert.deepEqual(unexpected, [], `${locale}: unexpected requests`);
    console.log(`${locale}: desktop/mobile screenshot and locale checks passed`);
    await context.close();
  }
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
