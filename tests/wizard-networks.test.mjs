import assert from "node:assert/strict";
import test from "node:test";
import { initialWizardInternalCidrs, retainedWizardNetworks, wizardInternalNetworks } from "../app/wizard-networks.ts";

test("reopening setup preserves its saved imported and manually added internal networks", () => {
  const networks = [
    { id: "wizard-internal-1", kind: "internal", enabled: true, cidrs: ["192.168.88.0/24"] },
    { id: "wizard-internal-2", kind: "internal", enabled: true, cidrs: ["10.10.0.0/24"] },
    { id: "management-lan", kind: "management", cidrs: ["192.168.88.0/24"] },
  ];
  const reviews = [{ cidr: "192.168.88.0/24", status: "accepted" }];
  const restored = initialWizardInternalCidrs(networks, reviews);
  assert.deepEqual(restored, ["192.168.88.0/24", "10.10.0.0/24"]);
  // Saving uses these values to replace the wizard-owned rows. A second open
  // must yield the same set, including manual additions absent from the import.
  const saved = restored.map((cidr, i) => ({ id: `wizard-internal-${i + 1}`, kind: "internal", enabled: true, cidrs: [cidr] }));
  assert.deepEqual(initialWizardInternalCidrs(saved, reviews), restored);
});

test("new import choices exclude existing independent networks and unaccepted suggestions", () => {
  const networks = [
    { id: "manual-network", kind: "internal", cidrs: ["192.168.88.0/24"] },
    { id: "wizard-internal-disabled", kind: "internal", enabled: false, cidrs: ["10.99.0.0/24"] },
  ];
  const reviews = [
    { cidr: "192.168.88.0/24", status: "accepted" },
    { cidr: "10.20.0.0/24", status: "accepted" },
    { cidr: "10.20.0.0/24", status: "accepted" },
    { cidr: "10.30.0.0/24", status: "pending" },
    { cidr: "10.40.0.0/24", status: "ignored" },
    { cidr: "10.99.0.0/24", status: "accepted" },
  ];
  assert.deepEqual(initialWizardInternalCidrs(networks, reviews), ["10.20.0.0/24"]);
});

test("saving setup retains disabled networks without reusing their IDs", () => {
  const disabled = { id: "wizard-internal-1", kind: "internal", enabled: false, cidrs: ["10.99.0.0/24"] };
  const independent = { id: "management-lan-extra", kind: "internal", cidrs: ["10.50.0.0/24"] };
  const previous = [disabled, independent, { id: "wizard-internal-2", kind: "internal", cidrs: ["10.10.0.0/24"] }, { id: "management-lan", kind: "management", cidrs: ["10.0.0.0/24"] }];
  const saved = [...retainedWizardNetworks(previous), ...wizardInternalNetworks(previous, ["10.10.0.0/24", "10.10.0.0/24"])];
  assert.deepEqual(saved.slice(0, 2), [disabled, independent]);
  assert.equal(new Set(saved.map(network => network.id)).size, saved.length);
  assert.deepEqual(initialWizardInternalCidrs(saved, []), ["10.10.0.0/24"]);
});
