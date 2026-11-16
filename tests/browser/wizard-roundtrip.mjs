import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

// Exercise the rendered setup flow with an isolated API; no router is contacted.
export async function verifyWizardRoundtrip(browser, baseURL) {
  const defaults = JSON.parse(await readFile(new URL("../../internal/controlplane/default-config.json", import.meta.url), "utf8"));
  const receipts = [];
  for (const locale of ["ru", "en", "fa", "zh-Hans", "zh-Hant"]) {
    let config = structuredClone(defaults), revision = 1;
    config.routeros = {...config.routeros, base_url: "https://router.example.test:59443", username_secret_ref: "test-user", password_secret_ref: "test-password", backup_password_secret_ref: "test-backup", import_review: {source_fingerprint: "fixture", networks: [{cidr: "10.90.0.0/24", status: "accepted"}]}};
    config.system.management = {...config.system.management, allowed_source_cidrs: ["10.90.0.0/24"], allowed_ingress_interfaces: ["bridge-test"]};
    config.system.networking = {...config.system.networking, bridge_name: "bridge-container", veth_name: "veth-container", routeros_gateway: "172.31.255.1", container_address: "172.31.255.2/30", tun_address: "172.31.254.1/30", container_dns: "172.31.255.1", addresses_confirmed: true};
    config.dns = {...config.dns, internal_server: "172.31.255.1", address_confirmed: true};
    const disabled = {id: "wizard-internal-1", kind: "internal", enabled: false, cidrs: ["10.99.0.0/24"]};
    const independent = {id: "management-lan-extra", kind: "internal", enabled: true, cidrs: ["10.50.0.0/24"]};
    config.networks = [disabled, independent, {id: "wizard-internal-2", kind: "internal", enabled: true, cidrs: ["10.90.0.0/24"]}, {id: "wizard-internal-3", kind: "internal", enabled: true, cidrs: ["10.91.0.0/24"]}];
    const writes = [], errors = [], unexpected = [];
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
      for (let pass = 0; pass < 2; pass++) {
        await page.locator("main .section-title .button-primary").first().click();
        const modal = page.locator(".setup-modal");
        await modal.locator(".notice-safe .card-actions button").click();
        if (pass === 0) {
          await modal.locator(".wizard-inline-fields input").fill("10.92.0.0/24");
          await modal.locator(".wizard-inline-fields button").click();
        }
        for (let step = 1; step <= 4; step++) await modal.locator(".wizard-actions .button-primary").click();
        await page.waitForFunction(() => document.querySelector(".setup-modal [role=status]") !== null);
        assert.equal(writes.length, pass + 1);
        const saved = writes[pass].networks;
        assert.deepEqual(saved.find(network => network.id === disabled.id), disabled);
        assert.deepEqual(saved.find(network => network.id === independent.id), independent);
        assert.equal(new Set(saved.map(network => network.id)).size, saved.length);
        const internal = saved.filter(network => network.enabled !== false && network.kind === "internal").flatMap(network => network.cidrs).sort();
        assert.deepEqual(internal, ["10.50.0.0/24", "10.90.0.0/24", "10.91.0.0/24", "10.92.0.0/24"]);
        await modal.locator(".wizard-actions .button-primary").click();
        await modal.waitFor({state: "hidden"});
      }
      assert.deepEqual(errors, []);
      assert.deepEqual(unexpected, []);
      receipts.push({locale, saves: writes.length, networksPreserved: true});
    } finally {await context.close();}
  }
  return receipts;
}
