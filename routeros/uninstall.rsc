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
    :local xrouteCompatibilityFile ([:pick $xrouteOwnedRoot 0 $xrouteLastSlash] . "/legacy/uninstall.rsc")
    :if ([:len [/file/find where name=$xrouteCompatibilityFile]] != 1) do={ :error "XRoute: upload the matching legacy installer directory beside the existing installation" }
    /import file-name=$xrouteCompatibilityFile
    :set xrouteDelegated true
  }
}
# XRoute namespace compatibility guard: end

:if ($xrouteDelegated != true) do={

# XRoute complete uninstall. Set the exact dedicated project-storage
# directory used by this installation before importing this file. The script
# refuses internal flash and a bare disk root. System-disk directories require x86 CHR.

# Service log preflight.
:if (([:len [/system/logging/find where comment~"^XROUTE auth-log v1 "]] > 0) || ([:len [/system/logging/find where disabled=no and regex="^(\$|[^u])"]] > 0)) do={
  :error "XROUTE: restore service account logging before manual removal; see docs/ROUTEROS.md"
}
# End service log preflight.

:global "SB_STORAGE_ROOT"
:local storageRoot [:tostr $"SB_STORAGE_ROOT"]
:if (([:len $storageRoot] < 3) || ($storageRoot ~ "[^a-zA-Z0-9_./-]") || ([:pick $storageRoot 0 1] = "/") || ([:pick $storageRoot ([:len $storageRoot] - 1) [:len $storageRoot]] = "/") || ([:typeof [:find $storageRoot ".."]] != "nil") || ([:typeof [:find $storageRoot "//"]] != "nil") || ($storageRoot ~ "^[fF][lL][aA][sS][hH]")) do={
  :error "XRoute: SB_STORAGE_ROOT must be a dedicated project directory"
}
:if ([:typeof [:find $storageRoot "/"]] = "nil") do={
  :local hostArchitecture [/system/resource/get architecture-name]
  :if (([:pick [/system/resource/get board-name] 0 3] != "CHR") || (($hostArchitecture != "x86") && ($hostArchitecture != "x86_64"))) do={ :error "XRoute: a system-disk project directory is supported only on x86 CHR" }
  :if (($storageRoot ~ "^(disk|usb|slot|sata|nvme)[0-9]+\$") || ($storageRoot ~ "^(skins|pub|hotspot|web-proxy)\$")) do={ :error "XRoute: select a dedicated project directory, not a disk or system directory" }
}
:if ([:len [/file/find where name=$storageRoot]] != 1) do={
  :error "XROUTE: exact project storage directory is missing or ambiguous"
}

:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]
:if ([:len $gate] = 1) do={ /ip/firewall/mangle/disable $gate }
:foreach sbFlow in=[/ip/firewall/connection/find where connection-mark="xroute-managed"] do={ :do { /ip/firewall/connection/remove $sbFlow } on-error={} }

:foreach state in=[/system/script/find where comment~"^XROUTE WG egress peer state "] do={
  :local stateData [/system/script/get $state source]
  :local separator [:find $stateData "|"]
  :if (($separator = nil) || ($separator < 2)) do={ :error "XROUTE: WireGuard peer restore state is invalid" }
  :local peerKey [:pick $stateData 1 $separator]
  :local originalAllowed [:pick $stateData ($separator + 1) [:len $stateData]]
  :local peer [/interface/wireguard/peers/find where public-key=$peerKey]
  :if ([:len $peer] != 1) do={ :error "XROUTE: WireGuard peer restore target is missing or ambiguous" }
  /interface/wireguard/peers/set $peer allowed-address=$originalAllowed
}

/system/scheduler/remove [find where comment~"^XROUTE "]
/system/script/remove [find where comment~"^XROUTE "]
/ip/firewall/mangle/remove [find where comment~"^XROUTE "]
/ip/firewall/filter/remove [find where comment~"^XROUTE "]
/ip/firewall/nat/remove [find where comment~"^XROUTE "]
/ip/firewall/address-list/remove [find where list="SB_PUBLIC_ABUSE"]
/ip/firewall/address-list/remove [find where comment~"^XROUTE "]
:do { /ipv6/firewall/filter/remove [find where comment~"^XROUTE "] } on-error={}
:do { /ipv6/firewall/address-list/remove [find where comment~"^XROUTE "] } on-error={}
/ip/route/remove [find where comment~"^XROUTE "]
:do { /interface/list/member/remove [find where comment~"^XROUTE "] } on-error={}

:local containerId [/container/find where comment="XROUTE container"]
:if ([:len $containerId] = 1) do={
  :do { /container/stop $containerId } on-error={}
  :local stopped false
  :local stopAttempt 0
  :while (($stopped = false) && ($stopAttempt < 60)) do={
    :do { :if ([/container/get $containerId running] = false) do={ :set stopped true } } on-error={}
    :do { :if ([/container/get $containerId stopped] = true) do={ :set stopped true } } on-error={}
    :do { :if ([/container/get $containerId status] = "stopped") do={ :set stopped true } } on-error={}
    :if ($stopped = false) do={ :set stopAttempt ($stopAttempt + 1); :delay 2s }
  }
  :if ($stopped = false) do={ :error "XROUTE: container did not stop; storage was preserved" }
  /container/remove $containerId
}
/container/envs/remove [find where comment~"^XROUTE "]
/container/mounts/remove [find where comment~"^XROUTE "]
/ip/address/remove [find where comment~"^XROUTE "]
/interface/bridge/port/remove [find where comment~"^XROUTE "]
/interface/veth/remove [find where comment~"^XROUTE "]
/interface/bridge/remove [find where comment~"^XROUTE "]
/interface/list/remove [find where comment~"^XROUTE "]
/routing/table/remove [find where comment="XROUTE routing table"]
/user/remove [find where comment="XROUTE control-plane REST user"]
/user/group/remove [find where comment="XROUTE REST group"]
/file/remove [find where name~"^sb-gateway-(before-|pre-)"]

:local storageEntry [/file/find where name=$storageRoot]
:if ([:len $storageEntry] = 1) do={ /file/remove $storageEntry }
:log warning "XROUTE: full uninstall complete; foreign RouterOS objects were preserved"
}
