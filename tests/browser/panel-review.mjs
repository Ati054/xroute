import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

export async function verifyPanelReview(browser, baseURL) {
  const defaults = JSON.parse(await readFile(new URL("../../internal/controlplane/default-config.json", import.meta.url), "utf8"));
  const localeFormats = {ru: "ru-RU", en: "en-GB", fa: "fa-IR", "zh-Hans": "zh-CN", "zh-Hant": "zh-TW"};
  const prepareLabels = {ru: "Подготовить", en: "Prepare", fa: "آماده‌سازی", "zh-Hans": "准备", "zh-Hant": "準備"};
  const expiryLabels = {ru: "Срок не определён", en: "Expiry unknown", fa: "انقضا نامشخص است", "zh-Hans": "有效期未知", "zh-Hant": "有效期限未知"};
  const defaultProfileNames = {ru: "CDN TLS · основной", en: "Primary CDN TLS", fa: "TLS اصلی CDN", "zh-Hans": "主 CDN TLS", "zh-Hant": "主要 CDN TLS"};
  const duplicateErrors = {ru: "Этот архив уже сохранён.", en: "This archive is already saved.", fa: "این بایگانی قبلاً ذخیره شده است.", "zh-Hans": "此归档已保存。", "zh-Hant": "此封存檔已儲存。"};
  const passwordErrors = {ru: "Неверный пароль администратора панели.", en: "The panel administrator password is incorrect.", fa: "رمز عبور مدیر پنل اشتباه است.", "zh-Hans": "面板管理员密码不正确。", "zh-Hant": "面板管理員密碼不正確。"};
  const longPolicyName = "Office policy for multiple segments and working groups ".repeat(5).trim();
  const started = "2026-02-03T16:17:18Z";
  const expires = "2035-02-09T14:00:00Z";
  const backup = {name: "xroute-test.sbgw", size_bytes: 100, created_at: started, ssd: true, routeros_files: false};
  const event = {action: "recovery.create", outcome: "ok", timestamp: started};
  const result = [];
  for (const [locale, format] of Object.entries(localeFormats)) {
    const config = structuredClone(defaults);
    config.system.deployment_ready = true;
    config.policies = [{id: "office", display_name: longPolicyName}];
    config.local_clients = [{id: "ready", name: "Ready device", enabled: true}, {id: "off", name: "Off device", enabled: false, policy_id: "office"}];
    config.transports.find(transport => transport.kind === "xhttp").cdn_deployments = [{id: "one"}, {id: "two"}];
    config.tls_profiles.unshift({id: "review", display_name: "Review profile", enabled: true, certificate_source: "acme",
      certificate_secret_ref: "tls/review/certificate", private_key_secret_ref: "tls/review/private-key"});
    const draft = {config, revision: "draft", has_active_configuration: true, pending_config_change_count: 10,
      pending_config_changes: [
        {op: "add", collection: "local_clients", display_name: "New device"},
        {op: "replace", collection: "routing"},
        {op: "remove", collection: "remote_users", display_name: "Old user"},
        {op: "add", collection: "networks", entity_id: "office-lan"},
        {op: "add", collection: "networks", entity_id: "guest-lan"},
        {op: "replace", collection: "dns"},
        {op: "replace", collection: "security"},
        {op: "replace", collection: "service_packs"},
      ]};
    const status = {state: "healthy", active_revision: "applied", selector_health: {}, subscriptions: {}, acme: {}};
    const overview = {status, active_revision: "applied", draft_revision: "draft", routeros: {}, recent_events: [event], recent_audit: []};
    const acme = {state: "running", message: "Проверка DNS и выпуск сертификата…", operation_stage: "dns_precheck",
      last_attempt: started, operation_timeout_seconds: 900, settings: {enabled: true, provider: "yandexcloud", email: "test@example.test", domains: ["example.test"], terms_accepted: true}};
    let backups = [];
    let recoveryUploads = 0;
    const context = await browser.newContext({timezoneId: "UTC", viewport: {width: locale === "en" || locale === "fa" ? 390 : 1440, height: 820}});
    await context.addInitScript(value => localStorage.setItem("sb-gateway:locale", value), locale);
    await context.route("**/*", route => new URL(route.request().url()).origin === new URL(baseURL).origin ? route.continue() : route.abort());
    await context.route("**/api/v1/**", async route => {
      const request = route.request(), path = new URL(request.url()).pathname.slice(7);
      let body;
      if (path === "/auth/session") body = {authenticated: true, username: "Test administrator", csrf_token: "test-token"};
      else if (path === "/bootstrap-state") body = {overview, draft, status};
      else if (path === "/overview") body = overview;
      else if (path === "/status") body = status;
      else if (path === "/drafts/current") body = draft;
      else if (path === "/client-telemetry") body = {clients: []};
      else if (path === "/tls-profiles/review/acme") body = acme;
      else if (path === "/network/tls-probe" && request.method() === "POST") {
        body = {addresses: ["192.0.2.10"], tls_version: "TLS 1.3", certificate_expires_at: expires};
      }
      else if (path === "/recovery/backups") {
        if (request.method() === "POST") {
          backups = [backup];
          overview.recent_audit = [event];
        }
        body = {items: backups, retention_limit: 3, routeros_error: "mirror-not-configured"};
      } else if (path === "/recovery/backups/upload/preflight" && request.method() === "POST") {
        const file = request.postDataJSON();
        assert.equal(file.filename, "xroute-uploaded.sbgw");
        assert.equal(file.size_bytes, 3 * 1024 ** 2);
        if (backups.some(item => item.name === file.filename)) {
          return route.fulfill({status: 409, json: {error: {code: "recovery_archive_exists", message: "This recovery archive is already stored.", request_id: "duplicate-test"}}});
        }
        body = {ok: true};
      } else if (path === "/recovery/backups/upload" && request.method() === "PUT") {
        recoveryUploads++;
        const uploaded = {...backup, name: "xroute-uploaded.sbgw"};
        backups.push(uploaded);
        overview.recent_audit = [{...event, action: "recovery.upload"}, event];
        body = uploaded;
      } else if (path === "/recovery/restore" && request.method() === "POST") {
        return route.fulfill({status: 403, json: {error: {code: "reauthentication_failed", message: "Administrator password confirmation failed.", request_id: "password-test"}}});
      } else body = {};
      await route.fulfill({json: body});
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", error => errors.push(String(error)));
    async function assertLocalized(locator) {
      const text = await locator.innerText();
      if (locale !== "ru") assert.doesNotMatch(text, /[А-Яа-яЁё]/u, `${locale}: ${text}`);
    }
    async function assertPicker(locator) {
      await locator.waitFor();
      const picker = await locator.evaluate(input => {
        const style = getComputedStyle(input, "::file-selector-button");
        const rect = input.getBoundingClientRect();
        return {gap: style.marginInlineEnd, border: style.borderInlineStartWidth, fits: rect.left >= 0 && rect.right <= window.innerWidth};
      });
      assert.deepEqual(picker, {gap: "10px", border: "1px", fits: true}, locale);
    }
    try {
      await page.goto(baseURL, {waitUntil: "networkidle"});
      const nav = page.locator(".sidebar nav button");
      const navigate = async (index) => {
        const mobileMenu = page.locator(".mobile-menu");
        if (await mobileMenu.isVisible()) await mobileMenu.click();
        await nav.nth(index).click();
      };
      const activity = page.locator(".overview-audit-list");
      await assertLocalized(activity);
      if (locale === "ru") assert.match(await activity.innerText(), /Создана резервная копия/);
      const changes = page.locator(".draft-change-popover li");
      await changes.first().waitFor({state: "attached"});
      assert.match(await page.locator(".draft-change-disclosure button").innerText(), /\b10\b/);
      if (locale === "en") {
        assert.equal(await page.locator(".draft-change-popover small").last().textContent(), "2 more");
      }
      const changeLabels = await changes.allTextContents();
      assert.equal(new Set(changeLabels).size, 8);
      assert.match(changeLabels[0], /New device/);
      assert.match(changeLabels[3], /office-lan/);
      assert.match(changeLabels[4], /guest-lan/);
      assert.match(changeLabels[5], /DNS/);
      const policyLabel = name => page.getByRole("row").filter({hasText: name}).locator(".overview-policy-chip > span");
      const longLabel = policyLabel("Off device");
      const shortLabel = policyLabel("Ready device");
      assert.equal(await longLabel.getAttribute("title"), longPolicyName);
      const layout = element => ({ellipsis: getComputedStyle(element).textOverflow, fits: element.scrollWidth <= element.clientWidth});
      assert.deepEqual(await longLabel.evaluate(layout), {ellipsis: "ellipsis", fits: false}, locale);
      assert.deepEqual(await shortLabel.evaluate(layout), {ellipsis: "ellipsis", fits: true}, locale);
      if (locale === "ru") assert.equal(await shortLabel.innerText(), "Не назначен");
      await navigate(1);
      await page.getByRole("row").filter({hasText: "Ready device"}).locator(".status-info").waitFor();
      await page.getByRole("row").filter({hasText: "Off device"}).locator(".status-muted").waitFor();
      await navigate(3);
      const profiles = page.locator(".tls-profile-panel");
      await page.locator(".transport-groups").getByRole("button", {name: prepareLabels[locale], exact: true}).first().waitFor();
      const webSocket = page.locator(".transport-card").filter({has: page.getByRole("heading", {name: /WebSocket/i})});
      await profiles.getByRole("heading", {name: defaultProfileNames[locale], exact: true}).waitFor();
      assert.ok((await webSocket.innerText()).includes(defaultProfileNames[locale]), locale);
      if (locale === "en") {
        const xhttp = page.locator(".transport-card").filter({has: page.getByRole("heading", {name: "XHTTP + TLS", exact: true})});
        assert.match(await webSocket.innerText(), /1 CDN deployment ·/);
        assert.match(await xhttp.innerText(), /2 CDN deployments ·/);
      }
      await webSocket.getByRole("button", {name: prepareLabels[locale], exact: true}).click();
      const editor = page.getByRole("dialog");
      const cdn = editor.locator(".cdn-deployment-row").first();
      await cdn.waitFor();
      const legend = cdn.locator("legend");
      await assertLocalized(cdn);
      assert.equal(await cdn.locator('input').first().inputValue(), await legend.innerText());
      const summary = cdn.locator(".cdn-client-overrides summary");
      assert.equal(await summary.locator("span").evaluate(span => getComputedStyle(span).display), "inline-flex");
      await summary.click();
      assert.equal(await summary.evaluate(element => element.parentElement.open), true);
      await cdn.locator('input[placeholder="edge.example.com"]').fill("edge.example.test");
      await cdn.locator('input[list^="cdn-origin-dns-"]').fill("origin.example.test");
      const probe = cdn.locator(".reality-target-check");
      await probe.getByRole("button").click();
      const probeResult = probe.getByRole("status");
      await probeResult.waitFor();
      await assertLocalized(probeResult);
      const expectedExpiry = await page.evaluate(([date, language]) => new Intl.DateTimeFormat(language,
        {dateStyle: "medium", timeStyle: "short"}).format(new Date(date)), [expires, format]);
      assert.equal((await probeResult.innerText()).split(expectedExpiry).length - 1, 2);
      await editor.locator(".modal-close").click();
      await editor.waitFor({state: "hidden"});

      await page.locator(".transport-groups .settings-launch-card").first().getByRole("button").click();
      const subscription = page.locator(".subscription-endpoint-dialog");
      const publication = subscription.locator(".subscription-publication-toggle");
      if (!(await publication.locator("input").isChecked())) await publication.click();
      await subscription.locator('label.subscription-mode-option:has([value="cdn"])').click();
      const advanced = subscription.locator(".subscription-advanced-settings");
      await advanced.locator("summary").click();
      const originHint = advanced.locator('label:has(option[value="auto-cidr"]) small');
      await originHint.waitFor();
      await assertLocalized(originHint);
      const cloudflareHint = await originHint.innerText();
      await subscription.locator('.subscription-address-grid-separate select:has(option[value="gcore"])').selectOption("gcore");
      await assertLocalized(originHint);
      assert.notEqual(await originHint.innerText(), cloudflareHint);
      await subscription.locator(".modal-close").click();

      await profiles.getByText(expiryLabels[locale], {exact: true}).waitFor();
      await profiles.locator(".button-compact").first().click();
      const transfer = page.locator(".tls-transfer-dialog[open]");
      await assertPicker(transfer.locator('input[type="file"]'));
      await assertLocalized(transfer);
      await page.keyboard.press("Escape");
      await transfer.waitFor({state: "hidden"});

      await profiles.locator(".button-compact").last().click();
      await assertPicker(editor.locator('input[name="certificate"]'));
      await assertPicker(editor.locator('input[name="private_key"]'));
      await editor.locator("select").first().selectOption("acme");
      await editor.locator('input[name="acme_domains"]').fill("example.test");
      await editor.locator("select").filter({has: page.locator('option[value="acmedns"]')}).selectOption("acmedns");
      const acmeAccounts = editor.locator('input[type="file"]');
      await assertPicker(acmeAccounts);
      await acmeAccounts.setInputFiles({name: "test.json", mimeType: "application/json", buffer: Buffer.from("{")});
      await editor.getByRole("alert").waitFor();
      await assertLocalized(editor);
      await editor.locator(".modal-close").click();
      await editor.waitFor({state: "hidden"});

      await profiles.locator("article").filter({hasText: "Review profile"}).locator(".button-tertiary").click();
      await assertPicker(editor.locator('input[type="file"]'));
      const acmeStatus = editor.locator(".acme-status");
      await acmeStatus.waitFor();
      const expectedDate = await page.evaluate(([date, language]) => new Date(date).toLocaleString(language), [started, format]);
      const shownStatus = await acmeStatus.innerText();
      assert.ok(shownStatus.includes(expectedDate), `${locale}: ${shownStatus}`);
      await assertLocalized(editor);
      await editor.locator(".modal-close").click();

      await navigate(4);
      const recovery = page.locator(".recovery-card");
      const history = page.getByRole("row").filter({hasText: /recovery\.create|Создание резервной копии|Create backup|ایجاد نسخهٔ پشتیبان|创建备份|建立備份/});
      assert.equal(await history.count(), 0);
      await recovery.locator(".recovery-toolbar .button-primary").click();
      await recovery.locator(".recovery-row .status-ok").waitFor();
      assert.equal(await recovery.locator(".status-warn").count(), 0);
      assert.doesNotMatch(await recovery.innerText(), /RouterOS Files/);
      await history.waitFor();
      assert.doesNotMatch(await history.innerText(), /recovery\.create/);
      const upload = recovery.locator('.recovery-upload-button input:not([disabled])');
      const archive = {name: "xroute-uploaded.sbgw", mimeType: "application/octet-stream", buffer: Buffer.alloc(3 * 1024 ** 2)};
      await upload.setInputFiles(archive);
      const uploadedHistory = page.getByRole("row").filter({hasText: /recovery\.upload|Загрузка резервной копии|Upload backup|بارگذاری نسخهٔ پشتیبان|上传备份|上傳備份/});
      await uploadedHistory.waitFor();
      assert.doesNotMatch(await uploadedHistory.innerText(), /recovery\.upload/);
      await upload.setInputFiles(archive);
      const recoveryError = recovery.getByRole("alert");
      await recovery.getByText(duplicateErrors[locale], {exact: false}).waitFor();
      assert.match(await recoveryError.innerText(), /duplicate-test/);
      assert.equal(recoveryUploads, 1, "duplicate preflight must prevent a second PUT");
      await recovery.locator(".recovery-row .button-secondary").first().click();
      const restore = recovery.locator(".recovery-restore-form");
      await restore.locator('input[name="password"]').fill("test-only-password");
      await restore.locator('button[type="submit"]').click();
      await recovery.getByText(passwordErrors[locale], {exact: false}).waitFor();
      assert.match(await recoveryError.innerText(), /password-test/);
      assert.deepEqual(errors, []);
      result.push({locale, fileInputs: true, audit: true, recovery: true, draftChanges: true, cdn: true});
    } finally { await context.close(); }
  }
  return result;
}
