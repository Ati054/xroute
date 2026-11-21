:local xrouteFileNamespace "XROUTE"
:local xrouteDelegated false
# XRoute namespace compatibility guard: begin
:local xrouteCurrentKnown [/container/find where comment~"^XROUTE (container|rollback container)( previous| candidate| rollback| failed)?\$"]
:local xrouteLegacyKnown [/container/find where comment~"^SB-GATEWAY (container|rollback container)( previous| candidate| rollback| failed)?\$"]
:if (([:len $xrouteCurrentKnown] > 0) && ([:len $xrouteLegacyKnown] > 0)) do={ :error "XRoute: managed namespaces are mixed; no changes made" }
:local xrouteKnown [/container/find where (comment="XROUTE container") or (comment="SB-GATEWAY container")]
:if ([:len $xrouteKnown] > 1) do={ :error "XRoute: managed container identity is ambiguous; no changes made" }
:if (([:len $xrouteKnown] = 0) && ([:len $xrouteLegacyKnown] > 0)) do={ :set xrouteKnown [:pick $xrouteLegacyKnown 0 1] }
:if (([:len $xrouteKnown] = 0) && ([:len $xrouteCurrentKnown] > 0)) do={ :set xrouteKnown [:pick $xrouteCurrentKnown 0 1] }
:if ([:len $xrouteKnown] = 1) do={
  :if (($xrouteFileNamespace = "SB-GATEWAY") && ([:len $xrouteCurrentKnown] > 0)) do={ :error "XRoute: legacy installer cannot manage a new installation" }
  :if (($xrouteFileNamespace = "XROUTE") && ([:len $xrouteLegacyKnown] > 0)) do={
    :local xrouteOwnedRoot [:tostr [/container/get $xrouteKnown root-dir]]
    :if ([:pick $xrouteOwnedRoot 0 1] = "/") do={ :set xrouteOwnedRoot [:pick $xrouteOwnedRoot 1 [:len $xrouteOwnedRoot]] }
    :local xrouteLastSlash -1
    :local xrouteScan [:find $xrouteOwnedRoot "/"]
    :while ([:typeof $xrouteScan] = "num") do={ :set xrouteLastSlash $xrouteScan; :set xrouteScan [:find $xrouteOwnedRoot "/" ($xrouteScan + 1)] }
    :if ($xrouteLastSlash < 1) do={ :error "XRoute: existing storage identity is invalid" }
    :local xrouteRootName [:pick $xrouteOwnedRoot ($xrouteLastSlash + 1) [:len $xrouteOwnedRoot]]
    :if (($xrouteRootName != "root") && ([:pick $xrouteRootName 0 5] != "root-")) do={ :error "XRoute: existing container root is not recognized" }
    :local xrouteCompatibilityFile ([:pick $xrouteOwnedRoot 0 $xrouteLastSlash] . "/legacy/install.rsc")
    :if ([:len [/file/find where name=$xrouteCompatibilityFile]] != 1) do={ :error "XRoute: upload the matching legacy installer directory beside the existing installation" }
    /import file-name=$xrouteCompatibilityFile
    :set xrouteDelegated true
  }
}
# XRoute namespace compatibility guard: end

