import assert from "node:assert/strict";

// First-run behavior against an isolated API, with no router or credentials.
export async function verifyFirstVisit(browser, baseURL) {
  const receipts = [];
  for (const scenario of [
    {browser: "ru-RU", expected: "ru"},
    {browser: "fa-IR", expected: "fa"},
    {browser: "zh-TW", expected: "zh-Hant"},
    {browser: "zh-HK", expected: "zh-Hant"},
    {browser: "zh-CN", expected: "zh-Hans"},
    {browser: "de-DE", expected: "en"},
    {browser: "ru-RU", saved: "en", expected: "en"},
    {browser: "fa-IR", saved: "invalid", expected: "fa"},
    {browser: "ru-RU", blockedStorage: true, expected: "ru"},
  ]) {
    const context = await browser.newContext({locale: scenario.browser, viewport: {width: 1366, height: 768}});
    await context.addInitScript(({saved, blockedStorage}) => {
      if (saved && localStorage.getItem("sb-gateway:locale") === null) localStorage.setItem("sb-gateway:locale", saved);
      if (blockedStorage) Object.defineProperty(window, "localStorage", {get() {throw new DOMException("Storage disabled", "SecurityError");}});
    }, scenario);
    const requests = [], errors = [];
    await context.route("**/*", route => new URL(route.request().url()).origin === new URL(baseURL).origin ? route.continue() : route.abort());
    await context.route("**/api/v1/**", async route => {
      requests.push(new URL(route.request().url()).pathname);
      assert.equal(route.request().method(), "GET");
      assert.equal(new URL(route.request().url()).pathname, "/api/v1/auth/session");
      await route.fulfill({json: {authenticated: false, bootstrap_required: true}});
    });
    const page = await context.newPage();
    page.on("pageerror", error => errors.push(String(error)));
    try {
      await page.goto(baseURL, {waitUntil: "networkidle"});
      await page.locator("#bootstrap-title").waitFor();
      assert.equal(await page.locator(".language-choice-shell").count(), 0);
      assert.equal(await page.locator(".login-language-toggle").inputValue(), scenario.expected);
      assert.equal(await page.locator("html").getAttribute("dir"), scenario.expected === "fa" ? "rtl" : "ltr");
      assert.equal(await page.locator(".login-card .eyebrow, .login-card aside").count(), 0);
      assert.equal(await page.locator("input[minlength='12']").count(), 2);
      assert.ok((await page.locator(".login-lead").textContent()).includes("admin"));
      // Manual language selection still works, even if storage is unavailable.
      await page.locator(".login-language-toggle").selectOption("zh-Hant");
      assert.equal(await page.locator("html").getAttribute("lang"), "zh-Hant");
      if (!scenario.blockedStorage) {
        await page.reload({waitUntil: "networkidle"});
        assert.equal(await page.locator(".login-language-toggle").inputValue(), "zh-Hant");
      }
      assert.deepEqual(errors, []);
      assert.ok(requests.length > 0);
      receipts.push({...scenario, directBootstrap: true, manualLanguage: true});
    } finally {await context.close();}
  }
  return receipts;
}
