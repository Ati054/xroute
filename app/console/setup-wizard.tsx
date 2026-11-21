"use client";

import { JsonObject, getCurrentDraft, importRouterOsExport, provisionRouterOsCredentials, saveCurrentDraft } from "../api-client";
import { useLanguage } from "../i18n";
import { initialWizardInternalCidrs, retainedWizardNetworks, wizardInternalNetworks } from "../wizard-networks";
import { NetworkReviewStatus, StatusPill, asObject, asObjectList, asStringList, asText, errorMessage, importedNetworkKind, isExplicitRouterHttpsOrigin, isIpv4Cidr, networkReviewStatus, routerImportWarningText, withDeploymentReadiness } from "./shared";
import { useEffect, useRef, useState } from "react";

export function SetupWizard({
  onClose,
  onDraftChanged,
  onApply,
  config: initialConfig,
}: {
  onClose: () => void;
  onDraftChanged: () => Promise<void>;
  onApply: () => void;
  config: JsonObject;
}) {
  const { locale, tr } = useLanguage();
  const initialSystem = asObject(initialConfig.system);
  const initialManagement = asObject(initialSystem.management);
  const initialNetworking = asObject(initialSystem.networking);
  const initialDns = asObject(initialConfig.dns);
  const initialRouteros = asObject(initialConfig.routeros);
  const routerosConnectionSaved =
    typeof initialRouteros.base_url === "string" &&
    typeof initialRouteros.username_secret_ref === "string" &&
    typeof initialRouteros.password_secret_ref === "string" &&
    typeof initialRouteros.backup_password_secret_ref === "string";
  const initialImportReview = asObject(initialRouteros.import_review);
  const initialManagedPanelAccess = asObject(initialImportReview.managed_panel_access);
  const [installedPanelAccess, setInstalledPanelAccess] = useState(initialManagedPanelAccess);
  const initialTopologySuggestions = asObject(
    initialImportReview.topology_suggestions,
  );
  const initialManagementNetworkSuggestions = asStringList(
    initialImportReview.management_network_suggestions,
  );
  const initialReviewNetworks = asObjectList(initialImportReview.networks);
  const configuredManagementCidr =
    asStringList(initialManagement.allowed_source_cidrs)[0] ??
    asStringList(initialManagedPanelAccess.allowed_source_cidrs)[0] ?? "";
  const configuredManagementInterface =
    asStringList(initialManagement.allowed_ingress_interfaces)[0] ??
    asStringList(initialManagedPanelAccess.allowed_ingress_interfaces)[0] ?? "";
  const configuredBridgeName = asText(initialNetworking.bridge_name, "");
  const configuredVethName = asText(initialNetworking.veth_name, "");
  const configuredRouterGateway = asText(
    initialNetworking.routeros_gateway,
    "",
  );
  const configuredContainerAddress = asText(
    initialNetworking.container_address,
    "",
  );
  const configuredInternalDns =
    asText(initialDns.internal_server, "") ||
    asText(initialNetworking.container_dns, "");
  const initialOfflinePrefilledFields = [
    !configuredManagementCidr &&
    initialManagementNetworkSuggestions.length > 0
      ? "management_cidr"
      : "",
    !configuredBridgeName &&
    asText(initialTopologySuggestions.bridge_name, "")
      ? "bridge_name"
      : "",
    !configuredVethName &&
    asText(initialTopologySuggestions.veth_name, "")
      ? "veth_name"
      : "",
    !configuredRouterGateway &&
    asText(initialTopologySuggestions.routeros_gateway, "")
      ? "routeros_gateway"
      : "",
    !configuredContainerAddress &&
    asText(initialTopologySuggestions.container_address, "")
      ? "container_address"
      : "",
  ].filter(Boolean);
  const hasInitialOfflineAddressPrefill = initialOfflinePrefilledFields.some(
    (field) =>
      [
        "management_cidr",
        "routeros_gateway",
        "container_address",
      ].includes(field),
  );
  const [step, setStep] = useState(0);
  const [exportFile, setExportFile] = useState<File | null>(null);
  const [managementCidr, setManagementCidr] = useState(
    configuredManagementCidr ||
      initialManagementNetworkSuggestions[0] ||
      "",
  );
  const [managementIngressInterface, setManagementIngressInterface] = useState(
    configuredManagementInterface ||
      (asStringList(initialImportReview.management_ingress_suggestions).length === 1
        ? asStringList(initialImportReview.management_ingress_suggestions)[0] : ""),
  );
  const [confirmManagementAccess, setConfirmManagementAccess] = useState(
    !configuredManagementCidr || !configuredManagementInterface,
  );
  const [managementIngressSuggestions, setManagementIngressSuggestions] =
    useState<string[]>(
      asStringList(initialImportReview.management_ingress_suggestions),
    );
  const [managementNetworkSuggestions, setManagementNetworkSuggestions] =
    useState<string[]>(initialManagementNetworkSuggestions);
  const [topologySuggestions, setTopologySuggestions] = useState<JsonObject>(
    initialTopologySuggestions,
  );
  const [offlinePrefilledFields, setOfflinePrefilledFields] = useState<string[]>(
    initialOfflinePrefilledFields,
  );
  const [bridgeName, setBridgeName] = useState(
    configuredBridgeName ||
      asText(initialTopologySuggestions.bridge_name, ""),
  );
  const [vethName, setVethName] = useState(
    configuredVethName || asText(initialTopologySuggestions.veth_name, ""),
  );
  const [routerGateway, setRouterGateway] = useState(
    configuredRouterGateway ||
      asText(initialTopologySuggestions.routeros_gateway, ""),
  );
  const [containerAddress, setContainerAddress] = useState(
    configuredContainerAddress ||
      asText(initialTopologySuggestions.container_address, ""),
  );
  const [showContainerNetwork, setShowContainerNetwork] = useState(
    !bridgeName || !vethName || !routerGateway || !containerAddress,
  );
  const [internalDnsOverride] = useState(
    configuredInternalDns === routerGateway ? "" : configuredInternalDns,
  );
  const internalDns = internalDnsOverride.trim() || routerGateway.trim();
  const [internalNetworks, setInternalNetworks] = useState<string[]>(
    initialWizardInternalCidrs(asObjectList(initialConfig.networks), initialReviewNetworks),
  );
  const [showInternalNetworks, setShowInternalNetworks] = useState<boolean | null>(null);
  const [discoveredNetworks, setDiscoveredNetworks] = useState<string[]>(
    initialReviewNetworks
      .map((item) => asText(item.cidr, ""))
      .filter(Boolean),
  );
  const [networkDecisions, setNetworkDecisions] = useState<
    Record<string, NetworkReviewStatus>
  >(
    Object.fromEntries(
      initialReviewNetworks
        .map((item) => [
          asText(item.cidr, ""),
          networkReviewStatus(item.status),
        ])
        .filter(([cidr]) => Boolean(cidr)),
    ),
  );
  const [importWarnings, setImportWarnings] = useState<string[]>(
    asStringList(initialImportReview.posture_warnings),
  );
  const [importAnalyzed, setImportAnalyzed] = useState(
    typeof initialImportReview.source_fingerprint === "string",
  );
  const [manualNetworkCidr, setManualNetworkCidr] = useState("");
  const [networkQuery, setNetworkQuery] = useState("");
  const [networkStatusFilter, setNetworkStatusFilter] = useState<
    "all" | NetworkReviewStatus
  >("pending");
  const [wizardBusy, setWizardBusy] = useState(false);
  const [wizardMessage, setWizardMessage] = useState("");
  const [wizardSaved, setWizardSaved] = useState(false);
  const routerBaseUrlRef = useRef<HTMLInputElement>(null);
  const routerSshPortRef = useRef<HTMLInputElement>(null);
  const routerUsernameRef = useRef<HTMLInputElement>(null);
  const routerPasswordRef = useRef<HTMLInputElement>(null);
  const routerCaRef = useRef<HTMLInputElement>(null);
  const routerConnectionDetails = useRef<HTMLDetailsElement>(null);
  const importedExport = useRef<{file: File; result: JsonObject} | null>(null);
  const wizardBodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (wizardBodyRef.current) wizardBodyRef.current.scrollTop = 0;
  }, [step]);

  useEffect(() => {
    if (!hasInitialOfflineAddressPrefill) return;
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const draft = await getCurrentDraft<{ config?: JsonObject }>();
          const config = asObject(draft.config);
          const system = asObject(config.system);
          const networking = asObject(system.networking);
          const dns = asObject(config.dns);
          if (
            networking.addresses_confirmed === false &&
            dns.address_confirmed === false
          ) {
            return;
          }
          await saveCurrentDraft({
            config: withDeploymentReadiness(
              {
                ...config,
                system: {
                  ...system,
                  networking: {
                    ...networking,
                    addresses_confirmed: false,
                  },
                },
                dns: {
                  ...dns,
                  address_confirmed: false,
                },
              },
              false,
            ),
          });
          await onDraftChanged();
        } catch (error) {
          setWizardMessage(
            tr("Не удалось сбросить подтверждение адресов: {value1}", { value1: errorMessage(error) }),
          );
        }
      })();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [hasInitialOfflineAddressPrefill, onDraftChanged, tr]);

  const steps = [
    "MikroTik",
    tr("Сети"),
    tr("Проверка"),
  ];
  const manualNetworkChoices = internalNetworks.filter(
    (cidr) => !discoveredNetworks.includes(cidr),
  );
  const pendingNetworks = discoveredNetworks.filter(
    (cidr) => networkReviewStatus(networkDecisions[cidr]) === "pending",
  );
  const acceptedSuggestionCount = discoveredNetworks.filter(
    (cidr) => networkReviewStatus(networkDecisions[cidr]) === "accepted",
  ).length;
  const ignoredSuggestionCount = discoveredNetworks.filter(
    (cidr) => networkReviewStatus(networkDecisions[cidr]) === "ignored",
  ).length;
  const visibleDiscoveredNetworks = discoveredNetworks.filter((cidr) => {
    const query = networkQuery.trim().toLowerCase();
    const status = networkReviewStatus(networkDecisions[cidr]);
    return (
      (!query ||
        cidr.toLowerCase().includes(query) ||
        tr(importedNetworkKind(cidr, managementNetworkSuggestions))
          .toLowerCase()
          .includes(query)) &&
      (networkStatusFilter === "all" || status === networkStatusFilter)
    );
  });
  const isOfflinePrefilled = (
    field: string,
    value: string,
    candidate: unknown,
  ) =>
    offlinePrefilledFields.includes(field) &&
    Boolean(value) &&
    value === asText(candidate, "");
  const activeOfflinePrefillCount = [
    isOfflinePrefilled(
      "management_cidr",
      managementCidr,
      managementNetworkSuggestions[0],
    ),
    isOfflinePrefilled(
      "bridge_name",
      bridgeName,
      topologySuggestions.bridge_name,
    ),
    isOfflinePrefilled(
      "veth_name",
      vethName,
      topologySuggestions.veth_name,
    ),
    isOfflinePrefilled(
      "routeros_gateway",
      routerGateway,
      topologySuggestions.routeros_gateway,
    ),
    isOfflinePrefilled(
      "container_address",
      containerAddress,
      topologySuggestions.container_address,
    ),
  ].filter(Boolean).length;

  function decideNetwork(cidr: string, status: "accepted" | "ignored") {
    setShowInternalNetworks(true);
    setNetworkDecisions((current) => ({ ...current, [cidr]: status }));
    setInternalNetworks((current) => {
      if (status === "accepted") {
        return current.includes(cidr) ? current : [...current, cidr];
      }
      return current.filter((item) => item !== cidr);
    });
  }

  function decideVisibleNetworks(status: "accepted" | "ignored") {
    if (!visibleDiscoveredNetworks.length) return;
    setShowInternalNetworks(true);
    const visible = new Set(visibleDiscoveredNetworks);
    setNetworkDecisions((current) => ({
      ...current,
      ...Object.fromEntries(
        visibleDiscoveredNetworks.map((cidr) => [cidr, status]),
      ),
    }));
    setInternalNetworks((current) =>
      status === "accepted"
        ? [...new Set([...current, ...visibleDiscoveredNetworks])]
        : current.filter((cidr) => !visible.has(cidr)),
    );
  }

  async function analyzeExport(file: File) {
    const previous = asObject(importedExport.current?.result.setup_connection);
    const result = importedExport.current?.file === file
      ? importedExport.current.result
      : asObject(await importRouterOsExport(file));
    const connection = asObject(result.setup_connection);
    const updateHint = (input: HTMLInputElement | null, key: string) => {
      if (input && (!input.value || input.value === String(previous[key] ?? ""))) {
        input.value = String(connection[key] ?? "");
      }
    };
    updateHint(routerBaseUrlRef.current, "base_url");
    updateHint(routerUsernameRef.current, "username");
    updateHint(routerSshPortRef.current, "ssh_port");
    if (routerConnectionDetails.current && (!connection.base_url || !connection.username || !connection.ssh_port)) {
      routerConnectionDetails.current.open = true;
    }
    importedExport.current = {file, result};
    return result;
  }

  async function selectExport(file: File) {
    setExportFile(file);
    setWizardMessage("");
    setWizardBusy(true);
    try {
      await analyzeExport(file);
    } catch (error) {
      setWizardMessage(errorMessage(error));
    } finally {
      setWizardBusy(false);
    }
  }

  async function advance() {
    setWizardMessage("");
    if (step === 0) {
      if (!exportFile) {
        if (importAnalyzed && routerosConnectionSaved) {
          setStep(1);
          return;
        }
        setWizardMessage(
          tr("Выберите обычный экспорт RouterOS без флага show-sensitive."),
        );
        return;
      }
      const routerPassword = routerPasswordRef.current?.value ?? "";
      const caFile = routerCaRef.current?.files?.[0];
      if (routerPassword.length < 16) {
        setWizardMessage(tr("Пароль RouterOS должен содержать не менее 16 символов."));
        routerPasswordRef.current?.focus();
        return;
      }
      setWizardBusy(true);
      try {
        const caCertificate = caFile ? (await caFile.text()).trim() : undefined;
        if (caCertificate !== undefined && !caCertificate.includes("-----BEGIN CERTIFICATE-----")) {
          setWizardMessage(tr("Выберите файл сертификата CA в формате PEM."));
          routerCaRef.current?.focus();
          return;
        }
        const resultObject = await analyzeExport(exportFile);
        const connection = asObject(resultObject.setup_connection);
        const baseUrl = routerBaseUrlRef.current?.value.trim() ?? "";
        const routerUsername = routerUsernameRef.current?.value.trim() ?? "";
        const routerSshPort = Number(routerSshPortRef.current?.value || (connection.base_url ? 0 : 22));
        const invalidRequiredField = !isExplicitRouterHttpsOrigin(baseUrl)
          ? {input: routerBaseUrlRef.current, message: tr("Укажите HTTPS-адрес RouterOS с портом службы www-ssl.")}
          : !routerUsername
            ? {input: routerUsernameRef.current, message: tr("Укажите имя REST-пользователя RouterOS.")}
            : !Number.isInteger(routerSshPort) || routerSshPort < 1 || routerSshPort > 65535
              ? {input: routerSshPortRef.current, message: tr("Укажите действующий SSH-порт RouterOS.")}
              : null;
        if (invalidRequiredField) {
          if (routerConnectionDetails.current) routerConnectionDetails.current.open = true;
          setWizardMessage(invalidRequiredField.message);
          invalidRequiredField.input?.focus();
          return;
        }
        const discovered = asObject(resultObject.discovered);
        const importedInventory = asObjectList(resultObject.network_inventory);
        const fallbackSuggestions = asStringList(
          resultObject.network_suggestions,
        );
        const inventoryRows = importedInventory.length
          ? importedInventory
          : fallbackSuggestions.map((cidr) => ({
              cidr,
              status: "pending",
            }));
        const suggestedNetworks = inventoryRows
          .map((item) => asText(item.cidr, ""))
          .filter(Boolean);
        const importedDecisions = Object.fromEntries(
          inventoryRows
            .map((item) => [
              asText(item.cidr, ""),
              networkReviewStatus(item.status),
            ])
            .filter(([cidr]) => Boolean(cidr)),
        );
        const suggestedIngressInterfaces = asStringList(
          resultObject.management_ingress_suggestions ??
            discovered.management_ingress_suggestions,
        );
        const suggestedManagementNetworks = asStringList(
          resultObject.management_network_suggestions ??
            discovered.management_network_suggestions,
        );
        const installedAccess = asObject(resultObject.managed_panel_access);
        const installedSources = asStringList(installedAccess.allowed_source_cidrs);
        const installedInterfaces = asStringList(installedAccess.allowed_ingress_interfaces);
        setInstalledPanelAccess(installedAccess);
        if (installedSources.length && installedInterfaces.length) {
          if (!configuredManagementCidr) setManagementCidr(installedSources[0]);
          if (!configuredManagementInterface) setManagementIngressInterface(installedInterfaces[0]);
          setConfirmManagementAccess(false);
        }
        const suggestedTopology = asObject(
          resultObject.topology_suggestions ??
            discovered.topology_suggestions,
        );
        const newlyPrefilled: string[] = [];
        const prefill = (
          field: string,
          current: string,
          value: unknown,
          setter: (value: string) => void,
        ) => {
          const candidate = asText(value, "").trim();
          if (!current.trim() && candidate) {
            setter(candidate);
            newlyPrefilled.push(field);
          }
        };
        prefill(
          "management_cidr",
          managementCidr,
          installedSources[0] ?? suggestedManagementNetworks[0],
          setManagementCidr,
        );
        if (suggestedIngressInterfaces.length === 1) {
          prefill("management_ingress_interface", managementIngressInterface,
            installedInterfaces[0] ?? suggestedIngressInterfaces[0], setManagementIngressInterface);
        }
        prefill(
          "bridge_name",
          bridgeName,
          suggestedTopology.bridge_name,
          setBridgeName,
        );
        prefill(
          "veth_name",
          vethName,
          suggestedTopology.veth_name,
          setVethName,
        );
        prefill(
          "routeros_gateway",
          routerGateway,
          suggestedTopology.routeros_gateway,
          setRouterGateway,
        );
        prefill(
          "container_address",
          containerAddress,
          suggestedTopology.container_address,
          setContainerAddress,
        );
        setInternalNetworks((current) => {
          const retained = current.filter(
            (cidr) => !discoveredNetworks.includes(cidr),
          );
          const newlyAccepted = suggestedNetworks.filter(
            (cidr) =>
              networkReviewStatus(importedDecisions[cidr]) === "accepted" &&
              !retainedWizardNetworks(asObjectList(initialConfig.networks)).some(
                (network) => asStringList(network.cidrs).includes(cidr),
              ),
          );
          return [...new Set([...retained, ...newlyAccepted])];
        });
        setDiscoveredNetworks(suggestedNetworks);
        setShowInternalNetworks(null);
        setNetworkDecisions(importedDecisions);
        setManagementIngressSuggestions(suggestedIngressInterfaces);
        setManagementNetworkSuggestions(suggestedManagementNetworks);
        setTopologySuggestions(suggestedTopology);
        setShowContainerNetwork(![
          bridgeName || asText(suggestedTopology.bridge_name, ""),
          vethName || asText(suggestedTopology.veth_name, ""),
          routerGateway || asText(suggestedTopology.routeros_gateway, ""),
          containerAddress || asText(suggestedTopology.container_address, ""),
        ].every(Boolean));
        setOfflinePrefilledFields((current) => [
          ...new Set([...current, ...newlyPrefilled]),
        ]);
        setImportWarnings(asStringList(resultObject.warnings));
        setImportAnalyzed(true);
        if (
          newlyPrefilled.some((field) =>
            [
              "management_cidr",
              "routeros_gateway",
              "container_address",
            ].includes(field),
          )
        ) {
          const importedDraft = await getCurrentDraft<{
            config?: JsonObject;
          }>();
          const importedConfig = asObject(importedDraft.config);
          const importedSystem = asObject(importedConfig.system);
          const importedNetworking = asObject(importedSystem.networking);
          const importedDns = asObject(importedConfig.dns);
          await saveCurrentDraft({
            config: withDeploymentReadiness(
              {
                ...importedConfig,
                system: {
                  ...importedSystem,
                  networking: {
                    ...importedNetworking,
                    addresses_confirmed: false,
                  },
                },
                dns: {
                  ...importedDns,
                  address_confirmed: false,
                },
              },
              false,
            ),
          });
        }
        await provisionRouterOsCredentials({
          base_url: baseUrl,
          username: routerUsername,
          password: routerPassword,
          ssh_port: routerSshPort,
          ca_certificate: caCertificate,
        });
        if (routerPasswordRef.current) routerPasswordRef.current.value = "";
        await onDraftChanged();
        setStep(1);
      } catch (error) {
        setWizardMessage(errorMessage(error));
        await onDraftChanged();
      } finally {
        setWizardBusy(false);
      }
      return;
    }
    if (step === 1) {
      if (!managementIngressInterface.trim()) {
        setWizardMessage(
          tr("Выберите предложенный bridge или введите точное имя фактического management ingress-интерфейса."),
        );
        return;
      }
      if (!bridgeName.trim() || !vethName.trim() || bridgeName === vethName) {
        setWizardMessage(
          tr("Проверьте имена нового container bridge и veth: оба обязательны и должны различаться."),
        );
        return;
      }
      if (pendingNetworks.length) {
        setWizardMessage(
          tr("Для {value1} сетевых подсказок ещё не выбрано «принять» или «игнорировать». К следующему шагу перейти нельзя.", { value1: pendingNetworks.length }),
        );
        return;
      }
      if (!isIpv4Cidr(managementCidr, false)) {
        setWizardMessage(
          tr("Укажите фактический IPv4 management CIDR с префиксом, например /24 или /32."),
        );
        return;
      }
      if (!routerGateway.trim() || routerGateway.includes("/")) {
        setWizardMessage(
          tr("Укажите фактический IPv4 RouterOS на container bridge без префикса."),
        );
        return;
      }
      if (!containerAddress.trim() || !containerAddress.includes("/")) {
        setWizardMessage(
          tr("Укажите фактический IPv4/CIDR контейнера на veth — он должен совпадать с SB_CONTAINER_ADDRESS."),
        );
        return;
      }
    }
    if (step < steps.length - 1) {
      setStep(step + 1);
      return;
    }

    setWizardBusy(true);
    try {
      const draft = await getCurrentDraft<{ config?: JsonObject }>();
      const config = asObject(draft.config);
      const system = asObject(config.system);
      const management = asObject(system.management);
      const networking = asObject(system.networking);
      const routeros = asObject(config.routeros);
      const importReview = asObject(routeros.import_review);
      const ingress = asObject(config.ingress);
      const dns = asObject(config.dns);

      const existingNetworks = asObjectList(config.networks);
      const retainedNetworks = retainedWizardNetworks(existingNetworks);
      const wizardNetworks = wizardInternalNetworks(existingNetworks, internalNetworks);
      const savedManagementSources = asStringList(management.allowed_source_cidrs);
      const savedManagementInterfaces = asStringList(management.allowed_ingress_interfaces);
      const managementSources = Array.from(new Set([
        managementCidr,
        ...(savedManagementSources.length ? savedManagementSources : asStringList(installedPanelAccess.allowed_source_cidrs)).slice(1),
      ]));
      const managementInterfaces = Array.from(new Set([
        managementIngressInterface,
        ...(savedManagementInterfaces.length ? savedManagementInterfaces : asStringList(installedPanelAccess.allowed_ingress_interfaces)).slice(1),
      ]));
      const nextNetworks = [
        ...retainedNetworks,
        ...wizardNetworks,
        {
          id: "management-lan",
          enabled: true,
          kind: "management",
          cidrs: managementSources,
        },
      ];

      const unconfiguredSubscription =
        !asText(ingress.subscription_hostname, "").trim() &&
        !asText(ingress.subscription_deployment_id, "") &&
        (!ingress.subscription_endpoint_mode || ingress.subscription_endpoint_mode === "separate");
      await saveCurrentDraft({
        config: withDeploymentReadiness({
          ...config,
          routeros: {
            ...routeros,
            channel_policy: "informational",
            preserve_channel: true,
            automatic_update: false,
            import_review: {
              ...importReview,
              networks: discoveredNetworks.map((cidr) => ({
                cidr,
                status: networkReviewStatus(networkDecisions[cidr]),
              })),
            },
          },
          system: {
            ...system,
            management: {
              ...management,
              allowed_source_cidrs: managementSources,
              allowed_ingress_interfaces: managementInterfaces,
            },
            networking: {
              ...networking,
              bridge_name: bridgeName,
              veth_name: vethName,
              routeros_gateway: routerGateway,
              container_address: containerAddress,
              container_dns: internalDns,
              addresses_confirmed: true,
            },
          },
          dns: {
            ...dns,
            internal_server: internalDns,
            address_confirmed: true,
          },
          ingress: unconfiguredSubscription ? {
            ...ingress,
            subscription_endpoint_enabled: false,
          } : ingress,
          networks: nextNetworks,
          tls_profiles: asObjectList(config.tls_profiles).filter((profile) => !(
            profile.id === "cdn-default" && profile.certificate_source === "manual" &&
            !profile.certificate_secret_ref && !profile.private_key_secret_ref
          )),
        }, false),
      });
      await onDraftChanged();
      setWizardSaved(true);
      setWizardMessage(
        tr("Черновик сохранён. Теперь запустите проверку и изучите точный план изменений."),
      );
    } catch (error) {
      setWizardMessage(errorMessage(error));
      await onDraftChanged();
    } finally {
      setWizardBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <section
        className="modal setup-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="setup-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <button className="modal-close" onClick={onClose} aria-label={tr("Закрыть")}>
          ×
        </button>
        <div className="setup-heading">
          <div>
            <h2 id="setup-title">{tr("Настройка XRoute")}</h2>
            <p>
               {tr("Укажите параметры RouterOS и сети. Изменения применятся после проверки и вашего подтверждения.")} </p>
          </div>
          <StatusPill tone="info">
             {tr("Шаг")} {step + 1}  {tr("из")} {steps.length}
          </StatusPill>
        </div>

        <ol className="wizard-steps" aria-label={tr("Этапы настройки")}>
          {steps.map((label, index) => (
            <li
              className={index === step ? "wizard-current" : ""}
              key={label}
            >
              <span>{index < step ? "✓" : index + 1}</span>
              <small>{label}</small>
            </li>
          ))}
        </ol>

        <div className="wizard-body" ref={wizardBodyRef}>
          {step === 0 ? (
            <div>
              <h3>{tr("Импорт конфигурации RouterOS")}</h3>
              <p>
                {tr("Выберите готовый экспорт RouterOS или создайте его командой")} <code dir="ltr">/export file=routeros-export</code>.
              </p>
              <label className="drop-zone">
                <input
                  type="file"
                  accept=".rsc,.txt"
                  disabled={wizardBusy}
                  onChange={(event) => {
                    const file = event.currentTarget.files?.[0];
                    if (file) void selectExport(file);
                  }}
                />
                <span className="drop-icon">⇧</span>
                <strong>{exportFile?.name || tr("Выберите экспорт MikroTik")}</strong>
              </label>
              <details className="wizard-rest-help" ref={routerConnectionDetails}>
                <summary>{tr("Ручная настройка REST")}</summary>
                <div className="wizard-advanced-fields form-grid">
                <label className="field">
                  {tr("Адрес RouterOS REST (HTTPS)")}
                  <input
                    ref={routerBaseUrlRef}
                    type="url"
                    pattern="https://.+:[0-9]+/?"
                    defaultValue={asText(initialRouteros.base_url, "")}
                    placeholder="https://172.31.255.1:59443"
                    required
                  />
                </label>
                <label className="field">
                   {tr("SSH-порт RouterOS")} <input
                    ref={routerSshPortRef}
                    type="number"
                    min={1}
                    max={65535}
                    defaultValue={
                      routerosConnectionSaved && typeof initialRouteros.ssh_port === "number"
                        ? initialRouteros.ssh_port
                        : ""
                    }
                    placeholder="22"
                    required
                  />
                </label>
                <label className="field">
                   {tr("Отдельный REST-пользователь")} <input
                    ref={routerUsernameRef}
                    placeholder={initialSystem.object_comment_prefix === "XROUTE" ? "xroute-api" : "sb-gateway-api"}
                    autoComplete="username"
                    required
                  />
                </label>
                </div>
              </details>
              <div className="form-grid">
                <label className="field">
                   {tr("Пароль RouterOS · минимум 16 символов")} <input
                    ref={routerPasswordRef}
                    type="password"
                    minLength={16}
                    autoComplete="new-password"
                    required
                  />
                </label>
                <label className="field form-span">
                  {tr("Сертификат CA для RouterOS (PEM)")}
                  <input
                    ref={routerCaRef}
                    name="routeros-ca"
                    className="wizard-certificate-file"
                    type="file"
                    accept=".pem,.crt,.cer,application/x-pem-file"
                  />
                  <small>
                    {tr("После setup.rsc выберите файл routeros-ca.crt. Если панель уже доверяет сертификату RouterOS, оставьте поле пустым.")}
                  </small>
                </label>
              </div>
              {importWarnings.length > 0 ? (
                <aside className="notice notice-danger">
                  <span className="notice-icon">!</span>
                  <div>
                    <strong>{tr("Требуется проверка RouterOS")}</strong>
                    <p>
                       {tr("При применении панель подключится к RouterOS и проверит версию, пакеты и настройки сети.")} </p>
                    <ul>
                      {importWarnings.map((warning) => (
                        <li key={warning}>
                          {routerImportWarningText(warning, locale)}
                        </li>
                      ))}
                    </ul>
                  </div>
                </aside>
              ) : null}
              {importAnalyzed && routerosConnectionSaved && !exportFile ? (
                <aside className="notice notice-safe">
                  <span className="notice-icon">✓</span>
                  <div>
                    <strong>{tr("Подключение к RouterOS сохранено")}</strong>
                  </div>
                </aside>
              ) : null}
            </div>
          ) : null}

          {step === 1 ? (
            <div>
              <h3>{tr("Внутренние сети")}</h3>
              <p>
                 {tr("Добавьте только свои сети LAN и VPN. Для них XRoute сохранит обычные маршруты RouterOS.")} </p>
              {activeOfflinePrefillCount ? (
                <aside className="notice notice-neutral">
                  <span className="notice-icon" aria-hidden="true">i</span>
                  <div>
                    <strong>
                       {tr("Предложенные параметры:")} {activeOfflinePrefillCount}
                    </strong>
                    <p>
                       {tr("Параметры предложены на основе экспорта. Проверьте адреса и имена интерфейсов; при применении панель сверит их с RouterOS.")} </p>
                  </div>
                </aside>
              ) : null}
              <details className="wizard-advanced" open={confirmManagementAccess}>
                <summary>{tr("Доступ к панели")}</summary>
                <div className="wizard-advanced-fields">
              <label className="field">
                 {tr("Интерфейс доступа к панели")} <input
                  list="management-ingress-options"
                  value={managementIngressInterface}
                  onChange={(event) =>
                    setManagementIngressInterface(event.target.value)
                  }
                  placeholder={tr("Укажите интерфейс LAN или VPN")}
                  required
                />
                <datalist id="management-ingress-options">
                  {managementIngressSuggestions.map((interfaceName) => (
                    <option value={interfaceName} key={interfaceName}>
                      {interfaceName}
                    </option>
                  ))}
                </datalist>
                <small>
                   {tr("Выберите LAN-интерфейс или мост для доступа к панели. Если его нет в списке, введите имя вручную. WAN использовать нельзя.")} </small>
              </label>
              {!managementIngressInterface && !managementIngressSuggestions.length ? (
                <aside className="notice notice-danger">
                  <span className="notice-icon">!</span>
                  <div>
                    <strong>{tr("Выберите интерфейс доступа")}</strong>
                    <p>
                       {tr("Укажите порт, мост, VLAN или VPN-интерфейс, через который вы открываете панель.")} </p>
                  </div>
                </aside>
              ) : null}
              {managementIngressInterface &&
              !managementIngressSuggestions.includes(
                managementIngressInterface,
              ) ? (
                <aside className="notice notice-neutral">
                  <span className="notice-icon">i</span>
                  <div>
                    <strong>{tr("Интерфейс введён вручную")}</strong>
                    <p>
                       {tr("Панель проверит введённый интерфейс перед применением. WAN-интерфейс здесь использовать нельзя.")} </p>
                  </div>
                </aside>
              ) : null}
              <label className="field">
                {tr("Подсеть доступа к панели (CIDR)")}
                <input
                  value={managementCidr}
                  onChange={(event) => setManagementCidr(event.target.value)}
                  placeholder={tr("Подсеть доступа к панели")}
                />
                <small>
                  {isOfflinePrefilled(
                    "management_cidr",
                    managementCidr,
                    managementNetworkSuggestions[0],
                  )
                    ? tr("Проверьте подсеть для доступа к панели.")
                    : tr("Отсюда будет доступна панель на порту 9443.")}
                </small>
              </label>
                </div>
              </details>
              <details className="wizard-advanced" open={showContainerNetwork}>
                <summary>{tr("Сеть контейнера")}</summary>
                <div className="wizard-advanced-fields form-grid">
                <label className="field">
                   {tr("Мост контейнера")} <input
                    value={bridgeName}
                    onChange={(event) => setBridgeName(event.target.value)}
                    placeholder={tr("Введите свободное имя bridge")}
                  />
                  <small>
                    {isOfflinePrefilled(
                      "bridge_name",
                      bridgeName,
                      topologySuggestions.bridge_name,
                    )
                      ? tr("Перед применением панель проверит это имя интерфейса.")
                      : tr("Имя не должно совпадать с существующим интерфейсом RouterOS.")}
                  </small>
                </label>
                <label className="field">
                   {tr("Имя нового veth")} <input
                    value={vethName}
                    onChange={(event) => setVethName(event.target.value)}
                    placeholder={tr("Введите свободное имя veth")}
                  />
                  <small>
                    {isOfflinePrefilled(
                      "veth_name",
                      vethName,
                      topologySuggestions.veth_name,
                    )
                      ? tr("Перед применением панель проверит это имя интерфейса.")
                      : tr("Имя будет использовано только для контейнера XRoute.")}
                  </small>
                </label>
                <label className="field">
                   {tr("Адрес RouterOS в сети контейнера")} <input
                    value={routerGateway}
                    onChange={(event) => setRouterGateway(event.target.value)}
                    placeholder={tr("Адрес RouterOS, выбранный при установке")}
                  />
                  <small>
                    {isOfflinePrefilled(
                      "routeros_gateway",
                      routerGateway,
                      topologySuggestions.routeros_gateway,
                    )
                      ? tr("Предложенный адрес. Проверьте, что он подходит вашей сети.")
                      : tr("Укажите адрес RouterOS в сети контейнера.")}
                  </small>
                </label>
                <label className="field">
                  {tr("IPv4-адрес контейнера (CIDR)")}
                  <input
                    value={containerAddress}
                    onChange={(event) => setContainerAddress(event.target.value)}
                    placeholder={tr("Адрес контейнера с префиксом подсети")}
                  />
                  <small>
                    {isOfflinePrefilled(
                      "container_address",
                      containerAddress,
                      topologySuggestions.container_address,
                    )
                      ? tr("Проверьте подсеть и убедитесь, что предложенный адрес свободен.")
                      : tr("Укажите свободный адрес в той же подсети, что и RouterOS.")}
                  </small>
                </label>
                </div>
              </details>
              <details
                className="wizard-advanced"
                open={showInternalNetworks ?? (pendingNetworks.length > 0)}
                onToggle={(event) => setShowInternalNetworks(event.currentTarget.open)}
              >
                <summary>{tr("Внутренние сети")} · {internalNetworks.length}</summary>
                <div className="wizard-advanced-fields">
              {!discoveredNetworks.length && !manualNetworkChoices.length ? (
                <aside className="notice notice-neutral">
                  <span className="notice-icon">i</span>
                  <div>
                    <strong>{tr("Сети не найдены в файле")}</strong>
                    <p>
                       {tr("Добавьте свои сети LAN и VPN вручную.")} </p>
                  </div>
                </aside>
              ) : null}
              {discoveredNetworks.length ? (
                <div className="network-review-toolbar">
                  <label className="search-field">
                    <span className="sr-only">{tr("Найти импортированную сеть")}</span>
                    <input
                      value={networkQuery}
                      onChange={(event) => setNetworkQuery(event.target.value)}
                      placeholder={tr("CIDR или тип сети")}
                    />
                  </label>
                  <label className="field compact-field">
                     {tr("Показывать")} <select
                      value={networkStatusFilter}
                      onChange={(event) =>
                        setNetworkStatusFilter(
                          event.target.value as "all" | NetworkReviewStatus,
                        )
                      }
                    >
                      <option value="pending">{tr("Требуют решения")}</option>
                      <option value="accepted">{tr("Принятые")}</option>
                      <option value="ignored">{tr("Игнорируемые")}</option>
                      <option value="all">{tr("Все")}</option>
                    </select>
                  </label>
                  <small>
                    {tr("Трафик к принятым подсетям идёт по маршрутам RouterOS.")}
                  </small>
                </div>
              ) : null}
              <div className="discovered-networks">
                {visibleDiscoveredNetworks.map((cidr) => {
                  const decision = networkReviewStatus(networkDecisions[cidr]);
                  return (
                    <div className="checkbox-field network-review-item" key={cidr}>
                      <span aria-hidden="true">
                        {decision === "accepted"
                          ? "✓"
                          : decision === "ignored"
                            ? "×"
                            : "?"}
                      </span>
                      <span>
                        <strong>{cidr}</strong>
                        <small>
                          {tr(importedNetworkKind(
                            cidr,
                            managementNetworkSuggestions,
                          ))}  {tr("· подсказка из офлайн-экспорта")} </small>
                      </span>
                      <div
                        className="card-actions"
                        role="group"
                        aria-label={tr("Тип сети {value1}", { value1: cidr })}
                      >
                        <button
                          className={`button ${
                            decision === "accepted"
                              ? "button-primary"
                              : "button-secondary"
                          }`}
                          type="button"
                          aria-pressed={decision === "accepted"}
                          onClick={() => decideNetwork(cidr, "accepted")}
                        >
                           {tr("Принять как внутреннюю")} </button>
                        <button
                          className={`button ${
                            decision === "ignored"
                              ? "button-primary"
                              : "button-tertiary"
                          }`}
                          type="button"
                          aria-pressed={decision === "ignored"}
                          onClick={() => decideNetwork(cidr, "ignored")}
                        >
                           {tr("Игнорировать")} </button>
                      </div>
                    </div>
                  );
                })}
                {!visibleDiscoveredNetworks.length && discoveredNetworks.length ? (
                  <div className="empty-result">
                     {tr("По выбранному фильтру сетей нет.")} </div>
                ) : null}
                {manualNetworkChoices.map((cidr) => (
                  <label key={`manual-${cidr}`}>
                    <input
                      type="checkbox"
                      checked
                      onChange={() =>
                        setInternalNetworks((current) =>
                          current.filter((item) => item !== cidr),
                        )
                      }
                    />
                    <span>
                      <strong>{cidr}</strong>
                      <small>{tr("Добавлено вручную · снимите флажок, чтобы удалить")}</small>
                    </span>
                  </label>
                ))}
              </div>
              {discoveredNetworks.length ? (
                <div className="card-actions network-review-bulk">
                  <button
                    className="button button-secondary"
                    type="button"
                    disabled={!visibleDiscoveredNetworks.length}
                    onClick={() => decideVisibleNetworks("accepted")}
                  >
                     {tr("Принять видимые ·")} {visibleDiscoveredNetworks.length}
                  </button>
                  <button
                    className="button button-tertiary"
                    type="button"
                    disabled={!visibleDiscoveredNetworks.length}
                    onClick={() => decideVisibleNetworks("ignored")}
                  >
                     {tr("Игнорировать видимые ·")} {visibleDiscoveredNetworks.length}
                  </button>
                </div>
              ) : null}
              {discoveredNetworks.length ? (
                <aside
                  className={
                    pendingNetworks.length
                      ? "notice notice-danger"
                      : "notice notice-safe"
                  }
                >
                  <span className="notice-icon">
                    {pendingNetworks.length ? "!" : "✓"}
                  </span>
                  <div>
                    <strong>
                      {pendingNetworks.length
                        ? tr("Ожидают решения: {value1}", { value1: pendingNetworks.length })
                        : tr("Все подсказки классифицированы")}
                    </strong>
                    <p>
                       {tr("Принято:")} {acceptedSuggestionCount}{tr("; проигнорировано:")}{" "}
                      {ignoredSuggestionCount} </p>
                  </div>
                </aside>
              ) : null}
              <div className="wizard-inline-fields">
                <label className="field">
                   {tr("Добавить внутренний CIDR вручную")} <input
                    value={manualNetworkCidr}
                    onChange={(event) => setManualNetworkCidr(event.target.value)}
                    placeholder={tr("Введите точный IPv4/IPv6 CIDR")}
                  />
                </label>
                <button
                  className="button button-secondary"
                  type="button"
                  disabled={
                    !manualNetworkCidr.trim() ||
                    !manualNetworkCidr.includes("/")
                  }
                  onClick={() => {
                    const cidr = manualNetworkCidr.trim();
                    setInternalNetworks((current) =>
                      current.includes(cidr) ? current : [...current, cidr],
                    );
                    setManualNetworkCidr("");
                  }}
                >
                   {tr("Добавить CIDR")} </button>
              </div>
                </div>
              </details>
            </div>
          ) : null}

          {step === 2 ? (
            <div>
              <h3>{tr("Черновик готов к проверке")}</h3>
              <p>
                 {tr("Сохраните черновик. Перед применением панель покажет список изменений и выполнит проверки.")} </p>
              <dl className="wizard-summary">
                <div>
                  <dt>RouterOS</dt>
                  <dd>{tr("Проверим при применении")}</dd>
                </div>
                <div>
                  <dt>{tr("Доступ к панели")}</dt>
                  <dd>
                    {managementCidr} · {managementIngressInterface} →
                    HTTPS 9443
                  </dd>
                </div>
                <div>
                  <dt>{tr("Контейнер")}</dt>
                  <dd>
                    {bridgeName} · {vethName} · RouterOS{" "}
                    {routerGateway} · {containerAddress} · DNS {internalDns}
                  </dd>
                </div>
                <div>
                  <dt>{tr("Внутренние сети")}</dt>
                  <dd>{tr("Подсети: {value1}", { value1: internalNetworks.length })}</dd>
                </div>
                <div>
                  <dt>{tr("Подсказки из .rsc")}</dt>
                  <dd>
                     {tr("принято")} {acceptedSuggestionCount}  {tr("· проигнорировано")}{" "}
                    {ignoredSuggestionCount}  {tr("· ожидает")} {pendingNetworks.length}
                  </dd>
                </div>
                <div>
                  <dt>{tr("Отказ контейнера")}</dt>
                  <dd>{tr("Локальные → WAN; удалённые → блокировка")}</dd>
                </div>
              </dl>
              <aside className="notice notice-neutral">
                <span className="notice-icon" aria-hidden="true">i</span>
                <div>
                  <strong>{tr("Проверка перед применением")}</strong>
                  <p>
                     {tr("Перед применением панель проверит RouterOS, Xray и веб-сервер. При изменении правил роутера создаст резервную копию и включит автоматический откат.")} </p>
                </div>
              </aside>
            </div>
          ) : null}
        </div>

        {wizardMessage ? (
          <div
            className={`inline-result ${
              wizardSaved ? "" : "inline-result-error"
            }`}
            role={wizardSaved ? "status" : "alert"}
          >
            <span>{wizardSaved ? "✓" : "!"}</span>
            {wizardMessage}
          </div>
        ) : null}

        <div className="wizard-actions">
          <button
            className="button button-tertiary"
            onClick={() => (step === 0 ? onClose() : setStep(step - 1))}
            disabled={wizardBusy}
          >
            {step === 0 ? tr("Закрыть") : tr("Назад")}
          </button>
          <button
            className="button button-primary"
            onClick={wizardSaved ? onApply : advance}
            disabled={
              wizardBusy ||
              (step === 1 &&
                (pendingNetworks.length > 0 ||
                  !managementIngressInterface.trim() ||
                  !bridgeName.trim() ||
                  !vethName.trim() ||
                  bridgeName === vethName))
            }
          >
            {wizardBusy
              ? tr("Сохраняю…")
              : wizardSaved
                ? tr("Перейти к применению")
                : step === steps.length - 1
                  ? tr("Сохранить черновик")
                  : tr("Продолжить")}
          </button>
        </div>
      </section>
    </div>
  );
}