:if ($xrouteDelegated != true) do={
# Reject a conflicting ownership binding before making any changes.
:local xrouteExistingBinding [/container/envs/find where list="xroute-env" and key="SB_ROUTEROS_NAMESPACE"]
:if ([:len $xrouteExistingBinding] > 1) do={ :error "XROUTE: managed namespace environment is ambiguous" }
:if ([:len $xrouteExistingBinding] = 1) do={ :if ([/container/envs/get $xrouteExistingBinding value] != "XROUTE") do={ :error "XROUTE: managed namespace environment does not match this installation" } }


# XRoute idempotent install for RouterOS 7 ARM64 and x86 CHR.
# Import variables, preflight, and fasttrack-patch first. The new diversion gate
# is created disabled and is enabled only by watchdog hysteresis.

:global "SB_IMAGE"
:global "SB_IMAGE_SOURCE"
:global "SB_IMAGE_FILE"
:global "SB_BRIDGE"
:global "SB_VETH"
:global "SB_ROUTER_ADDRESS"
:global "SB_ROUTER_IP"
:global "SB_CONTAINER_ADDRESS"
:global "SB_CONTAINER_IP"
:global "SB_CONTAINER_NETWORK"
:global "SB_TUN_ADDRESS"
:global "SB_ROUTING_TABLE"
:global "SB_ROOT_DIR"
:global "SB_CONFIG_DIR"
:global "SB_DATA_DIR"
:global "SB_LOGS_DIR"
:global "SB_STATE_DIR"
:global "SB_CONTAINER_MEMORY_HIGH"
:global "SB_CONTAINER_MEMORY_MAX"
:global "SB_MANAGED_CLIENTS"
:global "SB_INTERNAL_NETWORKS"
:global "SB_BYPASS_ENDPOINTS"
:global "SB_CONTAINER_ALLOWED_INTERNAL"
:global "SB_MANAGEMENT_SOURCES"
:global "SB_MANAGEMENT_INGRESS_INTERFACES"
:global "SB_IPV6_MODE"
:global "SB_MANAGED_CLIENTS_V6"
:global "SB_INTERNAL_NETWORKS_V6"
:global "SB_WS_HOST"
:global "SB_GRPC_HOST"
:global "SB_STATUS_HOST"
:global "SB_HU_HOST"
:global "SB_HTTPUPGRADE_ENABLED"
:global "SB_ROUTEROS_REST_PORT"

:local bootScript [/system/script/find where name="XROUTE-container-startup" and comment="XROUTE storage-aware startup"]
:local storageProbe [/system/script/find where name="XROUTE-storage-ready" and comment="XROUTE storage readiness"]
:local bootScheduler [/system/scheduler/find where name="XROUTE-container-startup" and comment="XROUTE storage-aware startup scheduler"]
:if (([:len $bootScript] != 1) || ([:len $storageProbe] != 1) || ([:len $bootScheduler] != 1)) do={ :error "XROUTE: import container-startup.rsc before install.rsc" }

# Only explicit installation may create missing mount folders. CHR may use a
# dedicated directory on its persistent system disk; physical routers require a disk.
:local hostArchitecture [/system/resource/get architecture-name]
:local systemDiskAllowed (([:pick [/system/resource/get board-name] 0 3] = "CHR") && (($hostArchitecture = "x86") || ($hostArchitecture = "x86_64")))
:foreach folder in={$"SB_CONFIG_DIR";$"SB_DATA_DIR";$"SB_LOGS_DIR";$"SB_STATE_DIR"} do={
  :local path [:tostr $folder]
  :if ([:pick $path 0 1] = "/") do={ :set path [:pick $path 1 [:len $path]] }
  :local separator [:find $path "/"]
  :if (([:typeof $separator] != "num") || ($separator = 0) || ([:typeof [:find $path ".."]] != "nil")) do={ :error "XROUTE: mount source path is invalid" }
  :local disk [/file/find where name=[:pick $path 0 $separator]]
  :if ([:len $disk] != 1) do={ :error "XROUTE: mount source disk is missing or ambiguous" }
  :if ([/file/get $disk type] != "disk") do={
    :if (($systemDiskAllowed != true) || ([/file/get $disk type] != "directory")) do={ :error "XROUTE: mount source disk is not mounted" }
  }
  :local entry [/file/find where name=$path]
  :if ([:len $entry] > 1) do={ :error "XROUTE: mount source is ambiguous" }
  :if ([:len $entry] = 0) do={ /file/add name=$path type=directory } else={
    :if ([/file/get $entry type] != "directory") do={ :error "XROUTE: mount source is not a directory" }
  }
}

:local containerMemoryHigh $"SB_CONTAINER_MEMORY_HIGH"
:local containerMemoryMax $"SB_CONTAINER_MEMORY_MAX"
:if ([:len $containerMemoryHigh] = 0) do={ :set containerMemoryHigh "288M" }
:if ([:len $containerMemoryMax] = 0) do={ :set containerMemoryMax "320M" }

# Re-imports also start fail-open.  Only the exact project gate/connections are
# touched while trusted ingress and policy rules are reconciled.
/ip/firewall/mangle/disable [find where comment="XROUTE diversion-gate"]
:foreach sbFlow in=[/ip/firewall/connection/find where connection-mark="xroute-managed"] do={ :do { /ip/firewall/connection/remove $sbFlow } on-error={} }

:if ([:len [/interface/bridge/find where name=$"SB_BRIDGE"]] = 0) do={
  /interface/bridge/add name=$"SB_BRIDGE" protocol-mode=none comment="XROUTE container bridge"
}
:if ([:len [/interface/veth/find where name=$"SB_VETH"]] = 0) do={
  /interface/veth/add name=$"SB_VETH" address=$"SB_CONTAINER_ADDRESS" gateway=$"SB_ROUTER_IP" comment="XROUTE container veth"
}
:if ([:len [/interface/bridge/port/find where bridge=$"SB_BRIDGE" and interface=$"SB_VETH"]] = 0) do={
  /interface/bridge/port/add bridge=$"SB_BRIDGE" interface=$"SB_VETH" comment="XROUTE veth bridge port"
}
:if ([:len [/ip/address/find where interface=$"SB_BRIDGE" and address=$"SB_ROUTER_ADDRESS"]] = 0) do={
  /ip/address/add address=$"SB_ROUTER_ADDRESS" interface=$"SB_BRIDGE" comment="XROUTE router address"
}

:if ([:len [/interface/list/find where name="SB_MANAGEMENT_INGRESS"]] = 0) do={
  /interface/list/add name="SB_MANAGEMENT_INGRESS" comment="XROUTE management ingress list"
}
:local ingressList [/interface/list/find where name="SB_MANAGEMENT_INGRESS" and comment="XROUTE management ingress list"]
:if ([:len $ingressList] != 1) do={ :error "XROUTE: management ingress interface-list is missing or ambiguous" }
:foreach member in=[/interface/list/member/find where list="SB_MANAGEMENT_INGRESS"] do={
  :if ([/interface/list/member/get $member comment] != "XROUTE management ingress") do={ :error "XROUTE: refusing to alter an unowned management ingress member" }
}
:foreach member in=[/interface/list/member/find where list="SB_MANAGEMENT_INGRESS" and comment="XROUTE management ingress"] do={
  /interface/list/member/remove $member
}
:foreach interfaceName in=$"SB_MANAGEMENT_INGRESS_INTERFACES" do={
  :if ([:len [/interface/find where name=$interfaceName]] != 1) do={ :error ("XROUTE: trusted ingress interface is missing or ambiguous: " . $interfaceName) }
  /interface/list/member/add list="SB_MANAGEMENT_INGRESS" interface=$interfaceName comment="XROUTE management ingress"
}

:if ([:len [/interface/list/find where name="SB_WIREGUARD_INGRESS"]] = 0) do={
  /interface/list/add name="SB_WIREGUARD_INGRESS" comment="XROUTE WireGuard panel ingress list"
}
:local wireguardIngressList [/interface/list/find where name="SB_WIREGUARD_INGRESS" and comment="XROUTE WireGuard panel ingress list"]
:if ([:len $wireguardIngressList] != 1) do={ :error "XROUTE: WireGuard panel ingress interface-list is missing or ambiguous" }
:foreach member in=[/interface/list/member/find where list="SB_WIREGUARD_INGRESS"] do={
  :if ([/interface/list/member/get $member comment] != "XROUTE WireGuard panel ingress") do={ :error "XROUTE: refusing to alter an unowned WireGuard panel ingress member" }
}
:foreach member in=[/interface/list/member/find where list="SB_WIREGUARD_INGRESS" and comment="XROUTE WireGuard panel ingress"] do={
  /interface/list/member/remove $member
}
:foreach wgInterface in=[/interface/wireguard/find where disabled=no] do={
  :local wgName [/interface/wireguard/get $wgInterface name]
  /interface/list/member/add list="SB_WIREGUARD_INGRESS" interface=$wgName comment="XROUTE WireGuard panel ingress"
}

:foreach sbManagedCidr in=$"SB_MANAGED_CLIENTS" do={
  :local sbManagedText [:tostr $sbManagedCidr]
  :local sbManagedLookup $sbManagedText
  :local sbManagedHostSuffix [:find $sbManagedText "/32"]
  :if (($sbManagedHostSuffix != nil) && ([:pick $sbManagedText $sbManagedHostSuffix [:len $sbManagedText]] = "/32")) do={ :set sbManagedLookup [:pick $sbManagedText 0 $sbManagedHostSuffix] }
  :if ([:len [/ip/firewall/address-list/find where list="SB_MANAGED_CLIENTS" and address=$sbManagedLookup]] = 0) do={
    /ip/firewall/address-list/add list="SB_MANAGED_CLIENTS" address=$sbManagedText comment="XROUTE managed client"
  }
}
:foreach sbInternalCidr in=$"SB_INTERNAL_NETWORKS" do={
  :local sbInternalText [:tostr $sbInternalCidr]
  :local sbInternalLookup $sbInternalText
  :local sbInternalHostSuffix [:find $sbInternalText "/32"]
  :if (($sbInternalHostSuffix != nil) && ([:pick $sbInternalText $sbInternalHostSuffix [:len $sbInternalText]] = "/32")) do={ :set sbInternalLookup [:pick $sbInternalText 0 $sbInternalHostSuffix] }
  :if ([:len [/ip/firewall/address-list/find where list="SB_INTERNAL_NETWORKS" and address=$sbInternalLookup]] = 0) do={
    /ip/firewall/address-list/add list="SB_INTERNAL_NETWORKS" address=$sbInternalText comment="XROUTE internal network"
  }
}
:foreach sbBypassCidr in=$"SB_BYPASS_ENDPOINTS" do={
  :local sbBypassText [:tostr $sbBypassCidr]
  :local sbBypassLookup $sbBypassText
  :local sbBypassHostSuffix [:find $sbBypassText "/32"]
  :if (($sbBypassHostSuffix != nil) && ([:pick $sbBypassText $sbBypassHostSuffix [:len $sbBypassText]] = "/32")) do={ :set sbBypassLookup [:pick $sbBypassText 0 $sbBypassHostSuffix] }
  :if ([:len [/ip/firewall/address-list/find where list="SB_BYPASS_ENDPOINTS" and address=$sbBypassLookup]] = 0) do={
    /ip/firewall/address-list/add list="SB_BYPASS_ENDPOINTS" address=$sbBypassText comment="XROUTE loop bypass"
  }
}
:foreach sbAllowedCidr in=$"SB_CONTAINER_ALLOWED_INTERNAL" do={
  :local sbAllowedText [:tostr $sbAllowedCidr]
  :local sbAllowedLookup $sbAllowedText
  :local sbAllowedHostSuffix [:find $sbAllowedText "/32"]
  :if (($sbAllowedHostSuffix != nil) && ([:pick $sbAllowedText $sbAllowedHostSuffix [:len $sbAllowedText]] = "/32")) do={ :set sbAllowedLookup [:pick $sbAllowedText 0 $sbAllowedHostSuffix] }
  :if ([:len [/ip/firewall/address-list/find where list="SB_CONTAINER_ALLOWED_INTERNAL" and address=$sbAllowedLookup]] = 0) do={
    /ip/firewall/address-list/add list="SB_CONTAINER_ALLOWED_INTERNAL" address=$sbAllowedText comment="XROUTE explicit container LAN access"
  }
}
:foreach sbManagementCidr in=$"SB_MANAGEMENT_SOURCES" do={
  :local sbManagementText [:tostr $sbManagementCidr]
  :local sbManagementLookup $sbManagementText
  :local sbManagementHostSuffix [:find $sbManagementText "/32"]
  :if (($sbManagementHostSuffix != nil) && ([:pick $sbManagementText $sbManagementHostSuffix [:len $sbManagementText]] = "/32")) do={ :set sbManagementLookup [:pick $sbManagementText 0 $sbManagementHostSuffix] }
  :if ([:len [/ip/firewall/address-list/find where list="SB_MANAGEMENT_SOURCES" and address=$sbManagementLookup]] = 0) do={
    /ip/firewall/address-list/add list="SB_MANAGEMENT_SOURCES" address=$sbManagementText comment="XROUTE management source"
  }
}
:if ([:len [/ip/firewall/address-list/find where list="SB_INTERNAL_NETWORKS" and address=$"SB_CONTAINER_NETWORK"]] = 0) do={
  /ip/firewall/address-list/add list="SB_INTERNAL_NETWORKS" address=$"SB_CONTAINER_NETWORK" comment="XROUTE container network bypass"
}

:if ([:len [/routing/table/find where name=$"SB_ROUTING_TABLE"]] = 0) do={
  /routing/table/add name=$"SB_ROUTING_TABLE" fib comment="XROUTE routing table"
}
:if ([:len [/ip/route/find where dst-address="0.0.0.0/0" and routing-table=$"SB_ROUTING_TABLE" and comment="XROUTE container default"]] = 0) do={
  /ip/route/add dst-address="0.0.0.0/0" gateway=($"SB_CONTAINER_IP" . "@main") routing-table=$"SB_ROUTING_TABLE" distance=1 check-gateway=ping comment="XROUTE container default"
}

:if ([:len [/container/mounts/find where list="xroute-config"]] = 0) do={ /container/mounts/add list="xroute-config" src=$"SB_CONFIG_DIR" dst="/config" comment="XROUTE config mount" }
:if ([:len [/container/mounts/find where list="xroute-data"]] = 0) do={ /container/mounts/add list="xroute-data" src=$"SB_DATA_DIR" dst="/data" comment="XROUTE data mount" }
:if ([:len [/container/mounts/find where list="xroute-logs"]] = 0) do={ /container/mounts/add list="xroute-logs" src=$"SB_LOGS_DIR" dst="/logs" comment="XROUTE logs mount" }
:if ([:len [/container/mounts/find where list="xroute-state"]] = 0) do={ /container/mounts/add list="xroute-state" src=$"SB_STATE_DIR" dst="/state" comment="XROUTE state mount" }

:if ([:len [/container/envs/find where list="xroute-env" and key="SB_ROUTEROS_GATEWAY"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_ROUTEROS_GATEWAY" value=$"SB_ROUTER_IP" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_CONTAINER_ADDRESS"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_CONTAINER_ADDRESS" value=$"SB_CONTAINER_ADDRESS" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_CONTAINER_IP"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_CONTAINER_IP" value=$"SB_CONTAINER_IP" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_TUN_ADDRESS"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_TUN_ADDRESS" value=$"SB_TUN_ADDRESS" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_WS_HOST"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_WS_HOST" value=$"SB_WS_HOST" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_GRPC_HOST"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_GRPC_HOST" value=$"SB_GRPC_HOST" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_STATUS_HOST"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_STATUS_HOST" value=$"SB_STATUS_HOST" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_HU_HOST"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_HU_HOST" value=$"SB_HU_HOST" comment="XROUTE runtime env" }
:if ([:len [/container/envs/find where list="xroute-env" and key="SB_HTTPUPGRADE_ENABLED"]] = 0) do={ /container/envs/add list="xroute-env" key="SB_HTTPUPGRADE_ENABLED" value=$"SB_HTTPUPGRADE_ENABLED" comment="XROUTE runtime env" }

# Bind ownership once; candidate images retain this envlist during updates.
:local xrouteNamespaceEnv [/container/envs/find where list="xroute-env" and key="SB_ROUTEROS_NAMESPACE"]
:if ([:len $xrouteNamespaceEnv] > 1) do={ :error "XROUTE: managed namespace environment is ambiguous" }
:if ([:len $xrouteNamespaceEnv] = 0) do={ /container/envs/add list="xroute-env" key="SB_ROUTEROS_NAMESPACE" value="XROUTE" comment="XROUTE runtime env" } else={
  :if ([/container/envs/get $xrouteNamespaceEnv value] != "XROUTE") do={ :error "XROUTE: managed namespace environment does not match this installation" }
}

:local containerId [/container/find where comment="XROUTE container"]
:local containerCreated false
:if ([:len $containerId] = 0) do={
  :onerror sbContainerError in={
    :if ($"SB_IMAGE_SOURCE" = "file") do={
      /container/add file=$"SB_IMAGE_FILE" interface=$"SB_VETH" root-dir=$"SB_ROOT_DIR" mountlists="xroute-config,xroute-data,xroute-logs,xroute-state" envlist="xroute-env" dns=$"SB_ROUTER_IP" memory-high=$containerMemoryHigh memory-max=$containerMemoryMax start-on-boot=no logging=yes comment="XROUTE container"
    } else={
      /container/add remote-image=$"SB_IMAGE" interface=$"SB_VETH" root-dir=$"SB_ROOT_DIR" mountlists="xroute-config,xroute-data,xroute-logs,xroute-state" envlist="xroute-env" dns=$"SB_ROUTER_IP" memory-high=$containerMemoryHigh memory-max=$containerMemoryMax start-on-boot=no logging=yes comment="XROUTE container"
    }
  } do={
    :log error ("XROUTE: container add/capability check failed; diversion remains disabled: " . $sbContainerError)
    :error ("XROUTE: container creation failed: " . $sbContainerError)
  }
  :set containerId [/container/find where comment="XROUTE container"]
  :if ([:len $containerId] != 1) do={ :error "XROUTE: newly added container is missing or ambiguous" }
  :set containerCreated true
} else={
  :if ([:len $containerId] != 1) do={ :error "XROUTE: project container is ambiguous" }
}

# Apply lifecycle limits to both a freshly added container and an existing
# project-owned container. A fresh install must never retain RouterOS defaults
# restart-policy=no/restart-interval=0s.
/container/set $containerId memory-high=$containerMemoryHigh memory-max=$containerMemoryMax start-on-boot=no logging=yes
:do { /container/set $containerId restart-policy=always restart-interval=10s } on-error={ /container/set $containerId auto-restart-interval=10s }

:local imageReady false
:local imageWaitAttempt 0
:local containerStopped false
:local legacyContainerStatus "unknown"
:if ($containerCreated = false) do={ :set imageReady true }
:while (($imageReady = false) && ($imageWaitAttempt < 360)) do={
  :local stoppedKnown false
  :local runningKnown false
  :local containerRunning false
  :local imageOsProbe ""
  :local imageArchProbe ""
  :do {
    :set containerStopped [/container/get $containerId stopped]
    :set stoppedKnown true
  } on-error={
    :do { :set containerRunning [/container/get $containerId running]; :set runningKnown true } on-error={
      :do { :set legacyContainerStatus [/container/get $containerId status] } on-error={ :set legacyContainerStatus "unknown" }
    }
  }
  :do { :set imageOsProbe [/container/get $containerId os]; :set imageArchProbe [/container/get $containerId arch] } on-error={}
  :if ((($stoppedKnown = true) || (($runningKnown = true) && ($containerRunning = true))) && ([:len $imageOsProbe] > 0) && ([:len $imageArchProbe] > 0)) do={
    :set imageReady true
  } else={
    :if (($stoppedKnown = false) && (($legacyContainerStatus = "stopped") || ($legacyContainerStatus = "running"))) do={ :set imageReady true }
    :if (($stoppedKnown = false) && ($legacyContainerStatus = "error")) do={
      :log error "XROUTE: container image download/extraction failed; diversion remains disabled and existing Internet is unchanged"
      :error "XROUTE: container image preparation failed"
    }
    :if ($imageReady = false) do={
      :set imageWaitAttempt ($imageWaitAttempt + 1)
      :delay 5s
    }
  }
}
:if ($imageReady = false) do={
  :log error "XROUTE: container image was not ready after 30 minutes; diversion remains disabled and existing Internet is unchanged"
  :error "XROUTE: timed out waiting for container image extraction"
}
:local imageOs ""
:local imageArch ""
:do {
  :set imageOs [/container/get $containerId os]
  :set imageArch [/container/get $containerId arch]
} on-error={
  :log error "XROUTE: unable to inspect extracted container platform; diversion remains disabled and existing Internet is unchanged"
  :error "XROUTE: container platform inspection failed"
}
:local expectedImageArchitecture "arm64"
:if (($hostArchitecture = "x86") || ($hostArchitecture = "x86_64")) do={ :set expectedImageArchitecture "amd64" }
:if (($imageOs != "linux") || ($imageArch != $expectedImageArchitecture)) do={
  :log error ("XROUTE: refusing container platform " . $imageOs . "/" . $imageArch . "; expected linux/" . $expectedImageArchitecture . "; diversion remains disabled")
  :error "XROUTE: wrong container platform"
}

:if ([:len [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]] = 0) do={
  /ip/firewall/mangle/add chain=prerouting action=jump jump-target="xroute-divert" src-address-list="SB_MANAGED_CLIENTS" dst-address-list="!SB_INTERNAL_NETWORKS" dst-address-type=!local disabled=yes comment="XROUTE diversion-gate"
}
:local diversionGate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]
:if ([:len $diversionGate] != 1) do={ :error "XROUTE: diversion gate is missing or ambiguous" }
/ip/firewall/mangle/unset $diversionGate in-interface-list
/ip/firewall/mangle/unset $diversionGate dst-address-list
/ip/firewall/mangle/unset $diversionGate dst-address-type
/ip/firewall/mangle/set $diversionGate chain=prerouting action=jump jump-target="xroute-divert" src-address-list="SB_MANAGED_CLIENTS" src-address=("!" . $"SB_CONTAINER_IP") disabled=yes
:if ([:len [/ip/firewall/mangle/find where comment="XROUTE endpoint bypass"]] = 0) do={
  /ip/firewall/mangle/add chain="xroute-divert" action=return dst-address-list="SB_BYPASS_ENDPOINTS" comment="XROUTE endpoint bypass"
}
:if ([:len [/ip/firewall/mangle/find where comment="XROUTE mark connection"]] = 0) do={
  /ip/firewall/mangle/add chain="xroute-divert" action=mark-connection connection-state=new connection-mark=no-mark new-connection-mark="xroute-managed" passthrough=yes comment="XROUTE mark connection"
}
:if ([:len [/ip/firewall/mangle/find where comment="XROUTE mark routing"]] = 0) do={
  /ip/firewall/mangle/add chain="xroute-divert" action=mark-routing connection-mark="xroute-managed" new-routing-mark=$"SB_ROUTING_TABLE" passthrough=no comment="XROUTE mark routing"
}
:local endpointBypass [/ip/firewall/mangle/find where comment="XROUTE endpoint bypass"]
:local markConnection [/ip/firewall/mangle/find where comment="XROUTE mark connection"]
:local markRouting [/ip/firewall/mangle/find where comment="XROUTE mark routing"]
:if (([:len $endpointBypass] != 1) || ([:len $markConnection] != 1) || ([:len $markRouting] != 1)) do={ :error "XROUTE: diversion rules are missing or ambiguous" }
/ip/firewall/mangle/set $endpointBypass chain="xroute-divert" action=return dst-address-list="SB_BYPASS_ENDPOINTS" disabled=no
/ip/firewall/mangle/set $markConnection chain="xroute-divert" action=mark-connection connection-state=new connection-mark=no-mark new-connection-mark="xroute-managed" passthrough=yes disabled=no
/ip/firewall/mangle/set $markRouting chain="xroute-divert" action=mark-routing connection-mark="xroute-managed" new-routing-mark=$"SB_ROUTING_TABLE" passthrough=no disabled=no
:local firstMangleRules [/ip/firewall/mangle/find where dynamic=no]
:if (([:len $firstMangleRules] > 0) && ([:pick $firstMangleRules 0 1] != $diversionGate)) do={ /ip/firewall/mangle/move $diversionGate destination=[:pick $firstMangleRules 0 1] }
/ip/firewall/mangle/move $markConnection destination=$markRouting
/ip/firewall/mangle/move $endpointBypass destination=$markConnection

# Local DNS uses the same readiness gate as public traffic. The local/internal
# returns keep all other router and LAN traffic outside policy routing.
:foreach dnsProtocol in={"udp";"tcp"} do={
  :local dnsSuffix "UDP"
  :if ($dnsProtocol = "tcp") do={ :set dnsSuffix "TCP" }
  :local markComment ("XROUTE managed DNS mark " . $dnsSuffix)
  :if ([:len [/ip/firewall/mangle/find where comment=$markComment]] = 0) do={ /ip/firewall/mangle/add chain="xroute-divert" action=mark-connection new-connection-mark="xroute-managed" passthrough=yes comment=$markComment disabled=yes }
  :local dnsMark [/ip/firewall/mangle/find where comment=$markComment]
  :if ([:len $dnsMark] != 1) do={ :error "XROUTE managed DNS mark missing or ambiguous" }
  /ip/firewall/mangle/set $dnsMark chain="xroute-divert" action=mark-connection connection-state=new connection-mark=no-mark in-interface-list=!WAN dst-address-type=local protocol=$dnsProtocol dst-port=53 new-connection-mark="xroute-managed" passthrough=yes disabled=no
  :local natComment ("XROUTE managed DNS " . $dnsSuffix)
  :if ([:len [/ip/firewall/nat/find where comment=$natComment]] = 0) do={ /ip/firewall/nat/add chain=dstnat action=dst-nat to-addresses=$"SB_CONTAINER_IP" comment=$natComment disabled=yes }
  :local dnsNat [/ip/firewall/nat/find where comment=$natComment]
  :if ([:len $dnsNat] != 1) do={ :error "XROUTE managed DNS NAT missing or ambiguous" }
  /ip/firewall/nat/unset $dnsNat dst-address-type
  /ip/firewall/nat/set $dnsNat chain=dstnat action=dst-nat src-address-list="SB_MANAGED_CLIENTS" in-interface-list=!WAN connection-mark="xroute-managed" protocol=$dnsProtocol dst-port=53 to-addresses=$"SB_CONTAINER_IP" to-ports=1053 disabled=no
  :local firstDNSNat [/ip/firewall/nat/find where dynamic=no]
  :if (([:len $firstDNSNat] > 0) && ([:pick $firstDNSNat 0 1] != $dnsNat)) do={ /ip/firewall/nat/move $dnsNat destination=[:pick $firstDNSNat 0 1] }
}
:foreach bypassKind in={"local";"internal"} do={
  :local bypassComment ("XROUTE diversion " . $bypassKind . " bypass")
  :if ([:len [/ip/firewall/mangle/find where comment=$bypassComment]] = 0) do={ /ip/firewall/mangle/add chain="xroute-divert" action=return comment=$bypassComment }
  :local dnsBypass [/ip/firewall/mangle/find where comment=$bypassComment]
  :if ([:len $dnsBypass] != 1) do={ :error "XROUTE diversion bypass missing or ambiguous" }
  :if ($bypassKind = "local") do={ /ip/firewall/mangle/set $dnsBypass chain="xroute-divert" action=return dst-address-type=local disabled=no } else={ /ip/firewall/mangle/set $dnsBypass chain="xroute-divert" action=return dst-address-list="SB_INTERNAL_NETWORKS" disabled=no }
}
:local dnsBefore $endpointBypass
:foreach dnsComment in={"XROUTE diversion internal bypass";"XROUTE diversion local bypass";"XROUTE managed DNS mark TCP";"XROUTE managed DNS mark UDP"} do={
  :local dnsRule [/ip/firewall/mangle/find where comment=$dnsComment]
  /ip/firewall/mangle/move $dnsRule destination=$dnsBefore
  :set dnsBefore $dnsRule
}

:if ([:len [/ip/firewall/nat/find where comment="XROUTE container WAN masquerade"]] = 0) do={
  /ip/firewall/nat/add chain=srcnat action=masquerade src-address=$"SB_CONTAINER_NETWORK" out-interface-list=WAN comment="XROUTE container WAN masquerade"
}
:if ([:len [/ip/firewall/nat/find where comment="XROUTE container LAN masquerade"]] = 0) do={
  /ip/firewall/nat/add chain=srcnat action=masquerade src-address=$"SB_CONTAINER_NETWORK" dst-address-list="SB_CONTAINER_ALLOWED_INTERNAL" comment="XROUTE container LAN masquerade"
}
:if ([:len [/ip/firewall/nat/find where comment="XROUTE WireGuard panel dstnat"]] = 0) do={
  /ip/firewall/nat/add chain=dstnat action=dst-nat in-interface-list="SB_WIREGUARD_INGRESS" dst-address-type=local protocol=tcp dst-port=9443 to-addresses=$"SB_CONTAINER_IP" to-ports=9443 comment="XROUTE WireGuard panel dstnat"
}
:local containerMasquerade [/ip/firewall/nat/find where comment="XROUTE container WAN masquerade"]
:local containerLanMasquerade [/ip/firewall/nat/find where comment="XROUTE container LAN masquerade"]
:local wireguardPanelDstnat [/ip/firewall/nat/find where comment="XROUTE WireGuard panel dstnat"]
:if (([:len $containerMasquerade] != 1) || ([:len $containerLanMasquerade] != 1) || ([:len $wireguardPanelDstnat] != 1)) do={ :error "XROUTE: NAT rules are missing or ambiguous" }
/ip/firewall/nat/set $containerMasquerade chain=srcnat action=masquerade src-address=$"SB_CONTAINER_NETWORK" out-interface-list=WAN disabled=no
/ip/firewall/nat/set $containerLanMasquerade chain=srcnat action=masquerade src-address=$"SB_CONTAINER_NETWORK" dst-address-list="SB_CONTAINER_ALLOWED_INTERNAL" disabled=no
/ip/firewall/nat/set $wireguardPanelDstnat chain=dstnat action=dst-nat in-interface-list="SB_WIREGUARD_INGRESS" dst-address-type=local protocol=tcp dst-port=9443 to-addresses=$"SB_CONTAINER_IP" to-ports=9443 disabled=no
:local firstNatRules [/ip/firewall/nat/find where dynamic=no]
:if (([:len $firstNatRules] > 0) && ([:pick $firstNatRules 0 1] != $wireguardPanelDstnat)) do={ /ip/firewall/nat/move $wireguardPanelDstnat destination=[:pick $firstNatRules 0 1] }

# A single scoped jump is placed before existing user rules. Its custom chain
# returns unmatched packets, preserving the user's firewall and VPN policy.
:if ([:len [/ip/firewall/filter/find where comment="XROUTE forward policy jump"]] = 0) do={
  :local firstForward [/ip/firewall/filter/find where chain=forward and dynamic=no]
  :if ([:len $firstForward] > 0) do={
    /ip/firewall/filter/add chain=forward action=jump jump-target="xroute-forward" place-before=[:pick $firstForward 0 1] comment="XROUTE forward policy jump"
  } else={
    /ip/firewall/filter/add chain=forward action=jump jump-target="xroute-forward" comment="XROUTE forward policy jump"
  }
}
:if ([:len [/ip/firewall/filter/find where comment="XROUTE WireGuard panel allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface-list="SB_WIREGUARD_INGRESS" dst-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=9443 comment="XROUTE WireGuard panel allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE management allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface-list="SB_MANAGEMENT_INGRESS" src-address-list="SB_MANAGEMENT_SOURCES" dst-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=9443 comment="XROUTE management allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE management deny"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=drop dst-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=9443 comment="XROUTE management deny" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE managed to container"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept src-address-list="SB_MANAGED_CLIENTS" out-interface=$"SB_BRIDGE" comment="XROUTE managed to container" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE container return to managed"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" dst-address-list="SB_MANAGED_CLIENTS" connection-state=established,related comment="XROUTE container return to managed" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE container return to management"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" dst-address-list="SB_MANAGEMENT_SOURCES" connection-state=established,related comment="XROUTE container return to management" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE WireGuard panel return"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" out-interface-list="SB_WIREGUARD_INGRESS" src-address=$"SB_CONTAINER_IP" protocol=tcp src-port=9443 connection-state=established,related connection-nat-state=dstnat comment="XROUTE WireGuard panel return" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE container explicit LAN allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" dst-address-list="SB_CONTAINER_ALLOWED_INTERNAL" comment="XROUTE container explicit LAN allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE container LAN deny"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=drop in-interface=$"SB_BRIDGE" dst-address-list="SB_INTERNAL_NETWORKS" comment="XROUTE container LAN deny" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE container WAN allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" out-interface-list=WAN comment="XROUTE container WAN allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE fail-closed public drop"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=drop src-address-list="SB_FAIL_CLOSED_CLIENTS" dst-address-list="!SB_INTERNAL_NETWORKS" comment="XROUTE fail-closed public drop" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE forward policy return"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=return comment="XROUTE forward policy return" }
:local forwardJump [/ip/firewall/filter/find where comment="XROUTE forward policy jump"]
:local wireguardPanelAllow [/ip/firewall/filter/find where comment="XROUTE WireGuard panel allow"]
:local managementAllow [/ip/firewall/filter/find where comment="XROUTE management allow"]
:local managementDeny [/ip/firewall/filter/find where comment="XROUTE management deny"]
:local managedToContainer [/ip/firewall/filter/find where comment="XROUTE managed to container"]
:local containerReturn [/ip/firewall/filter/find where comment="XROUTE container return to managed"]
:local containerManagementReturn [/ip/firewall/filter/find where comment="XROUTE container return to management"]
:local wireguardPanelReturn [/ip/firewall/filter/find where comment="XROUTE WireGuard panel return"]
:local explicitLanAllow [/ip/firewall/filter/find where comment="XROUTE container explicit LAN allow"]
:local containerLanDeny [/ip/firewall/filter/find where comment="XROUTE container LAN deny"]
:local containerWanAllow [/ip/firewall/filter/find where comment="XROUTE container WAN allow"]
:local failClosedDrop [/ip/firewall/filter/find where comment="XROUTE fail-closed public drop"]
:local forwardReturn [/ip/firewall/filter/find where comment="XROUTE forward policy return"]
:if (([:len $forwardJump] != 1) || ([:len $wireguardPanelAllow] != 1) || ([:len $managementAllow] != 1) || ([:len $managementDeny] != 1) || ([:len $managedToContainer] != 1) || ([:len $containerReturn] != 1) || ([:len $containerManagementReturn] != 1) || ([:len $wireguardPanelReturn] != 1) || ([:len $explicitLanAllow] != 1) || ([:len $containerLanDeny] != 1) || ([:len $containerWanAllow] != 1) || ([:len $failClosedDrop] != 1) || ([:len $forwardReturn] != 1)) do={ :error "XROUTE: forward filter rules are missing or ambiguous" }
/ip/firewall/filter/set $forwardJump chain=forward action=jump jump-target="xroute-forward" disabled=no
/ip/firewall/filter/set $wireguardPanelAllow chain="xroute-forward" action=accept in-interface-list="SB_WIREGUARD_INGRESS" dst-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=9443 disabled=no
/ip/firewall/filter/set $managementAllow chain="xroute-forward" action=accept in-interface-list="SB_MANAGEMENT_INGRESS" src-address-list="SB_MANAGEMENT_SOURCES" dst-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=9443 disabled=no
/ip/firewall/filter/set $managementDeny chain="xroute-forward" action=drop dst-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=9443 disabled=no
/ip/firewall/filter/unset $managedToContainer in-interface-list
/ip/firewall/filter/set $managedToContainer chain="xroute-forward" action=accept src-address-list="SB_MANAGED_CLIENTS" out-interface=$"SB_BRIDGE" disabled=no
/ip/firewall/filter/set $containerReturn chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" dst-address-list="SB_MANAGED_CLIENTS" connection-state=established,related disabled=no
/ip/firewall/filter/set $containerManagementReturn chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" dst-address-list="SB_MANAGEMENT_SOURCES" connection-state=established,related disabled=no
/ip/firewall/filter/set $wireguardPanelReturn chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" out-interface-list="SB_WIREGUARD_INGRESS" src-address=$"SB_CONTAINER_IP" protocol=tcp src-port=9443 connection-state=established,related connection-nat-state=dstnat disabled=no
/ip/firewall/filter/set $explicitLanAllow chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" dst-address-list="SB_CONTAINER_ALLOWED_INTERNAL" disabled=no
/ip/firewall/filter/set $containerLanDeny chain="xroute-forward" action=drop in-interface=$"SB_BRIDGE" dst-address-list="SB_INTERNAL_NETWORKS" disabled=no
/ip/firewall/filter/set $containerWanAllow chain="xroute-forward" action=accept in-interface=$"SB_BRIDGE" out-interface-list=WAN disabled=no
/ip/firewall/filter/set $failClosedDrop chain="xroute-forward" action=drop src-address-list="SB_FAIL_CLOSED_CLIENTS" dst-address-list="!SB_INTERNAL_NETWORKS" disabled=no
/ip/firewall/filter/set $forwardReturn chain="xroute-forward" action=return disabled=no
:local firstFilterRules [/ip/firewall/filter/find where dynamic=no]
:if (([:len $firstFilterRules] > 0) && ([:pick $firstFilterRules 0 1] != $forwardJump)) do={ /ip/firewall/filter/move $forwardJump destination=[:pick $firstFilterRules 0 1] }
/ip/firewall/filter/move $containerWanAllow destination=$forwardReturn
/ip/firewall/filter/move $failClosedDrop destination=$forwardReturn
/ip/firewall/filter/move $containerLanDeny destination=$containerWanAllow
/ip/firewall/filter/move $explicitLanAllow destination=$containerLanDeny
/ip/firewall/filter/move $containerManagementReturn destination=$explicitLanAllow
/ip/firewall/filter/move $wireguardPanelReturn destination=$explicitLanAllow
/ip/firewall/filter/move $containerReturn destination=$containerManagementReturn
/ip/firewall/filter/move $managedToContainer destination=$containerReturn
/ip/firewall/filter/move $managementDeny destination=$managedToContainer
/ip/firewall/filter/move $managementAllow destination=$managementDeny
/ip/firewall/filter/move $wireguardPanelAllow destination=$managementAllow

:if ([:len [/ip/firewall/filter/find where comment="XROUTE input policy jump"]] = 0) do={
  :local firstInput [/ip/firewall/filter/find where chain=input and dynamic=no]
  :if ([:len $firstInput] > 0) do={
    /ip/firewall/filter/add chain=input action=jump jump-target="xroute-input" place-before=[:pick $firstInput 0 1] comment="XROUTE input policy jump"
  } else={
    /ip/firewall/filter/add chain=input action=jump jump-target="xroute-input" comment="XROUTE input policy jump"
  }
}
:local sshService [/ip/service/find where name="ssh" and dynamic=no]
:local winboxService [/ip/service/find where name="winbox" and dynamic=no]
:if (([:len $sshService] != 1) || ([:len $winboxService] != 1)) do={ :error "XROUTE: RouterOS SSH or Winbox service is missing or ambiguous; services were not changed" }
:local remoteManagementPorts (([/ip/service/get $sshService port]) . "," . $"SB_ROUTEROS_REST_PORT" . "," . ([/ip/service/get $winboxService port]))
:foreach obsoleteComment in={"XROUTE RouterOS www-ssl management allow";"XROUTE RouterOS www-ssl deny"} do={ :local obsoleteRules [/ip/firewall/filter/find where comment=$obsoleteComment]; :if ([:len $obsoleteRules] > 1) do={ :error ("XROUTE obsolete management rule is ambiguous: " . $obsoleteComment) }; :if ([:len $obsoleteRules] = 1) do={ /ip/firewall/filter/remove $obsoleteRules } }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE RouterOS REST allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=tcp dst-port=$"SB_ROUTEROS_REST_PORT" comment="XROUTE RouterOS REST allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE RouterOS remote full management allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=$remoteManagementPorts comment="XROUTE RouterOS remote full management allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE RouterOS DNS UDP allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=udp dst-port=53 comment="XROUTE RouterOS DNS UDP allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE RouterOS DNS TCP allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=tcp dst-port=53 comment="XROUTE RouterOS DNS TCP allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE RouterOS ICMP allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=icmp comment="XROUTE RouterOS ICMP allow" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE container established input"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" connection-state=established,related comment="XROUTE container established input" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE RouterOS management deny"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=drop src-address=$"SB_CONTAINER_IP" comment="XROUTE RouterOS management deny" }
:if ([:len [/ip/firewall/filter/find where comment="XROUTE input policy return"]] = 0) do={ /ip/firewall/filter/add chain="xroute-input" action=return comment="XROUTE input policy return" }
:local inputJump [/ip/firewall/filter/find where comment="XROUTE input policy jump"]
:local restAllow [/ip/firewall/filter/find where comment="XROUTE RouterOS REST allow"]
:local remoteFullManagementAllow [/ip/firewall/filter/find where comment="XROUTE RouterOS remote full management allow"]
:local dnsUdpAllow [/ip/firewall/filter/find where comment="XROUTE RouterOS DNS UDP allow"]
:local dnsTcpAllow [/ip/firewall/filter/find where comment="XROUTE RouterOS DNS TCP allow"]
:local icmpAllow [/ip/firewall/filter/find where comment="XROUTE RouterOS ICMP allow"]
:local containerEstablishedInput [/ip/firewall/filter/find where comment="XROUTE container established input"]
:local routerManagementDeny [/ip/firewall/filter/find where comment="XROUTE RouterOS management deny"]
:local inputReturn [/ip/firewall/filter/find where comment="XROUTE input policy return"]
:if (([:len $inputJump] != 1) || ([:len $restAllow] != 1) || ([:len $remoteFullManagementAllow] != 1) || ([:len $dnsUdpAllow] != 1) || ([:len $dnsTcpAllow] != 1) || ([:len $icmpAllow] != 1) || ([:len $containerEstablishedInput] != 1) || ([:len $routerManagementDeny] != 1) || ([:len $inputReturn] != 1)) do={ :error "XROUTE: RouterOS input rules are missing or ambiguous" }
/ip/firewall/filter/set $inputJump chain=input action=jump jump-target="xroute-input" disabled=no
/ip/firewall/filter/set $restAllow chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=tcp dst-port=$"SB_ROUTEROS_REST_PORT" disabled=no
/ip/firewall/filter/set $remoteFullManagementAllow chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" protocol=tcp dst-port=$remoteManagementPorts disabled=no
/ip/firewall/filter/set $dnsUdpAllow chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=udp dst-port=53 disabled=no
/ip/firewall/filter/set $dnsTcpAllow chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=tcp dst-port=53 disabled=no
/ip/firewall/filter/set $icmpAllow chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" dst-address=$"SB_ROUTER_IP" protocol=icmp disabled=no
/ip/firewall/filter/set $containerEstablishedInput chain="xroute-input" action=accept in-interface=$"SB_BRIDGE" src-address=$"SB_CONTAINER_IP" connection-state=established,related disabled=no
/ip/firewall/filter/set $routerManagementDeny chain="xroute-input" action=drop src-address=$"SB_CONTAINER_IP" disabled=no
/ip/firewall/filter/set $inputReturn chain="xroute-input" action=return disabled=no
:set firstFilterRules [/ip/firewall/filter/find where dynamic=no]
:if (([:len $firstFilterRules] > 0) && ([:pick $firstFilterRules 0 1] != $inputJump)) do={ /ip/firewall/filter/move $inputJump destination=[:pick $firstFilterRules 0 1] }
/ip/firewall/filter/move $routerManagementDeny destination=$inputReturn
/ip/firewall/filter/move $containerEstablishedInput destination=$routerManagementDeny
/ip/firewall/filter/move $icmpAllow destination=$containerEstablishedInput
/ip/firewall/filter/move $dnsTcpAllow destination=$icmpAllow
/ip/firewall/filter/move $dnsUdpAllow destination=$dnsTcpAllow
/ip/firewall/filter/move $remoteFullManagementAllow destination=$dnsUdpAllow
/ip/firewall/filter/move $restAllow destination=$remoteFullManagementAllow

:if ($"SB_IPV6_MODE" = "block_managed") do={
  :foreach sbManagedCidrV6 in=$"SB_MANAGED_CLIENTS_V6" do={
    :local sbManagedTextV6 [:tostr $sbManagedCidrV6]
    :local sbManagedLookupV6 $sbManagedTextV6
    :local sbManagedHostSuffixV6 [:find $sbManagedTextV6 "/128"]
    :if (($sbManagedHostSuffixV6 != nil) && ([:pick $sbManagedTextV6 $sbManagedHostSuffixV6 [:len $sbManagedTextV6]] = "/128")) do={ :set sbManagedLookupV6 [:pick $sbManagedTextV6 0 $sbManagedHostSuffixV6] }
    :if ([:len [/ipv6/firewall/address-list/find where list="SB_MANAGED_CLIENTS_V6" and address=$sbManagedLookupV6]] = 0) do={ /ipv6/firewall/address-list/add list="SB_MANAGED_CLIENTS_V6" address=$sbManagedTextV6 comment="XROUTE managed IPv6 client" }
  }
  :foreach sbInternalCidrV6 in=$"SB_INTERNAL_NETWORKS_V6" do={
    :local sbInternalTextV6 [:tostr $sbInternalCidrV6]
    :local sbInternalLookupV6 $sbInternalTextV6
    :local sbInternalHostSuffixV6 [:find $sbInternalTextV6 "/128"]
    :if (($sbInternalHostSuffixV6 != nil) && ([:pick $sbInternalTextV6 $sbInternalHostSuffixV6 [:len $sbInternalTextV6]] = "/128")) do={ :set sbInternalLookupV6 [:pick $sbInternalTextV6 0 $sbInternalHostSuffixV6] }
    :if ([:len [/ipv6/firewall/address-list/find where list="SB_INTERNAL_NETWORKS_V6" and address=$sbInternalLookupV6]] = 0) do={ /ipv6/firewall/address-list/add list="SB_INTERNAL_NETWORKS_V6" address=$sbInternalTextV6 comment="XROUTE internal IPv6 network" }
  }
  :if ([:len [/ipv6/firewall/filter/find where comment="XROUTE managed IPv6 WAN block"]] = 0) do={ /ipv6/firewall/filter/add chain=forward action=drop src-address-list="SB_MANAGED_CLIENTS_V6" dst-address-list="!SB_INTERNAL_NETWORKS_V6" out-interface-list=WAN comment="XROUTE managed IPv6 WAN block" }
  :local managedIpv6Block [/ipv6/firewall/filter/find where comment="XROUTE managed IPv6 WAN block"]
  :if ([:len $managedIpv6Block] != 1) do={ :error "XROUTE: managed IPv6 WAN block is missing or ambiguous" }
  /ipv6/firewall/filter/set $managedIpv6Block chain=forward action=drop src-address-list="SB_MANAGED_CLIENTS_V6" dst-address-list="!SB_INTERNAL_NETWORKS_V6" out-interface-list=WAN disabled=no
  :local firstIpv6FilterRules [/ipv6/firewall/filter/find where dynamic=no]
  :if (([:len $firstIpv6FilterRules] > 0) && ([:pick $firstIpv6FilterRules 0 1] != $managedIpv6Block)) do={ /ipv6/firewall/filter/move $managedIpv6Block destination=[:pick $firstIpv6FilterRules 0 1] }
}

:local shouldStart true
:do {
  :local containerRunningNow [/container/get $containerId running]
  :if ($containerRunningNow = true) do={ :set shouldStart false }
} on-error={
  :do {
    :set shouldStart [/container/get $containerId stopped]
  } on-error={
    :do {
      :set legacyContainerStatus [/container/get $containerId status]
      :set shouldStart ($legacyContainerStatus != "running")
    } on-error={ :set shouldStart true }
  }
}
:if ($shouldStart = true) do={
  :local storageReady [:parse [/system/script/get $storageProbe source]]
  :if ([$storageReady $containerId] != true) do={ :error "XROUTE: container storage is not ready; start refused" }
  /container/start $containerId
}
/system/scheduler/enable $bootScheduler
/ip/firewall/mangle/disable [find where comment="XROUTE diversion-gate"]
:foreach sbFlow in=[/ip/firewall/connection/find where connection-mark="xroute-managed"] do={ :do { /ip/firewall/connection/remove $sbFlow } on-error={} }
:log warning "XROUTE: install complete in fail-open state; install the watchdog, then finish the first-launch wizard"
}
