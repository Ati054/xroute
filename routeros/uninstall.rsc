# XRoute complete uninstall. Set the exact dedicated project-storage
# directory used by this installation before importing this file. The script
# refuses internal flash and a bare disk root. System-disk directories require x86 CHR.

# Service log preflight.
:if (([:len [/system/logging/find where comment~"^SB-GATEWAY auth-log v1 "]] > 0) || ([:len [/system/logging/find where disabled=no and regex="^(\$|[^u])"]] > 0)) do={
  :error "SB-GATEWAY: restore service account logging before manual removal; see docs/ROUTEROS.md"
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
  :error "SB-GATEWAY: exact project storage directory is missing or ambiguous"
}

:local gate [/ip/firewall/mangle/find where comment="SB-GATEWAY diversion-gate"]
:if ([:len $gate] = 1) do={ /ip/firewall/mangle/disable $gate }
:foreach sbFlow in=[/ip/firewall/connection/find where connection-mark="sb-managed"] do={ :do { /ip/firewall/connection/remove $sbFlow } on-error={} }

:foreach state in=[/system/script/find where comment~"^SB-GATEWAY WG egress peer state "] do={
  :local stateData [/system/script/get $state source]
  :local separator [:find $stateData "|"]
  :if (($separator = nil) || ($separator < 2)) do={ :error "SB-GATEWAY: WireGuard peer restore state is invalid" }
  :local peerKey [:pick $stateData 1 $separator]
  :local originalAllowed [:pick $stateData ($separator + 1) [:len $stateData]]
  :local peer [/interface/wireguard/peers/find where public-key=$peerKey]
  :if ([:len $peer] != 1) do={ :error "SB-GATEWAY: WireGuard peer restore target is missing or ambiguous" }
  /interface/wireguard/peers/set $peer allowed-address=$originalAllowed
}

/system/scheduler/remove [find where comment~"^SB-GATEWAY "]
/system/script/remove [find where comment~"^SB-GATEWAY "]
/ip/firewall/mangle/remove [find where comment~"^SB-GATEWAY "]
/ip/firewall/filter/remove [find where comment~"^SB-GATEWAY "]
/ip/firewall/nat/remove [find where comment~"^SB-GATEWAY "]
/ip/firewall/address-list/remove [find where list="SB_PUBLIC_ABUSE"]
/ip/firewall/address-list/remove [find where comment~"^SB-GATEWAY "]
:do { /ipv6/firewall/filter/remove [find where comment~"^SB-GATEWAY "] } on-error={}
:do { /ipv6/firewall/address-list/remove [find where comment~"^SB-GATEWAY "] } on-error={}
/ip/route/remove [find where comment~"^SB-GATEWAY "]
:do { /interface/list/member/remove [find where comment~"^SB-GATEWAY "] } on-error={}

:local containerId [/container/find where comment="SB-GATEWAY container"]
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
  :if ($stopped = false) do={ :error "SB-GATEWAY: container did not stop; storage was preserved" }
  /container/remove $containerId
}
/container/envs/remove [find where comment~"^SB-GATEWAY "]
/container/mounts/remove [find where comment~"^SB-GATEWAY "]
/ip/address/remove [find where comment~"^SB-GATEWAY "]
/interface/bridge/port/remove [find where comment~"^SB-GATEWAY "]
/interface/veth/remove [find where comment~"^SB-GATEWAY "]
/interface/bridge/remove [find where comment~"^SB-GATEWAY "]
/interface/list/remove [find where comment~"^SB-GATEWAY "]
/routing/table/remove [find where comment="SB-GATEWAY routing table"]
/user/remove [find where comment="SB-GATEWAY control-plane REST user"]
/user/group/remove [find where comment="SB-GATEWAY REST group"]
/file/remove [find where name~"^sb-gateway-(before-|pre-)"]

:local storageEntry [/file/find where name=$storageRoot]
:if ([:len $storageEntry] = 1) do={ /file/remove $storageEntry }
:log warning "SB-GATEWAY: full uninstall complete; foreign RouterOS objects were preserved"
