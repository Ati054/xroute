import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const examplePEM = "-----BEGIN CERTIFICATE-----\nVEVTVA==\n-----END CERTIFICATE-----";

// Exercise the rendered setup flow with an isolated API; no router is contacted.
export async function verifyWizardRoundtrip(browser, baseURL) {
  const defaults = JSON.parse(await readFile(new URL("../../internal/controlplane/default-config.json", import.meta.url), "utf8"));
  const receipts = [];
  for (const locale of ["ru", "en", "fa", "zh-Hans", "zh-Hant"]) {
    let config = structuredClone(defaults), revision = 1;
    const panelAccess = {allowed_source_cidrs: ["10.90.0.0/24", "10.93.0.0/24"], allowed_ingress_interfaces: ["ether-test", "vpn-test"]};
    const setupConnection = {base_url: `https://172.31.255.1:${locale === "ru" ? 58443 : 59443}`, username: "test-routeros-user", ssh_port: 2223};
    const importedConnection = locale === "ru"
      ? {base_url: setupConnection.base_url, ssh_port: setupConnection.ssh_port}
      : locale === "zh-Hant" ? null : setupConnection;
    config.routeros = {...config.routeros, base_url: null, username_secret_ref: "test-user", password_secret_ref: "test-password", backup_password_secret_ref: "test-backup", import_review: {source_fingerprint: "fixture", management_ingress_suggestions: ["ether-test"], managed_panel_access: panelAccess, networks: [{cidr: "10.90.0.0/24", status: "accepted"}]}};
    config.routeros.import_review.networks.push({cidr: "10.66.0.0/24", status: "pending"});
    if (locale !== "ru") {
      config.ingress = {...config.ingress, status_hostname: "status.example.test", subscription_hostname: "sub.example.test", subscription_tls_profile_id: "custom-profile"};
    }
    config.transports[0].enabled = true;
    const configuredIngress = locale === "ru"
      ? {...config.ingress, subscription_endpoint_enabled: false}
      : structuredClone(config.ingress);
    const configuredTransports = structuredClone(config.transports);
    const existingProvider = {id: "provider-main", display_name: "Saved provider", enabled: false, refresh_minutes: 90, allowed_locations: ["Finland"], url_secret_ref: "subscriptions/provider-main.url"};
    config.subscriptions = [existingProvider];
    const expectedDns = locale === "fa" ? "10.90.0.53" : "172.31.255.1";
    const legacyTun = locale === "ru" ? "172.31.254.1/30" : "";
    config.system.networking = {...config.system.networking, bridge_name: "bridge-container", veth_name: "veth-container", routeros_gateway: "172.31.255.1", container_address: "172.31.255.2/30", tun_address: legacyTun, container_dns: "", addresses_confirmed: true};
    config.dns = {...config.dns, internal_server: locale === "fa" ? expectedDns : "", address_confirmed: true};
    const disabled = {id: "wizard-internal-1", kind: "internal", enabled: false, cidrs: ["10.99.0.0/24"]};
    const independent = {id: "management-lan-extra", kind: "internal", enabled: true, cidrs: ["10.50.0.0/24"]};
    config.networks = [disabled, independent, {id: "wizard-internal-2", kind: "internal", enabled: true, cidrs: ["10.90.0.0/24"]}, {id: "wizard-internal-3", kind: "internal", enabled: true, cidrs: ["10.91.0.0/24"]}];
    const writes = [], imports = [], credentials = [], errors = [], unexpected = [];
    const context = await browser.newContext({viewport: {width: 1440, height: 1000}});
    await context.addInitScript(value => localStorage.setItem("sb-gateway:locale", value), locale);
    await context.route("**/*", route => new URL(route.request().url()).origin === new URL(baseURL).origin ? route.continue() : route.abort());
    const envelope = () => ({config, revision: String(revision), has_active_configuration: false, pending_config_change_count: writes.length});
    const overview = () => ({status: {state: "unconfigured"}, build: {container_version: "test"}, routeros: {}});
    await context.route("**/api/v1/**", async route => {
      const req = route.request(), path = new URL(req.url()).pathname.slice(7);
      let json;
      if (path === "/auth/session") json = {authenticated: true, username: "Test administrator", csrf_token: "test-token"};
      else if (path === "/bootstrap-state") json = {overview: overview(), draft: envelope(), status: {state: "unconfigured"}};
      else if (path === "/overview") json = overview();
      else if (path === "/status") json = {state: "unconfigured", selector_health: {}, subscriptions: {}, acme: {}};
      else if (path === "/client-telemetry") json = {clients: []};
      else if (path === "/routeros/discover") json = {routeros: {live_available: true}};
      else if (path === "/routeros/container-status") json = {container: {running: true, restart_count: 0}};
      else if (path === "/routeros/import" && req.method() === "POST") {
        imports.push(req.postDataJSON());
        json = {...config.routeros.import_review, network_inventory: config.routeros.import_review.networks,
          setup_connection: importedConnection};
      }
      else if (path === "/routeros/credentials" && req.method() === "POST") {
        credentials.push(req.postDataJSON());
        config.routeros = {...config.routeros, base_url: credentials.at(-1).base_url, ssh_port: credentials.at(-1).ssh_port};
        json = {ok: true, credentials_provisioned: true};
      }
      else if (path === "/drafts/plan") json = {valid: true, apply_mode: "runtime_fast", check: {errors: []}};
      else if (path === "/drafts/current") {
        if (req.method() === "PUT") {
          assert.equal(req.headers()["x-csrf-token"], "test-token");
          config = req.postDataJSON().config; revision++; writes.push(structuredClone(config));
        }
        json = envelope();
      } else {unexpected.push(`${req.method()} ${path}`); await route.fulfill({status: 404, json: {error: {message: "Unexpected endpoint"}}}); return;}
      await route.fulfill({json: structuredClone(json)});
    });
    const page = await context.newPage();
    page.on("pageerror", error => errors.push(String(error)));
    try {
      await page.goto(baseURL, {waitUntil: "networkidle"});
      assert.equal(await page.locator(".overview-transport-chip").count(), 0);
      for (let pass = 0; pass < 2; pass++) {
        await page.setViewportSize(pass === 0 ? {width: 1366, height: 768} : {width: 390, height: 844});
        await page.locator("main .section-title .button-primary").first().click();
        const modal = page.locator(".setup-modal");
        await modal.waitFor({state: "visible"});
        assert.equal(await modal.locator(".wizard-steps li").count(), 3);
        const geometry = await modal.evaluate(element => {
          const actions = element.querySelector(".wizard-actions").getBoundingClientRect();
          const body = element.querySelector(".wizard-body").getBoundingClientRect();
          const certificateInput = element.querySelector('input[name="routeros-ca"]');
          const certificate = certificateInput.getBoundingClientRect();
          const picker = getComputedStyle(certificateInput, "::file-selector-button");
          return {bottom: actions.bottom, top: actions.top, bodyBottom: body.bottom,
            height: window.innerHeight, certificateFits: certificate.left >= body.left && certificate.right <= body.right,
            pickerSpacing: picker.marginInlineEnd, pickerBorder: picker.borderInlineStartWidth,
            manualExpanded: element.querySelector(".wizard-rest-help").open};
        });
        assert.ok(geometry.bottom <= geometry.height && geometry.top >= geometry.bodyBottom - 1, JSON.stringify(geometry));
        assert.equal(geometry.certificateFits, true);
        assert.equal(geometry.pickerSpacing, "10px");
        assert.equal(geometry.pickerBorder, "1px");
        assert.equal(geometry.manualExpanded, false);
        assert.equal(await modal.locator("textarea").count(), 0);
        if (pass === 0) {
          const password = modal.locator('input[type="password"]');
          const certificate = modal.locator('input[name="routeros-ca"]');
          const continueButton = modal.locator(".wizard-actions .button-primary");
          await modal.locator(".drop-zone input").setInputFiles({name: "routeros-export.rsc", mimeType: "text/plain", buffer: Buffer.from("# test RouterOS export")});
          if (locale === "ru" || locale === "zh-Hant") {
            await modal.locator(".wizard-rest-help[open]").waitFor();
            assert.equal(await modal.locator('input[type="url"]').inputValue(), importedConnection?.base_url ?? "");
            assert.equal(await modal.locator('input[autocomplete="username"]').inputValue(), "");
            if (!importedConnection) {
              await modal.locator('input[type="url"]').fill(setupConnection.base_url);
              await modal.locator('input[type="number"]').fill(String(setupConnection.ssh_port));
              await modal.locator('input[autocomplete="username"]').fill(setupConnection.username);
            }
          }
          await password.fill("example-routeros-password");
          await certificate.setInputFiles({name: "invalid.crt", mimeType: "text/plain", buffer: Buffer.from("invalid file")});
          await continueButton.click();
          await modal.getByRole("alert").waitFor();
          assert.equal(await password.inputValue(), "example-routeros-password");
          assert.equal(imports.length, 1);
          assert.equal(credentials.length, 0);
          await certificate.setInputFiles({name: "routeros-ca.crt", mimeType: "application/x-pem-file", buffer: Buffer.from(examplePEM + "\n")});
          await continueButton.click();
          if (locale === "ru") {
            await modal.getByRole("alert").filter({hasText: "Укажите имя REST-пользователя RouterOS."}).waitFor();
            assert.equal(await password.inputValue(), "example-routeros-password");
            await modal.locator('input[autocomplete="username"]').fill(setupConnection.username);
            await continueButton.click();
          }
        } else {
          await modal.locator(".wizard-actions .button-primary").click();
        }
        await modal.locator(".wizard-advanced").first().waitFor();
        await page.waitForFunction(() => document.querySelector(".setup-modal .wizard-body")?.scrollTop === 0);
        const access = modal.locator(".wizard-advanced").first();
        assert.equal(await access.evaluate(details => details.open), false);
        assert.equal(await access.locator('input[list="management-ingress-options"]').inputValue(), "ether-test");
        const containerNetwork = modal.locator(".wizard-advanced").nth(1);
        assert.equal(await containerNetwork.evaluate(details => details.open), false);
        assert.equal(await containerNetwork.locator("input").count(), 4);
        const internalNetworks = modal.locator(".wizard-advanced").last();
        if (pass === 0) {
          assert.equal(await internalNetworks.evaluate(details => details.open), true);
          const discovered = internalNetworks.locator(".network-review-item").filter({hasText: "10.66.0.0/24"});
          const bulk = internalNetworks.locator(".network-review-bulk");
          await discovered.waitFor();
          assert.ok((await discovered.boundingBox()).y < (await bulk.boundingBox()).y);
          await bulk.getByRole("button").last().click();
          assert.equal(await internalNetworks.evaluate(details => details.open), true);
          assert.ok(await internalNetworks.locator(".wizard-inline-fields input").isVisible());
        } else {
          assert.equal(await internalNetworks.evaluate(details => details.open), false);
          await internalNetworks.locator("summary").click();
        }
        await modal.locator(".network-review-toolbar select").selectOption("all");
        const networkBounds = await modal.locator(".network-review-item").evaluateAll(elements => elements.map(element => {
          const box = element.getBoundingClientRect();
          return Array.from(element.querySelectorAll("button")).every(button => {
            const rect = button.getBoundingClientRect();
            return rect.left >= box.left && rect.right <= box.right;
          });
        }));
        assert.ok(networkBounds.length > 0 && networkBounds.every(Boolean), JSON.stringify({locale, pass, networkBounds}));
        if (pass === 0) {
          await modal.locator(".wizard-inline-fields input").fill("10.92.0.0/24");
          await modal.locator(".wizard-inline-fields button").click();
        }
        await modal.locator(".wizard-actions .button-primary").click();
        const review = modal.locator("dl + .notice");
        const assertReviewWidth = async () => {
          assert.ok(await review.evaluate(element => (
            element.querySelector("div").getBoundingClientRect().width >= element.clientWidth * 0.65
          )), `wizard review was squeezed in ${locale}, pass ${pass}`);
        };
        await assertReviewWidth();
        await modal.locator(".wizard-actions .button-primary").click();
        await page.waitForFunction(() => document.querySelector(".setup-modal [role=status]") !== null);
        await assertReviewWidth();
        assert.equal(writes.length, pass + 1);
        const saved = writes[pass].networks;
        assert.deepEqual(saved.find(network => network.id === disabled.id), disabled);
        assert.deepEqual(saved.find(network => network.id === independent.id), independent);
        assert.equal(new Set(saved.map(network => network.id)).size, saved.length);
        assert.deepEqual(writes[pass].system.management.allowed_ingress_interfaces, ["ether-test", "vpn-test"]);
        assert.deepEqual(writes[pass].system.management.allowed_source_cidrs, ["10.90.0.0/24", "10.93.0.0/24"]);
        assert.deepEqual(writes[pass].ingress, configuredIngress);
        assert.deepEqual(writes[pass].transports, configuredTransports);
        assert.deepEqual(writes[pass].subscriptions, [existingProvider]);
        assert.equal(writes[pass].system.networking.tun_address, legacyTun);
        assert.equal(writes[pass].system.networking.container_dns, expectedDns);
        assert.equal(writes[pass].dns.internal_server, expectedDns);
        const internal = saved.filter(network => network.enabled !== false && network.kind === "internal").flatMap(network => network.cidrs).sort();
        assert.deepEqual(internal, ["10.50.0.0/24", "10.90.0.0/24", "10.91.0.0/24", "10.92.0.0/24"]);
        await modal.locator(".wizard-actions .button-primary").click();
        await modal.waitFor({state: "hidden"});
        const apply = page.getByRole("dialog");
        await apply.waitFor({state: "visible"});
        await page.keyboard.press("Escape");
        await apply.waitFor({state: "hidden"});
      }
      assert.deepEqual(errors, []);
      assert.deepEqual(unexpected, []);
      assert.deepEqual(imports, [{source_name: "routeros-export.rsc", export_text: "# test RouterOS export"}]);
      assert.equal(credentials.length, 1);
      assert.equal(credentials[0].ca_certificate, examplePEM);
      assert.equal(credentials[0].base_url, setupConnection.base_url);
      assert.equal(credentials[0].username, setupConnection.username);
      assert.equal(credentials[0].ssh_port, setupConnection.ssh_port);
      receipts.push({locale, saves: writes.length, networksPreserved: true});
    } finally {await context.close();}
  }
  return receipts;
}
