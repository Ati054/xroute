package runtimeconfig

import (
	"fmt"
	"strconv"

	"github.com/Ati054/xroute/internal/managedidentity"
)

func rosQuote(value string) string { return strconv.Quote(value) }

// Checked once during Apply, before any renderer-owned state is changed.
// Conservative overlap detection never steals another service or NAT port.
func renderRouterOSPanelPortCheck(model RouterOSRenderModel) []string {
	if model.PanelPort == 0 {
		return []string{"# Router-local panel access is not configured."}
	}
	return []string{
		fmt.Sprintf(`:local panelPort %d`, model.PanelPort),
		`:local panelPortContains do={ :local spec [:tostr $1]; :local port $2; :if ([:len $spec] = 0) do={ :return true }; :foreach item in=[:toarray $spec] do={ :local dash [:find $item "-"]; :if ([:typeof $dash] = "nil") do={ :if ([:tonum $item] = $port) do={ :return true } } else={ :local low [:tonum [:pick $item 0 $dash]]; :local high [:tonum [:pick $item ($dash + 1) [:len $item]]]; :if (($port >= $low) && ($port <= $high)) do={ :return true } } }; :return false }`,
		managedidentity.Literal(`:foreach service in=[/ip/service/find where disabled=no and dynamic=no] do={ :local ports [/ip/service/get $service port]; :if (([:len [:tostr $ports]] > 0) && [$panelPortContains $ports $panelPort]) do={ :error ("XROUTE panel port " . $panelPort . " conflicts with service " . [/ip/service/get $service name] . "; choose another panel port") } }`),
		managedidentity.Literal(`:foreach rule in=[/ip/firewall/nat/find where disabled=no] do={ :local chain [/ip/firewall/nat/get $rule chain]; :local comment [/ip/firewall/nat/get $rule comment]; :if (($chain = "dstnat") && ([:pick $comment 0 13] != "XROUTE panel ")) do={ :local protocol [:tostr [/ip/firewall/nat/get $rule protocol]]; :if (($protocol = "tcp") || ($protocol = "6") || ([:len $protocol] = 0)) do={ :local ports [/ip/firewall/nat/get $rule dst-port]; :if ([$panelPortContains $ports $panelPort]) do={ :error ("XROUTE panel port " . $panelPort . " overlaps NAT " . $rule . " " . $comment . "; choose another panel port") } } } }`),
		managedidentity.Literal(`:foreach rule in=[/ip/firewall/nat/find where chain="sb-gateway-panel"] do={ :local comment [/ip/firewall/nat/get $rule comment]; :if (($comment != "XROUTE panel WAN return") && ($comment != "XROUTE panel target")) do={ :error "XROUTE panel chain contains an unowned rule" } }`),
	}
}

