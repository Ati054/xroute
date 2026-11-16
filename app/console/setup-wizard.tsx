"use client";

import { JsonObject, createCollectionItem, getCurrentDraft, importRouterOsExport, provisionRouterOsCredentials, saveCurrentDraft, updateCollectionItem } from "../api-client";
import { useLanguage } from "../i18n";
import { initialWizardInternalCidrs, retainedWizardNetworks, wizardInternalNetworks } from "../wizard-networks";
import { NetworkReviewStatus, StatusPill, asObject, asObjectList, asStringList, asText, cdnDeploymentsForTransport, errorMessage, importedNetworkKind, isExplicitRouterHttpsOrigin, isIpv4Cidr, networkReviewStatus, routerImportWarningText, withDeploymentReadiness } from "./shared";
import { useEffect, useRef, useState } from "react";

export function SetupWizard({
  onClose,
  onDraftChanged,
  config: initialConfig,
}: {
  onClose: () => void;
  onDraftChanged: () => Promise<void>;
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
  const initialTopologySuggestions = asObject(
    initialImportReview.topology_suggestions,
  );
  const initialManagementNetworkSuggestions = asStringList(
    initialImportReview.management_network_suggestions,
  );
  const initialReviewNetworks = asObjectList(initialImportReview.networks);
  const configuredManagementCidr =
    asStringList(initialManagement.allowed_source_cidrs)[0] ?? "";
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
  const configuredTunAddress = asText(initialNetworking.tun_address, "");
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
    !configuredTunAddress &&
    asText(initialTopologySuggestions.tun_address, "")
      ? "tun_address"
      : "",
    !configuredInternalDns &&
    asText(initialTopologySuggestions.container_dns, "")
      ? "container_dns"
      : "",
  ].filter(Boolean);
  const hasInitialOfflineAddressPrefill = initialOfflinePrefilledFields.some(
    (field) =>
      [
        "management_cidr",
        "routeros_gateway",
        "container_address",
        "tun_address",
        "container_dns",
      ].includes(field),
  );
  const [step, setStep] = useState(0);
  const [exportName, setExportName] = useState("");
  const [exportFile, setExportFile] = useState<File | null>(null);
  const [channel, setChannel] = useState(
    asText(initialRouteros.channel, tr("Нет данных в экспорте")),
  );
  const [managementCidr, setManagementCidr] = useState(
    configuredManagementCidr ||
      initialManagementNetworkSuggestions[0] ||
      "",
  );
  const [managementIngressInterface, setManagementIngressInterface] = useState(
    asStringList(initialManagement.allowed_ingress_interfaces)[0] ?? "",
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
  const [tunAddress, setTunAddress] = useState(
    configuredTunAddress || asText(initialTopologySuggestions.tun_address, ""),
  );
  const [internalDns, setInternalDns] = useState(
    configuredInternalDns ||
      asText(initialTopologySuggestions.container_dns, ""),
  );
  const [internalNetworks, setInternalNetworks] = useState<string[]>(
    initialWizardInternalCidrs(asObjectList(initialConfig.networks), initialReviewNetworks),
  );
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
  const [subscriptionHost, setSubscriptionHost] = useState("");
  const [providerName, setProviderName] = useState(tr("Основной провайдер"));
  const providerConfigured = asObjectList(initialConfig.subscriptions).some(
    (provider) => provider.id === "provider-main" &&
      provider.enabled !== false && asText(provider.url_secret_ref, "") !== "",
  );
  const [wizardBusy, setWizardBusy] = useState(false);
  const [wizardMessage, setWizardMessage] = useState("");
  const [wizardSaved, setWizardSaved] = useState(false);
  const routerBaseUrlRef = useRef<HTMLInputElement>(null);
  const routerSshPortRef = useRef<HTMLInputElement>(null);
  const routerUsernameRef = useRef<HTMLInputElement>(null);
  const routerPasswordRef = useRef<HTMLInputElement>(null);
  const routerCaRef = useRef<HTMLTextAreaElement>(null);
  const providerUrlRef = useRef<HTMLInputElement>(null);

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
    tr("Удалённый вход"),
    tr("Провайдер"),
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
    isOfflinePrefilled(
      "tun_address",
      tunAddress,
      topologySuggestions.tun_address,
    ),
    isOfflinePrefilled(
      "container_dns",
      internalDns,
      topologySuggestions.container_dns,
    ),
  ].filter(Boolean).length;

  function decideNetwork(cidr: string, status: "accepted" | "ignored") {
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

  async function advance() {
    setWizardMessage("");
    if (step === 0) {
      if (!exportFile) {
        setWizardMessage(
          tr("Выберите обычный экспорт RouterOS без флага show-sensitive."),
        );
        return;
      }
      const baseUrl = routerBaseUrlRef.current?.value.trim() ?? "";
      const routerSshPort = Number(routerSshPortRef.current?.value ?? "22");
      const routerUsername = routerUsernameRef.current?.value.trim() ?? "";
      const routerPassword = routerPasswordRef.current?.value ?? "";
      const caCertificate = routerCaRef.current?.value.trim() ?? "";
      if (!isExplicitRouterHttpsOrigin(baseUrl) || !routerUsername) {
        setWizardMessage(
          tr("Укажите HTTPS origin RouterOS REST с явным портом www-ssl и отдельного пользователя управления."),
        );
        return;
      }
      if (!Number.isInteger(routerSshPort) || routerSshPort < 1 || routerSshPort > 65535) {
        setWizardMessage(tr("Укажите действующий SSH-порт RouterOS."));
        routerSshPortRef.current?.focus();
        return;
      }
      if (routerPassword.length < 16) {
        setWizardMessage(tr("Пароль RouterOS должен содержать не менее 16 символов."));
        routerPasswordRef.current?.focus();
        return;
      }
      if (routerPasswordRef.current) routerPasswordRef.current.value = "";
      if (routerCaRef.current) routerCaRef.current.value = "";
      setWizardBusy(true);
      try {
        const result = await importRouterOsExport(exportFile);
        const resultObject = asObject(result);
        const discovered = asObject(resultObject.discovered);
        if (typeof discovered.channel === "string") {
          setChannel(discovered.channel);
        } else {
          setChannel(tr("Нет данных в экспорте"));
        }
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
          suggestedManagementNetworks[0],
          setManagementCidr,
        );
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
        prefill(
          "tun_address",
          tunAddress,
          suggestedTopology.tun_address,
          setTunAddress,
        );
        prefill(
          "container_dns",
          internalDns,
          suggestedTopology.container_dns,
          setInternalDns,
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
        setNetworkDecisions(importedDecisions);
        setManagementIngressSuggestions(suggestedIngressInterfaces);
        setManagementNetworkSuggestions(suggestedManagementNetworks);
        setTopologySuggestions(suggestedTopology);
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
              "tun_address",
              "container_dns",
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
          ca_certificate: caCertificate || undefined,
        });
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
      if (!tunAddress.trim() || !tunAddress.includes("/")) {
        setWizardMessage(
          tr("Укажите подтверждённый TUN IPv4/CIDR, который не пересекается с вашими сетями."),
        );
        return;
      }
      if (!internalDns.trim() || internalDns.includes("/")) {
        setWizardMessage(
          tr("Укажите фактический IP внутреннего DNS; обычно это RouterOS на container bridge."),
        );
        return;
      }
    }
    if (step === 2 && subscriptionHost && !subscriptionHost.includes(".")) {
      setWizardMessage(
        tr("Укажите фактическое DNS-имя клиентской HTTPS-подписки."),
      );
      return;
    }
    if (step === 3) {
      const providerUrl = providerUrlRef.current?.value.trim() ?? "";
      if (providerUrlRef.current) providerUrlRef.current.value = "";
      if (!providerUrl) {
        setStep(4);
        return;
      }
      if (!providerUrl.startsWith("https://")) {
        setWizardMessage(
          tr("Подписка должна использовать HTTPS. Введите URL ещё раз."),
        );
        return;
      }
      setWizardBusy(true);
      try {
        const draft = await getCurrentDraft<{ config?: JsonObject }>();
        const config = asObject(draft.config);
        const provider = {
          id: "provider-main",
          display_name: providerName,
          enabled: true,
          url: providerUrl,
          refresh_minutes: 360,
          allowed_locations: [],
        };
        const providerExists = asObjectList(config.subscriptions).some(
          (item) => item.id === "provider-main",
        );
        if (providerExists) {
          await updateCollectionItem("subscriptions", "provider-main", provider);
        } else {
          await createCollectionItem("subscriptions", provider);
        }
        await onDraftChanged();
        setStep(4);
      } catch (error) {
        setWizardMessage(errorMessage(error));
        await onDraftChanged();
      } finally {
        setWizardBusy(false);
      }
      return;
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
      const nextNetworks = [
        ...retainedNetworks,
        ...wizardNetworks,
        {
          id: "management-lan",
          enabled: true,
          kind: "management",
          cidrs: [managementCidr],
        },
      ];

      const currentTransports = asObjectList(config.transports);
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
              allowed_source_cidrs: [managementCidr],
              allowed_ingress_interfaces: [managementIngressInterface],
            },
            networking: {
              ...networking,
              bridge_name: bridgeName,
              veth_name: vethName,
              routeros_gateway: routerGateway,
              container_address: containerAddress,
              container_dns: internalDns,
              tun_address: tunAddress,
              addresses_confirmed: true,
            },
          },
          dns: {
            ...dns,
            internal_server: internalDns,
            address_confirmed: true,
          },
          ingress: {
            ...ingress,
            status_hostname: "",
            subscription_endpoint_enabled: Boolean(subscriptionHost),
            subscription_hostname: subscriptionHost,
            subscription_listen_port: Number(
              ingress.subscription_listen_port ?? ingress.public_listen_port ?? 443,
            ),
            subscription_public_port: Number(
              ingress.subscription_public_port ?? ingress.subscription_listen_port ?? ingress.public_listen_port ?? 443,
            ),
            subscription_tls_profile_id: asText(
              ingress.tls_profile_id,
              "cdn-default",
            ),
          },
          networks: nextNetworks,
          transports: currentTransports.map((transport) => ({
            ...transport,
            enabled: false,
            cdn_deployments: cdnDeploymentsForTransport(transport).map((deployment) => ({
              ...deployment,
              enabled: false,
            })),
          })),
          tls_profiles: asObjectList(config.tls_profiles).filter(
            (profile) =>
              Boolean(asText(profile.certificate_secret_ref, "")) ||
              Boolean(asText(profile.private_key_secret_ref, "")),
          ),
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

        <div className="wizard-body">
          {step === 0 ? (
            <div>
              <h3>{tr("Сначала разберём офлайн-снимок конфигурации")}</h3>
              <p>
                 {tr("Загрузите обычный")} <code>{tr("/export file=имя")}</code>  {tr("без флага show-sensitive. Панель прочитает интерфейсы, маршруты, VPN и FastTrack из файла. Перед применением она проверит текущую архитектуру, пакеты, device-mode, накопители и доступность REST.")} </p>
              <label className="drop-zone">
                <input
                  type="file"
                  accept=".rsc,.txt"
                  onChange={(event) => {
                    const file = event.target.files?.[0] ?? null;
                    setExportFile(file);
                    setExportName(file?.name ?? "");
                    setWizardMessage("");
                  }}
                />
                <span className="drop-icon">⇧</span>
                <strong>{exportName || tr("Выберите экспорт MikroTik")}</strong>
                <small>{tr("Секреты в экспорт включать нельзя")}</small>
              </label>
              <div className="form-grid">
                <label className="field">
                  RouterOS REST HTTPS URL
                  <input
                    ref={routerBaseUrlRef}
                    type="url"
                    pattern="https://.+:[0-9]+/?"
                    defaultValue={asText(initialRouteros.base_url, "")}
                    placeholder="https://172.31.255.1:59443"
                    required
                  />
                  <small>
                     {tr("RouterOS REST обслуживается службой www-ssl, не api-ssl. В URL укажите выбранный свободный порт; TCP 443 нельзя считать свободным по умолчанию. В Available From оставьте ровно выбранные management CIDR и container /32, без 0.0.0.0/0. Только HTTPS, проверка сертификата не отключается.")} </small>
                </label>
                <label className="field">
                   {tr("SSH-порт RouterOS")} <input
                    ref={routerSshPortRef}
                    type="number"
                    min={1}
                    max={65535}
                    defaultValue={
                      typeof initialRouteros.ssh_port === "number"
                        ? initialRouteros.ssh_port
                        : 22
                    }
                    required
                  />
                  <small>
                     {tr("Панель применяет изменения по SSH. Локальная задача RouterOS вернёт прежние правила, если применение не завершится успешно.")} </small>
                </label>
                <label className="field">
                   {tr("Отдельный REST-пользователь")} <input
                    ref={routerUsernameRef}
                    defaultValue={initialSystem.object_comment_prefix === "XROUTE" ? "xroute-api" : "sb-gateway-api"}
                    autoComplete="username"
                    required
                  />
                </label>
                <label className="field">
                   {tr("Пароль RouterOS · минимум 16 символов")} <input
                    ref={routerPasswordRef}
                    type="password"
                    minLength={16}
                    autoComplete="new-password"
                    required
                  />
                </label>
                <div className="locked-field">
                  <span>{tr("Шифрование резервных копий")}</span>
                  <strong>{tr("Пароль администратора панели")}</strong>
                  <small>{tr("Отдельный пароль не требуется")}</small>
                </div>
                <label className="field form-span">
                   {tr("Сертификат CA для RouterOS (PEM)")} <textarea
                    ref={routerCaRef}
                    rows={5}
                    autoComplete="off"
                    placeholder="-----BEGIN CERTIFICATE-----"
                  />
                  <small>
                     {tr("Для локального сертификата вставьте публичный сертификат CA. Для сертификата из доверенного системного хранилища оставьте поле пустым.")} </small>
                </label>
              </div>
              <div className="wizard-inline-fields">
                <label className="field">
                   {tr("Канал обновлений из экспорта")} <input
                    value={channel}
                    readOnly
                  />
                  <small>
                     {tr("Панель проверит необходимые функции RouterOS и сохранит выбранный канал обновлений.")} </small>
                </label>
                <div className="locked-field">
                  <span>{tr("Проверка версии")}</span>
                  <strong>{tr("Совместимость с RouterOS 7")}</strong>
                  <small>{tr("Версию и необходимые функции проверим перед применением.")}</small>
                </div>
              </div>
              {importAnalyzed ? (
                <aside className="notice notice-danger">
                  <span className="notice-icon">!</span>
                  <div>
                    <strong>{tr("Требуется проверка RouterOS")}</strong>
                    <p>
                       {tr("При применении панель подключится к RouterOS и проверит версию, пакеты и настройки сети.")} </p>
                    {importWarnings.length ? (
                      <ul>
                        {importWarnings.map((warning) => (
                          <li key={warning}>
                            {routerImportWarningText(warning, locale)}
                          </li>
                        ))}
                      </ul>
                    ) : null}
                  </div>
                </aside>
              ) : null}
              {importAnalyzed && routerosConnectionSaved && !exportFile ? (
                <aside className="notice notice-safe">
                  <span className="notice-icon">✓</span>
                  <div>
                    <strong>{tr("Подключение к RouterOS сохранено")}</strong>
                    <p>
                       {tr("Продолжите настройку с сохранёнными параметрами. Панель проверит подключение перед применением.")} </p>
                    <div className="card-actions">
                      <button
                        className="button button-primary"
                        type="button"
                        onClick={() => setStep(1)}
                      >
                         {tr("Продолжить с сохранённым снимком")} </button>
                    </div>
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
                <aside className="notice notice-danger">
                  <span className="notice-icon">!</span>
                  <div>
                    <strong>
                       {tr("Предложенные параметры:")} {activeOfflinePrefillCount}
                    </strong>
                    <p>
                       {tr("Параметры предложены на основе экспорта. Проверьте адреса и имена интерфейсов; при применении панель сверит их с RouterOS.")} </p>
                  </div>
                </aside>
              ) : null}
              <label className="field">
                 {tr("Интерфейс доступа к панели")} <input
                  list="management-ingress-options"
                  value={managementIngressInterface}
                  onChange={(event) =>
                    setManagementIngressInterface(event.target.value)
                  }
                  placeholder={tr("Выберите bridge или введите точное имя")}
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
                   {tr("Выберите LAN-мост для доступа к панели. Если его нет в списке, введите имя вручную. WAN использовать нельзя.")} </small>
              </label>
              {!managementIngressSuggestions.length ? (
                <aside className="notice notice-danger">
                  <span className="notice-icon">!</span>
                  <div>
                    <strong>{tr("Выберите интерфейс доступа")}</strong>
                    <p>
                       {tr("Введите имя своего LAN-моста. Панель проверит интерфейс перед применением.")} </p>
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
                Management CIDR
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
              <div className="form-grid">
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
                  Container IPv4/CIDR
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
                <label className="field">
                  TUN IPv4/CIDR
                  <input
                    value={tunAddress}
                    onChange={(event) => setTunAddress(event.target.value)}
                    placeholder={tr("Выберите свободную непересекающуюся подсеть")}
                  />
                  <small>
                    {isOfflinePrefilled(
                      "tun_address",
                      tunAddress,
                      topologySuggestions.tun_address,
                    )
                      ? tr("Панель проверит, что эта подсеть не пересекается с сетями LAN и VPN.")
                      : tr("Apply проверит пересечение с импортированными LAN/VPN сетями.")}
                  </small>
                </label>
                <label className="field form-span">
                   {tr("Внутренний DNS")} <input
                    value={internalDns}
                    onChange={(event) => setInternalDns(event.target.value)}
                    placeholder={tr("Фактический IP RouterOS или другого DNS")}
                  />
                  <small>
                    {isOfflinePrefilled(
                      "container_dns",
                      internalDns,
                      topologySuggestions.container_dns,
                    )
                      ? tr("Проверьте адрес DNS-сервера вашей локальной сети.")
                      : tr("Внутренние зоны не отправляются внешнему DNS-провайдеру.")}
                  </small>
                </label>
              </div>
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
                  <small>
                     {tr("Массовое действие применяется только к текущему фильтру и остаётся явным решением в черновике. Для полного переноса существующих VPN-маршрутов можно показать все и принять их.")} </small>
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
                      {ignoredSuggestionCount}{tr(". Простое отсутствие галочки не считается решением.")} </p>
                  </div>
                </aside>
              ) : null}
              <div className="discovered-networks">
                {visibleDiscoveredNetworks.map((cidr) => {
                  const decision = networkReviewStatus(networkDecisions[cidr]);
                  return (
                    <div className="checkbox-field" key={cidr}>
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
            </div>
          ) : null}

          {step === 2 ? (
            <div>
              <h3>{tr("HTTPS-подписка для удалённых устройств")}</h3>
              <p>
                 {tr("Ссылка подписки настраивается отдельно. После мастера включите только нужные транспорты: WebSocket не является обязательным.")} </p>
              <div className="form-grid">
                <label className="field form-span">
                 {tr("DNS-имя клиентской подписки (необязательно)")} <input
                    value={subscriptionHost}
                    onChange={(event) => setSubscriptionHost(event.target.value)}
                    placeholder="sub.example.com"
                  />
                  <small>
                     {tr("Постоянная HTTPS-ссылка нужна VPN-клиентам для импорта и автоматического обновления конфигурации. Контейнер отдельно проверяет сам MikroTik по внутренней сети.")} </small>
                </label>
              </div>
              <aside className="notice notice-neutral">
                <span className="notice-icon">i</span>
                <div>
                  <strong>{tr("Транспорты настраиваются независимо")}</strong>
                  <p>
                     {tr("Можно включить только gRPC и XHTTP либо любой другой набор. Все включённые транспорты попадут в ту же постоянную ссылку.")} </p>
                </div>
              </aside>
            </div>
          ) : null}

          {step === 3 ? (
            <div>
              <h3>{tr("Добавьте первого исходящего прокси-провайдера")}</h3>
              <p>
                 {tr("Ссылка не записывается в журнал. Изменения проверяются перед заменой рабочей конфигурации.")} </p>
              <label className="field">
                 {tr("Понятное название")} <input
                  value={providerName}
                  onChange={(event) => setProviderName(event.target.value)}
                />
              </label>
              <label className="field">
                 {tr("URL подписки (необязательно)")} <input
                  ref={providerUrlRef}
                  type="password"
                  placeholder={tr("Ссылка на подписку провайдера")}
                  autoComplete="off"
                />
                <small>
                   {tr("После сохранения URL можно только заменить — прочитать его из панели нельзя.")} </small>
              </label>
              <div className="wizard-provider-check">
                <StatusPill tone="info">{tr("Проверка при сохранении")}</StatusPill>
                <span>
                   {tr("Поддерживаются ссылки VLESS, REALITY и Hysteria 2, списки Base64 и файлы Xray JSON.")} </span>
              </div>
            </div>
          ) : null}

          {step === 4 ? (
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
                  <dt>{tr("Контейнер")} / TUN / DNS</dt>
                  <dd>
                    {bridgeName} · {vethName} · RouterOS{" "}
                    {routerGateway} · {tr("Контейнер")} {containerAddress} · TUN{" "}
                    {tunAddress} · DNS {internalDns}
                  </dd>
                </div>
                <div>
                  <dt>{tr("Внутренние сети")}</dt>
                  <dd>{internalNetworks.length}  {tr("точных CIDR")}</dd>
                </div>
                <div>
                  <dt>{tr("Подсказки из .rsc")}</dt>
                  <dd>
                     {tr("принято")} {acceptedSuggestionCount}  {tr("· проигнорировано")}{" "}
                    {ignoredSuggestionCount}  {tr("· ожидает")} {pendingNetworks.length}
                  </dd>
                </div>
                <div>
                  <dt>{tr("Удалённый вход")}</dt>
                  <dd>{subscriptionHost ? <>{tr("подписка:")} {subscriptionHost} {tr("· транспорты настраиваются отдельно")}</> : tr("клиентский режим · публичная подписка выключена")}</dd>
                </div>
                <div>
                  <dt>{tr("Провайдер")}</dt>
                  <dd>{providerConfigured ? <>{providerName} {tr("· секрет скрыт")}</> : tr("Не настроено")}</dd>
                </div>
                <div>
                  <dt>{tr("Отказ контейнера")}</dt>
                  <dd>{tr("Локальные → WAN; удалённые → блокировка")}</dd>
                </div>
              </dl>
              {activeOfflinePrefillCount ? (
                <aside className="notice notice-danger">
                  <span className="notice-icon">!</span>
                  <div>
                    <strong>{tr("Проверьте параметры перед сохранением")}</strong>
                    <p>
                       {tr("Сохранение запишет параметры в черновик. Чтобы включить настройки, нажмите «Применить» и подтвердите изменения.")} </p>
                  </div>
                </aside>
              ) : null}
              <aside className="notice notice-danger">
                <span className="notice-icon">!</span>
                <div>
                  <strong>{tr("Проверка перед применением")}</strong>
                  <p>
                     {tr("При применении панель проверит RouterOS и покажет список изменений. Если проверка не пройдёт, настройки останутся прежними.")} </p>
                </div>
              </aside>
              <aside className="notice notice-safe">
                <span className="notice-icon">✓</span>
                <div>
                  <strong>{tr("Перед применением автоматически")}</strong>
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
            onClick={wizardSaved ? onClose : advance}
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
                ? tr("Закрыть мастер")
                : step === steps.length - 1
                  ? tr("Сохранить черновик")
                  : tr("Продолжить")}
          </button>
        </div>
      </section>
    </div>
  );
}
