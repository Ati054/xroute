import type { JsonObject } from "./api-client";

export function retainedWizardNetworks(networks: readonly JsonObject[]): JsonObject[] {
  return networks.filter((network) => {
    const id = String(network.id ?? "");
    return id !== "management-lan" &&
      (!id.startsWith("wizard-internal-") || network.enabled === false);
  });
}

export function wizardInternalNetworks(networks: readonly JsonObject[], cidrs: readonly string[]): JsonObject[] {
  const usedIds = new Set(retainedWizardNetworks(networks).map((network) => String(network.id ?? "")));
  let index = 0;
  return [...new Set(cidrs)].map((cidr) => {
    let id: string;
    do { id = `wizard-internal-${++index}`; } while (usedIds.has(id));
    usedIds.add(id);
    return { id, enabled: true, kind: "internal", cidrs: [cidr] };
  });
}

export function initialWizardInternalCidrs(
  networks: readonly JsonObject[],
  reviews: readonly JsonObject[],
): string[] {
  const cidrs = (network: JsonObject): string[] =>
    Array.isArray(network.cidrs)
      ? network.cidrs.filter((cidr): cidr is string => typeof cidr === "string" && cidr !== "")
      : [];
  const configured = networks.filter(
    (network) => network.kind === undefined || network.kind === "internal" || network.kind === "management",
  );
  const existing = new Set(configured.flatMap(cidrs));
  const owned = configured.filter(
    (network) => network.enabled !== false && network.kind !== "management" &&
      typeof network.id === "string" && network.id.startsWith("wizard-internal-"),
  );
  const accepted = reviews.filter(
    (review) => review.status === "accepted" && typeof review.cidr === "string" &&
      review.cidr !== "" && !existing.has(review.cidr),
  ).map((review) => review.cidr as string);
  return [...new Set([...owned.flatMap(cidrs), ...accepted])];
}