func renderRouterOSPanelIngress(model RouterOSRenderModel) []string {
	comments := []string{managedidentity.Literal("XROUTE panel LAN jump"), managedidentity.Literal("XROUTE panel WG jump"), managedidentity.Literal("XROUTE panel WAN return"), managedidentity.Literal("XROUTE panel target")}
	lines := []string{}
	// Missing setting is opt-in for existing installations and a precise rollback
	// target for the first Apply. Fresh installs still receive the direct :9443
	// WireGuard recovery path from install.rsc.
	if model.PanelPort == 0 {
		for _, comment := range comments {
			lines = append(lines, fmt.Sprintf(`/ip/firewall/nat/remove [find where comment=%s]`, rosQuote(comment)))
		}
		return lines
	}
	// WireGuard membership and the symmetric forward path protect both the
	// router-local port and the direct recovery endpoint on container :9443.
	lines = append(lines,
		managedidentity.Literal(`:if ([:len [/interface/list/find where name="SB_WIREGUARD_INGRESS"]] = 0) do={ /interface/list/add name="SB_WIREGUARD_INGRESS" comment="XROUTE WireGuard panel ingress list" }`),
		managedidentity.Literal(`:local panelWireGuardList [/interface/list/find where name="SB_WIREGUARD_INGRESS" and comment="XROUTE WireGuard panel ingress list"]`),
		managedidentity.Literal(`:if ([:len $panelWireGuardList] != 1) do={ :error "XROUTE WireGuard panel ingress interface-list is missing or ambiguous" }`),
		managedidentity.Literal(`:foreach member in=[/interface/list/member/find where list="SB_WIREGUARD_INGRESS"] do={ :if ([/interface/list/member/get $member comment] != "XROUTE WireGuard panel ingress") do={ :error "XROUTE refusing to alter an unowned WireGuard panel ingress member" } }`),
		managedidentity.Literal(`/interface/list/member/remove [find where list="SB_WIREGUARD_INGRESS" and comment="XROUTE WireGuard panel ingress"]`),
		managedidentity.Literal(`:foreach wireguard in=[/interface/wireguard/find where disabled=no] do={ /interface/list/member/add list="SB_WIREGUARD_INGRESS" interface=[/interface/wireguard/get $wireguard name] comment="XROUTE WireGuard panel ingress" }`),
		managedidentity.Literal(`:local managementDeny [/ip/firewall/filter/find where comment="XROUTE management deny"]`),
		managedidentity.Literal(`:local containerLanDeny [/ip/firewall/filter/find where comment="XROUTE container LAN deny"]`),
		managedidentity.Literal(`:if (([:len $managementDeny] != 1) || ([:len $containerLanDeny] != 1)) do={ :error "XROUTE panel forward anchors are missing or ambiguous" }`),
		managedidentity.Literal(`:local wireguardPanelAllow [/ip/firewall/filter/find where comment="XROUTE WireGuard panel allow"]`),
		managedidentity.Literal(`:if ([:len $wireguardPanelAllow] > 1) do={ :error "XROUTE WireGuard panel allow is ambiguous" }`),
		fmt.Sprintf(managedidentity.Literal(`:if ([:len $wireguardPanelAllow] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface-list="SB_WIREGUARD_INGRESS" dst-address=%s protocol=tcp dst-port=9443 place-before=$managementDeny comment="XROUTE WireGuard panel allow"; :set wireguardPanelAllow [/ip/firewall/filter/find where comment="XROUTE WireGuard panel allow"] }`), rosQuote(model.ContainerIP)),
		fmt.Sprintf(managedidentity.Literal(`/ip/firewall/filter/set $wireguardPanelAllow chain="xroute-forward" action=accept in-interface-list="SB_WIREGUARD_INGRESS" dst-address=%s protocol=tcp dst-port=9443 disabled=no`), rosQuote(model.ContainerIP)),
		managedidentity.Literal(`:local wireguardPanelReturn [/ip/firewall/filter/find where comment="XROUTE WireGuard panel return"]`),
		managedidentity.Literal(`:if ([:len $wireguardPanelReturn] > 1) do={ :error "XROUTE WireGuard panel return is ambiguous" }`),
		fmt.Sprintf(managedidentity.Literal(`:if ([:len $wireguardPanelReturn] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=%s out-interface-list="SB_WIREGUARD_INGRESS" src-address=%s protocol=tcp src-port=9443 connection-state=established,related connection-nat-state=dstnat place-before=$containerLanDeny comment="XROUTE WireGuard panel return"; :set wireguardPanelReturn [/ip/firewall/filter/find where comment="XROUTE WireGuard panel return"] }`), rosQuote(model.BridgeName), rosQuote(model.ContainerIP)),
		fmt.Sprintf(managedidentity.Literal(`/ip/firewall/filter/set $wireguardPanelReturn chain="xroute-forward" action=accept in-interface=%s out-interface-list="SB_WIREGUARD_INGRESS" src-address=%s protocol=tcp src-port=9443 connection-state=established,related connection-nat-state=dstnat disabled=no`), rosQuote(model.BridgeName), rosQuote(model.ContainerIP)),
		`/ip/firewall/filter/move $wireguardPanelAllow destination=$managementDeny`,
		`/ip/firewall/filter/move $wireguardPanelReturn destination=$containerLanDeny`,
	)
	properties := []string{
		fmt.Sprintf(`chain=dstnat action=jump jump-target="sb-gateway-panel" in-interface-list="SB_MANAGEMENT_INGRESS" src-address-list="SB_MANAGEMENT_SOURCES" dst-address-type=local protocol=tcp dst-port=%d`, model.PanelPort),
		fmt.Sprintf(`chain=dstnat action=jump jump-target="sb-gateway-panel" in-interface-list="SB_WIREGUARD_INGRESS" dst-address-type=local protocol=tcp dst-port=%d`, model.PanelPort),
		`chain="sb-gateway-panel" action=return in-interface-list=WAN`,
		fmt.Sprintf(`chain="sb-gateway-panel" action=dst-nat protocol=tcp to-addresses=%s to-ports=9443`, rosQuote(model.ContainerIP)),
	}
	for index, comment := range comments {
		lines = append(lines,
			fmt.Sprintf(`:local panelRule%d [/ip/firewall/nat/find where comment=%s]`, index, rosQuote(comment)),
			fmt.Sprintf(managedidentity.Literal(`:if ([:len $panelRule%d] > 1) do={ :error "XROUTE panel NAT rule is ambiguous" }`), index),
			fmt.Sprintf(`:if ([:len $panelRule%d] = 0) do={ /ip/firewall/nat/add %s comment=%s; :set panelRule%d [/ip/firewall/nat/find where comment=%s] }`, index, properties[index], rosQuote(comment), index, rosQuote(comment)),
			fmt.Sprintf(`/ip/firewall/nat/set $panelRule%d %s disabled=no`, index, properties[index]),
		)
	}
	lines = append(lines, `/ip/firewall/nat/move $panelRule2 destination=$panelRule3`)
	// Only our two narrow jumps move. Foreign rules retain their relative order.
	for _, index := range []int{1, 0} {
		lines = append(lines, fmt.Sprintf(`:local panelFirst%d [:pick [/ip/firewall/nat/find where dynamic=no] 0 1]; :if (([:len $panelFirst%d] > 0) && ($panelFirst%d != $panelRule%d)) do={ /ip/firewall/nat/move $panelRule%d destination=$panelFirst%d }`, index, index, index, index, index, index))
	}
	return lines
}
