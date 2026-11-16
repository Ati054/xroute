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
    :local xrouteCompatibilityFile ([:pick $xrouteOwnedRoot 0 $xrouteLastSlash] . "/legacy/rollback.rsc")
    :if ([:len [/file/find where name=$xrouteCompatibilityFile]] != 1) do={ :error "XRoute: upload the matching legacy installer directory beside the existing installation" }
    /import file-name=$xrouteCompatibilityFile
    :set xrouteDelegated true
  }
}
# XRoute namespace compatibility guard: end

:if ($xrouteDelegated != true) do={

# XROUTE scoped rollback. It never wipes a table and never removes objects
# without the XROUTE comment/name. Existing VPNs, routes and firewall stay.

# Service log preflight.
:if (([:len [/system/logging/find where comment~"^XROUTE auth-log v1 "]] > 0) || ([:len [/system/logging/find where disabled=no and regex="^(\$|[^u])"]] > 0)) do={
  :error "XROUTE: restore service account logging before manual rollback; see docs/ROUTEROS.md"
}
# End service log preflight.

:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]
:if ([:len $gate] = 1) do={ /ip/firewall/mangle/disable $gate }
:foreach sbFlow in=[/ip/firewall/connection/find where connection-mark="xroute-managed"] do={ :do { /ip/firewall/connection/remove $sbFlow } on-error={} }

:if ([:len [/system/script/find where name="XROUTE-fasttrack-state"]] = 1) do={
  :local fasttrack [/ip/firewall/filter/find where action="fasttrack-connection" and disabled=no]
  :if ([:len $fasttrack] = 1) do={
    :if ([/ip/firewall/filter/get $fasttrack connection-mark] = "no-mark") do={
      /ip/firewall/filter/set $fasttrack connection-mark=""
      :log warning "XROUTE: restored the sole FastTrack connection-mark matcher to empty"
    }
  } else={
    :log error "XROUTE: FastTrack became ambiguous; matcher was not restored automatically"
  }
}

/system/scheduler/remove [find where comment~"^XROUTE "]
/system/script/remove [find where comment~"^XROUTE "]
/ip/firewall/mangle/remove [find where comment~"^XROUTE "]
/ip/firewall/filter/remove [find where comment~"^XROUTE "]
/ip/firewall/nat/remove [find where comment~"^XROUTE "]
/ip/firewall/address-list/remove [find where list="SB_PUBLIC_ABUSE"]
/ip/firewall/address-list/remove [find where comment~"^XROUTE "]
:do { /interface/list/member/remove [find where comment~"^XROUTE "] } on-error={}
:do { /interface/list/remove [find where comment="XROUTE management ingress list"] } on-error={}
:do { /ipv6/firewall/filter/remove [find where comment~"^XROUTE "] } on-error={}
:do { /ipv6/firewall/address-list/remove [find where comment~"^XROUTE "] } on-error={}
/ip/route/remove [find where comment~"^XROUTE "]

:local containerId [/container/find where comment="XROUTE container"]
:if ([:len $containerId] = 1) do={
  :do { /container/stop $containerId } on-error={}
  /container/remove $containerId
}
/container/envs/remove [find where comment~"^XROUTE "]
/container/mounts/remove [find where comment~"^XROUTE "]

/ip/address/remove [find where comment~"^XROUTE "]
/interface/bridge/port/remove [find where comment~"^XROUTE "]
/interface/veth/remove [find where comment~"^XROUTE "]
/interface/bridge/remove [find where comment~"^XROUTE "]
/routing/table/remove [find where comment="XROUTE routing table"]

/user/remove [find where comment="XROUTE control-plane REST user"]
/user/group/remove [find where comment="XROUTE REST group"]
:log warning "XROUTE: scoped rollback complete; backups/exports and USB data were preserved"
}
