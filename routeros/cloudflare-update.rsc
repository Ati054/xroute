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
    :local xrouteCompatibilityFile ([:pick $xrouteOwnedRoot 0 $xrouteLastSlash] . "/legacy/cloudflare-update.rsc")
    :if ([:len [/file/find where name=$xrouteCompatibilityFile]] != 1) do={ :error "XRoute: upload the matching legacy installer directory beside the existing installation" }
    /import file-name=$xrouteCompatibilityFile
    :set xrouteDelegated true
  }
}
# XRoute namespace compatibility guard: end

:if ($xrouteDelegated != true) do={

# XROUTE Cloudflare IPv4+IPv6 updater.
# Both official feeds are validated into staging lists before either live list
# changes. New entries are added before stale entries are removed, so origin
# filtering never has a fail-open gap and a bad download preserves both LKGs.

:if ([:len [/system/script/find where name="XROUTE-cloudflare-update"]] > 0) do={ /system/script/remove [find where name="XROUTE-cloudflare-update"] }
/system/script/add name="XROUTE-cloudflare-update" policy=read,write,test,ftp source={
  :local v4stage "SB_CLOUDFLARE_V4_NEXT"
  :local v6stage "SB_CLOUDFLARE_V6_NEXT"
  :local v4live "SB_CLOUDFLARE_V4"
  :local v6live "SB_CLOUDFLARE_V6"
  :local v4response
  :local v6response
  :local downloadsOk true

  :onerror sbCloudflareV4Error in={
    :set v4response [/tool/fetch url="https://www.cloudflare.com/ips-v4" output=user as-value idle-timeout=60s]
  } do={ :set downloadsOk false; :log error ("XROUTE: Cloudflare IPv4 fetch failed: " . $sbCloudflareV4Error) }
  :onerror sbCloudflareV6Error in={
    :set v6response [/tool/fetch url="https://www.cloudflare.com/ips-v6" output=user as-value idle-timeout=60s]
  } do={ :set downloadsOk false; :log error ("XROUTE: Cloudflare IPv6 fetch failed: " . $sbCloudflareV6Error) }
  :if (($downloadsOk = false) || (($v4response->"status") != "finished") || (($v6response->"status") != "finished")) do={
    :log error "XROUTE: Cloudflare feed fetch failed; keeping both current allow-lists"
  } else={

  /ip/firewall/address-list/remove [find where list=$v4stage and comment="XROUTE cloudflare staging v4"]
  /ipv6/firewall/address-list/remove [find where list=$v6stage and comment="XROUTE cloudflare staging v6"]
  :local v4data ($v4response->"data")
  :local v6data ($v6response->"data")
  :local v4accepted 0
  :local v6accepted 0

  :while ([:len $v4data] > 0) do={
    :local newline [:find $v4data "\n"]
    :local line ""
    :if ([:typeof $newline] = "nil") do={ :set line $v4data; :set v4data "" } else={
      :set line [:pick $v4data 0 $newline]
      :set v4data [:pick $v4data ($newline + 1) [:len $v4data]]
    }
    :if (([:len $line] > 0) && ([:pick $line ([:len $line] - 1) [:len $line]] = "\r")) do={ :set line [:pick $line 0 ([:len $line] - 1)] }
    :if (([:len $line] > 0) && ([:typeof [:find $line "/"]] != "nil") && ([:typeof [:find $line " "]] = "nil") && ([:typeof [:find $line ":"]] = "nil")) do={
      :do {
        /ip/firewall/address-list/add list=$v4stage address=$line comment="XROUTE cloudflare staging v4"
        :set v4accepted ($v4accepted + 1)
      } on-error={ :log error "XROUTE: invalid IPv4 CIDR in Cloudflare feed" }
    }
  }

  :while ([:len $v6data] > 0) do={
    :local newline [:find $v6data "\n"]
    :local line ""
    :if ([:typeof $newline] = "nil") do={ :set line $v6data; :set v6data "" } else={
      :set line [:pick $v6data 0 $newline]
      :set v6data [:pick $v6data ($newline + 1) [:len $v6data]]
    }
    :if (([:len $line] > 0) && ([:pick $line ([:len $line] - 1) [:len $line]] = "\r")) do={ :set line [:pick $line 0 ([:len $line] - 1)] }
    :if (([:len $line] > 0) && ([:typeof [:find $line "/"]] != "nil") && ([:typeof [:find $line " "]] = "nil") && ([:typeof [:find $line ":"]] != "nil")) do={
      :do {
        /ipv6/firewall/address-list/add list=$v6stage address=$line comment="XROUTE cloudflare staging v6"
        :set v6accepted ($v6accepted + 1)
      } on-error={ :log error "XROUTE: invalid IPv6 CIDR in Cloudflare feed" }
    }
  }

  :if (($v4accepted < 10) || ($v6accepted < 5)) do={
    /ip/firewall/address-list/remove [find where list=$v4stage and comment="XROUTE cloudflare staging v4"]
    /ipv6/firewall/address-list/remove [find where list=$v6stage and comment="XROUTE cloudflare staging v6"]
    :log error ("XROUTE: Cloudflare feed validation failed (v4=" . $v4accepted . ", v6=" . $v6accepted . "); keeping both current allow-lists")
  } else={

  :foreach staged in=[/ip/firewall/address-list/find where list=$v4stage and comment="XROUTE cloudflare staging v4"] do={
    :local cidr [/ip/firewall/address-list/get $staged address]
    :if ([:len [/ip/firewall/address-list/find where list=$v4live and address=$cidr and comment="XROUTE cloudflare active v4"]] = 0) do={
      /ip/firewall/address-list/add list=$v4live address=$cidr comment="XROUTE cloudflare active v4"
    }
  }
  :foreach staged in=[/ipv6/firewall/address-list/find where list=$v6stage and comment="XROUTE cloudflare staging v6"] do={
    :local cidr [/ipv6/firewall/address-list/get $staged address]
    :if ([:len [/ipv6/firewall/address-list/find where list=$v6live and address=$cidr and comment="XROUTE cloudflare active v6"]] = 0) do={
      /ipv6/firewall/address-list/add list=$v6live address=$cidr comment="XROUTE cloudflare active v6"
    }
  }

  :foreach current in=[/ip/firewall/address-list/find where list=$v4live and comment="XROUTE cloudflare active v4"] do={
    :local cidr [/ip/firewall/address-list/get $current address]
    :if ([:len [/ip/firewall/address-list/find where list=$v4stage and address=$cidr and comment="XROUTE cloudflare staging v4"]] = 0) do={ /ip/firewall/address-list/remove $current }
  }
  :foreach current in=[/ipv6/firewall/address-list/find where list=$v6live and comment="XROUTE cloudflare active v6"] do={
    :local cidr [/ipv6/firewall/address-list/get $current address]
    :if ([:len [/ipv6/firewall/address-list/find where list=$v6stage and address=$cidr and comment="XROUTE cloudflare staging v6"]] = 0) do={ /ipv6/firewall/address-list/remove $current }
  }

  /ip/firewall/address-list/remove [find where list=$v4stage and comment="XROUTE cloudflare staging v4"]
  /ipv6/firewall/address-list/remove [find where list=$v6stage and comment="XROUTE cloudflare staging v6"]
  :log info ("XROUTE: Cloudflare allow-lists updated (v4=" . $v4accepted . ", v6=" . $v6accepted . ")")
  }
  }
} comment="XROUTE Cloudflare updater"

:if ([:len [/system/scheduler/find where name="XROUTE-cloudflare-update"]] = 0) do={
  /system/scheduler/add name="XROUTE-cloudflare-update" interval=1d start-time=03:17:00 on-event="/system/script/run XROUTE-cloudflare-update" policy=read,write,test,ftp comment="XROUTE Cloudflare scheduler"
}
/system/scheduler/set [find where name="XROUTE-cloudflare-update"] policy=read,write,test,ftp
/system/script/run "XROUTE-cloudflare-update"
}
