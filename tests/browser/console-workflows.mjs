import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

// Isolated UI contract: no RouterOS access, credentials or external network.
export async function verifyConsoleWorkflows(browser, baseURL, { screenshotDirectory } = {}) {
  const defaults = JSON.parse(await readFile(new URL("../../internal/controlplane/default-config.json", import.meta.url), "utf8"));
  const receipts = [];
  for (const locale of ["ru", "en"]) {
    const config = structuredClone(defaults);
    // Exercise both fresh and upgraded installations with the same UI bundle.
    config.system.object_comment_prefix = locale === "en" ? "XROUTE" : "SB-GATEWAY";
    config.transports.forEach(transport => { transport.enabled = false; });
    config.transports.find(transport => transport.id === "xhttp-reality").enabled = true;
    config.system.deployment_ready = true;
    config.system.management.allowed_ingress_interfaces = ["bridge-test"];
    config.system.management.allowed_source_cidrs = ["10.90.0.0/24"];
    config.system.networking.addresses_confirmed = true;
    config.system.networking.bridge_name = "bridge-container";
    config.system.networking.veth_name = "veth-test";
    config.system.networking.container_address = "172.31.255.2/30";
    config.system.networking.wireguard_egress_enabled = true;
    config.system.networking.wireguard_egress_exits = [{id: "wg-exit", interface: "wg-exit", enabled: true}];
    config.routeros.import_review.networks = [{cidr: "10.66.0.0/24", status: "ignored"}];
    config.networks = [
      {id: "management-lan", kind: "management", enabled: true, cidrs: ["10.90.0.0/24"]},
      {id: "wizard-internal-1", kind: "internal", enabled: true, cidrs: ["10.88.0.0/24"]},
      {id: "wizard-internal-2", kind: "internal", enabled: true, cidrs: ["172.31.255.0/30"]},
    ];
    config.policies = [{id: "test-route", display_name: "Test route", enabled: true, mode: "priority", traffic_mode: "vless_with_wan_exceptions", outbounds: [], locations: [], selection_order: []}];
    config.local_clients = ["lan", "wireguard", "ppp-vpn"].map((kind, index) => ({
      id: `test-${kind}`, name: `Test ${kind}`, enabled: true, source_kind: kind,
      source_cidrs: [kind === "lan" ? "10.100.100.101/32" : `10.90.${index}.2/32`], source_peer_refs: kind === "wireguard" ? ["*TEST"] : [],
      policy_id: "test-route", container_outage: "direct",
    }));
    config.remote_users = [{id: "test-remote", display_name: "Test remote", enabled: true, role: "trusted-full", policy_id: "test-route", client_tun_address: "10.99.0.2/30", subscription_format: "xray"}];
    config.reverse_vless_exits = [{id: "test-reverse", display_name: "Test reverse", enabled: true, transport_ids: ["xhttp-reality"], transport_id: "xhttp-reality"}];
    let draft = structuredClone(config), active = structuredClone(config), revision = 1, activeRevision = 1;
    let failNextSave = false, delayNextSave = 0, failNextApply = false;
    let holdStatus = false;
    const heldStatus = [];
    const releaseStatus = () => {holdStatus = false; heldStatus.splice(0).forEach(resolve => resolve());};
    const writes = [], applies = [], unexpected = [], errors = [];
    let firstLaunch = true;
    const context = await browser.newContext({viewport: {width: 1440, height: 1000}});
    await context.route("**/*", async route => {
      if (new URL(route.request().url()).origin !== new URL(baseURL).origin) {
        unexpected.push("External network request blocked");
        await route.abort();
      } else await route.continue();
    });
    await context.addInitScript(value => localStorage.setItem("sb-gateway:locale", value), locale);
    const page = await context.newPage();
    page.on("pageerror", error => errors.push(String(error)));
    const envelope = () => ({config: draft, revision: String(revision), has_active_configuration: true, pending_config_change_count: revision === activeRevision ? 0 : 1});
    const overview = () => ({build: {container_version: "test", active_core_name: "Xray"}, active_revision: String(activeRevision), draft_revision: String(revision), pending_config_change_count: revision === activeRevision ? 0 : 1, status: {state: firstLaunch ? "unconfigured" : "healthy"}, applied_transports: firstLaunch ? [] : active.transports, routeros: {}});
    await context.route("**/api/v1/**", async route => {
      const request = route.request(), pathname = new URL(request.url()).pathname.slice(7), method = request.method();
      const body = request.postDataJSON();
      let response;
      if (pathname === "/auth/session") response = {authenticated: true, username: "Test administrator", csrf_token: "test-token"};
      else if (pathname === "/bootstrap-state") response = {overview: overview(), draft: envelope(), status: {state: firstLaunch ? "unconfigured" : "healthy"}};
      else if (pathname === "/overview") response = overview();
      else if (pathname === "/status") {
        if (holdStatus) await new Promise(resolve => heldStatus.push(resolve));
        response = {state: firstLaunch ? "unconfigured" : "healthy", active_revision: String(activeRevision), last_apply: {previous_revision: "test-predecessor"}, selector_health: {}, subscriptions: {}, acme: {}};
      }
      else if (pathname === "/client-telemetry") response = {sampled_at: "test", clients: []};
      else if (pathname === "/routeros/discover") response = {routeros: {
        live_available: true, wireguard_egress_inventory_complete: true, wireguard_egress_candidates: [{interface: "wg-exit", eligible: true}],
        client_source_inventory_complete: {lan: true, wireguard: true, "ppp-vpn": true},
        lan_source_inventory_complete: true, lan_source_cidrs: ["10.90.0.0/24"],
        access_destination_inventory_complete: true,
        access_destinations: ["10.90.0.0/24", "10.77.0.0/24", "10.66.0.0/24", "172.31.255.0/30"].map(cidr => ({cidr})),
        client_sources: Object.fromEntries(config.local_clients.map(client => [client.source_kind, [{name: client.name, cidrs: client.source_cidrs, source_cidrs: client.source_cidrs, peer_ref: client.source_peer_refs[0], valid: true}]])),
      }};
      else if (pathname === "/routeros/container-status") response = {container: {running: true, restart_count: 0}};
      else if (pathname === "/routeros/container/memory") response = {container: {inventory_available: true, memory_high: "224MiB", memory_max: "256MiB"}};
      else if (pathname === "/network/tls-probe" && method === "POST") response = {endpoint_results: [
        {address: "192.0.2.10", ok: true, tls_version: "TLSv1.3", alpn: "h2"},
        {address: "2001:db8::10", ok: false, error_code: "tls_probe_handshake_failed", error_message: "TLS handshake failed for [2001:db8::10]:443."},
      ]};
      else if (pathname === "/drafts/current" && method === "GET") response = envelope();
      else if (pathname === "/drafts/current" && method === "PUT") {
        if (failNextSave) {
          failNextSave = false;
          await route.fulfill({status: 503, json: {error: {code: "test_save_failed", message: "Test save failed"}}});
          return;
        }
        if (delayNextSave) {const delay = delayNextSave; delayNextSave = 0; await new Promise(resolve => setTimeout(resolve, delay));}
        assert.equal(request.headers()["x-csrf-token"], "test-token");
        draft = structuredClone(body.config); revision++; writes.push(structuredClone(draft)); response = envelope();
      } else if (pathname === "/drafts/plan") response = {valid: true, apply_mode: "runtime_fast", check: {errors: []}};
      else if (pathname === "/drafts/check") response = {valid: true, errors: []};
      else if (pathname === "/drafts/apply") {
        if (failNextApply) {
          failNextApply = false;
          await route.fulfill({status: 422, json: {error: {code: "test_apply_rejected", message: "Test Apply rejected"}}});
          return;
        }
        active = structuredClone(draft); activeRevision = revision; applies.push(active); response = {ok: true, revision: String(activeRevision)};
      } else if (pathname === "/drafts/reset") {draft = structuredClone(active); revision = activeRevision; response = {ok: true};}
      else if (pathname === "/remote-users/test-remote" && method === "PUT") {
        draft.remote_users = draft.remote_users.map(item => item.id === "test-remote" ? {...item, ...body} : item); revision++; response = {ok: true};
      } else if (pathname === "/remote-users/test-remote/subscription-link") {
        response = {url: "https://gateway.example.test/test-subscription", active: true};
      } else if (pathname === "/lifecycle") response = {};
      else if (pathname === "/lifecycle/uninstall" && method === "GET") response = {objects: []};
      else if (pathname === "/recovery/backups") response = {backups: []};
      else if (pathname.startsWith("/logs/")) response = {lines: []};
      else {unexpected.push(`${method} ${pathname}`); await route.fulfill({status: 404, json: {error: {message: "Unexpected test endpoint"}}}); return;}
      await route.fulfill({json: structuredClone(response)});
    });
    try {
      await page.goto(baseURL, {waitUntil: "networkidle"});
      const nav = page.locator(".sidebar nav button");
      const navigate = async index => {
        await nav.nth(index).click();
        const screen = ["overview", "clients", "routing", "connections", "operations", "settings"][index];
        await page.waitForURL(url => url.hash === `#/${screen}`);
      };
      await navigate(0);
      assert.equal(await page.locator(".overview-transport-chip").count(), 0);
      await page.setViewportSize({width: 1366, height: 768});
      const headers = await page.locator(".overview-device-table th").evaluateAll(elements => elements.map(element => {
        const range = document.createRange();
        range.selectNodeContents(element);
        const cell = element.getBoundingClientRect();
        return {fits: Array.from(range.getClientRects()).every(rect => rect.left >= cell.left && rect.right <= cell.right), label: element.textContent};
      }));
      assert.ok(headers.length > 0);
      assert.ok(headers.every(header => header.fits), JSON.stringify(headers));
      await page.getByRole("button", {name: locale === "ru" ? "Настроить шлюз" : "Set up gateway", exact: true}).click();
      const wizard = page.locator(".setup-modal");
      assert.equal(await wizard.locator('input[autocomplete="username"]').getAttribute("placeholder"), locale === "en" ? "xroute-api" : "sb-gateway-api");
      await page.keyboard.press("Escape");
      await wizard.waitFor({state: "hidden"});
      firstLaunch = false;
      await page.reload({waitUntil: "networkidle"});
      await page.setViewportSize({width: 1440, height: 900});
      const clientAddress = page.locator('.overview-device-identity small[title="10.100.100.101/32"]');
      assert.equal(await clientAddress.innerText(), "10.100.100.101/32");
      assert.ok(await clientAddress.evaluate(label => label.scrollWidth <= label.clientWidth));
      const ingress = page.locator(".overview-transport-chip");
      assert.equal(await ingress.count(), 1);
      assert.ok(await ingress.evaluate(chip => chip.offsetWidth === chip.parentElement.clientWidth));
      await navigate(2);
      const ping = page.locator(".routing-behavior-settings input[type=checkbox]").filter({visible: true}).first();
      await ping.waitFor();
      const originalPolicies = structuredClone(draft.policies);
      const dns = page.locator(".dns-path-settings select");
      assert.equal(await dns.count(), 2);
      await dns.nth(0).selectOption("quad9:doh");
      await dns.nth(1).selectOption("google:dot");
      await ping.uncheck();
      await navigate(0);
      assert.deepEqual(draft.dns.direct_resolver, {provider: "quad9", protocol: "doh"});
      assert.deepEqual(draft.dns.vpn_resolver, {provider: "google", protocol: "dot"});
      assert.equal(draft.system.networking.allow_wan_ping, false);
      assert.deepEqual(draft.policies, originalPolicies);
      await navigate(2);
      await page.locator(".routing-policy-section .button-primary").first().click();
      const routeEditor = page.locator(".policy-editor-modal");
      const routeBody = routeEditor.locator(".policy-editor-body");
      for (const [group, option] of [["WireGuard", "WG · wg-exit"], ["Reverse VLESS", "Test reverse"]]) {
        const expander = routeEditor.getByRole("button", {name: new RegExp(`${group}$`)});
        if (await expander.getAttribute("aria-expanded") === "false") await expander.click();
        const exit = routeEditor.getByRole("checkbox", {name: new RegExp(option)});
        await exit.check();
        assert.equal(await exit.isChecked(), true);
      }
      await routeEditor.getByRole("button", {name: "RU", exact: true}).click();
      await routeEditor.locator(".service-pack-geoip-note").evaluate(note => note.scrollIntoView({block: "end"}));
      assert.ok(await routeBody.evaluate(body => {
        const note = body.querySelector(".service-pack-geoip-note").getBoundingClientRect();
        const viewport = body.getBoundingClientRect();
        const footer = body.nextElementSibling.getBoundingClientRect();
        return note.top >= viewport.top && note.bottom + 16 <= footer.top;
      }));
      await routeEditor.locator(".modal-close").click();
      assert.equal(await ping.isChecked(), false);
      const reverseName = page.locator(".reverse-vless-settings input").first();
      const longName = `Reverse ${locale} complete name`;
      await reverseName.fill(longName);
      delayNextSave = 250;
      await navigate(1);
      assert.equal(draft.reverse_vless_exits[0].display_name, longName);
      for (const [index, kind] of ["lan", "wireguard", "ppp-vpn"].entries()) {
        const row = page.locator("tbody tr").filter({hasText: `Test ${kind}`});
        await row.locator("button").last().click();
        const dialog = page.getByRole("dialog");
        await dialog.locator('input[name="name"]').fill(`Edited ${kind} ${locale}`);
        await dialog.locator('select[name="container_outage"]').selectOption("lan_only");
        await dialog.locator(".modal-actions .button-primary").click();
        await dialog.locator(".modal-success .button-primary").click();
        assert.equal(draft.local_clients[index].name, `Edited ${kind} ${locale}`);
        assert.equal(draft.local_clients[index].container_outage, "lan_only");
        assert.deepEqual(draft.local_clients[index].source_cidrs, config.local_clients[index].source_cidrs);
      }
      await page.getByRole("tab").nth(1).click();
      for (const format of ["auto", "array", "xray", "singbox", "mihomo", "links"]) {
        await page.locator(".user-card").first().locator(".user-card-actions button").last().click();
        const dialog = page.getByRole("dialog");
        await dialog.locator('input[name="name"]').fill(`Edited remote ${locale} ${format}`);
        await dialog.locator('select[name="role"]').selectOption(format === "auto" ? "trusted-limited" : "internet-only");
        if (format === "auto") {
          const cidrs = dialog.locator('input[name="allowed_cidrs"]');
          await cidrs.filter({visible: true}).first().waitFor();
          await dialog.locator('input[name="allowed_cidrs"][value="10.77.0.0/24"]').waitFor();
          assert.deepEqual((await cidrs.evaluateAll(items => items.map(item => item.value))).sort(), ["10.77.0.0/24", "10.88.0.0/24", "10.90.0.0/24"]);
          assert.doesNotMatch(await dialog.locator(".lan-network-list").innerText(), /wizard-internal|management-lan/);
          await dialog.locator('input[name="allowed_cidrs"][value="10.88.0.0/24"]').check();
        }
        await dialog.locator('select[name="subscription_format"]').selectOption(format);
        await dialog.locator('input[name="client_tun_address"]').fill("10.99.0.9/30");
        await dialog.locator(".modal-actions .button-primary").click();
        await dialog.locator(".modal-success .button-primary").click();
        assert.equal(draft.remote_users[0].display_name, `Edited remote ${locale} ${format}`);
        assert.equal(draft.remote_users[0].subscription_format, format);
        if (format === "auto") assert.deepEqual(draft.remote_users[0].allowed_cidrs, ["10.88.0.0/24"]);
        assert.equal(draft.remote_users[0].client_tun_address, "10.99.0.9/30");
      }
      await navigate(3);
      const connectionOrder = await page.locator(".subscriptions-grid").evaluate(element => {
        const inputs = document.querySelector(".transport-groups");
        return !!inputs && !!(element.compareDocumentPosition(inputs) & Node.DOCUMENT_POSITION_FOLLOWING);
      });
      assert.equal(connectionOrder, true);
      const reality = page.locator(".transport-card").filter({has: page.getByRole("heading", {name: "VLESS + XHTTP + REALITY", exact: true})});
      await reality.getByRole("button").click();
      const transportEditor = page.getByRole("dialog");
      await transportEditor.locator('input[name="server_name"]').fill("target.example.test");
      await transportEditor.locator('input[name="handshake_server"]').fill("target.example.test");
      await transportEditor.locator(".reality-target-check button").click();
      const failedAddress = transportEditor.locator(".reality-probe-endpoints .is-warning");
      await failedAddress.waitFor();
      assert.match(await failedAddress.innerText(), /2001:db8::10/);
      assert.match(await failedAddress.innerText(), locale === "ru" ? /TLS-рукопожатие не прошло/ : /The TLS handshake failed/);
      assert.doesNotMatch(await failedAddress.innerText(), /TLS handshake failed for/);
      await transportEditor.locator(".modal-close").click();
      await navigate(4);
      await page.locator(".versions-card .panel-heading button").click();
      const rollback = page.getByRole("dialog");
      const confirmation = rollback.locator(".checkbox-field span");
      assert.ok(await confirmation.evaluate(label => (
        label.querySelector("small").getBoundingClientRect().top >= label.querySelector("strong").getBoundingClientRect().bottom
      )));
      await page.keyboard.press("Escape");
      await rollback.waitFor({state: "hidden"});
      await navigate(5);
      const storage = page.locator('.settings-card:has(input[placeholder="usb1/xroute"])');
      assert.ok(await storage.locator(".container-memory-fields").count());
      await page.locator(".panel-address-fields input[type=number]").fill("18443");
      const settingsSaved = page.waitForResponse(response => response.url().endsWith("/api/v1/drafts/current") && response.request().method() === "GET");
      await page.locator(".settings-save-actions .button-primary").click();
      await settingsSaved;
      assert.equal(draft.system.management.routeros_panel_port, 18443);
      assert.equal(draft.dns.vpn_resolver.protocol, "dot");
      await navigate(2);
      failNextSave = true;
      await ping.check();
      await nav.nth(0).click();
      await page.getByText("Test save failed", {exact: false}).first().waitFor();
      assert.equal(new URL(page.url()).hash, "#/routing");
      assert.equal(await ping.isChecked(), true);
      assert.equal(draft.system.networking.allow_wan_ping, false);
      assert.equal(applies.length, 0);
      await navigate(0);
      assert.equal(draft.system.networking.allow_wan_ping, true);
      await page.locator(".draft-actions > .button-primary").click();
      const apply = page.getByRole("dialog");
      await apply.locator(".checkbox-field input").check();
      failNextApply = true;
      await apply.locator(".modal-actions .button-primary").click();
      await apply.getByText("Test Apply rejected", {exact: false}).waitFor();
      assert.equal(applies.length, 0);
      assert.notDeepEqual(active, draft);
      holdStatus = true;
      await apply.locator(".modal-actions .button-primary").click();
      await apply.locator(".modal-success").waitFor({state: "visible", timeout: 2500});
      assert.ok(heldStatus.length > 0, "slow post-Apply status read was not exercised");
      releaseStatus();
      await apply.locator(".modal-success .button-primary").click();
      assert.equal(applies.length, 1);
      assert.deepEqual(active, draft);
      assert.equal(active.dns.vpn_resolver.protocol, "dot");
      assert.equal(active.reverse_vless_exits[0].display_name, longName);
      assert.equal(active.system.management.routeros_panel_port, 18443);
      assert.equal(active.remote_users[0].subscription_format, "links");
      await page.reload({waitUntil: "networkidle"});
      await navigate(2);
      assert.equal(await ping.isChecked(), true);
      await page.emulateMedia({reducedMotion: "reduce"});
      await page.evaluate(() => document.activeElement?.blur());
      if (screenshotDirectory) await page.screenshot({path: `${screenshotDirectory}/console-${locale}-desktop.png`, fullPage: true, animations: "disabled"});
      await page.setViewportSize({width: 390, height: 844});
      await page.waitForFunction(() => document.querySelector(".sidebar").getBoundingClientRect().right <= 0);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
      if (screenshotDirectory) await page.screenshot({path: `${screenshotDirectory}/console-${locale}-mobile.png`, fullPage: true, animations: "disabled"});
      assert.deepEqual(errors, []);
      assert.deepEqual(unexpected, []);
      receipts.push({locale, writes: writes.length, applies: applies.length, devices: 3, remote_formats: 6, settings_default_saved: true, saved_after_navigation: true, failure_preserves_input: true, rejected_apply_not_reported_as_success: true, slow_status_does_not_delay_apply: true});
    } finally {releaseStatus(); await context.close();}
  }
  return receipts;
}
