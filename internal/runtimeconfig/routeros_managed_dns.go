package runtimeconfig

import (
	"fmt"
	"strings"

	"github.com/Ati054/xroute/internal/managedidentity"
)

func renderRouterOSManagedDNS(model RouterOSRenderModel) []string {
	disabled := "yes"
	if model.ManagedDNS {
		disabled = "no"
	}
	lines := []string{}
	// Only the shared readiness gate admits packets into this chain. DNS
	// receives a connection mark, but local traffic never gets a routing mark.
	for _, proto := range []string{"udp", "tcp"} {
		comment := managedidentity.Literal("XROUTE managed DNS mark ") + strings.ToUpper(proto)
		fields := fmt.Sprintf(managedidentity.Literal(`chain="xroute-divert" action=mark-connection connection-state=new connection-mark=no-mark in-interface-list=!WAN dst-address-type=local protocol=%s dst-port=53 new-connection-mark="xroute-managed" passthrough=yes disabled=%s`), proto, disabled)
		lines = append(lines,
			fmt.Sprintf(`:if ([:len [/ip/firewall/mangle/find where comment="%s"]] = 0) do={ /ip/firewall/mangle/add %s comment="%s" }`, comment, fields, comment),
			fmt.Sprintf(`:local dnsMark [/ip/firewall/mangle/find where comment="%s"]`, comment),
			managedidentity.Literal(`:if ([:len $dnsMark] != 1) do={ :error "XROUTE managed DNS mark missing or ambiguous" }`),
			`/ip/firewall/mangle/set $dnsMark `+fields,
		)
	}
	for _, bypass := range []struct{ name, fields string }{
		{"local", `dst-address-type=local`},
		{"internal", `dst-address-list="SB_INTERNAL_NETWORKS"`},
	} {
		comment := managedidentity.Literal("XROUTE diversion ") + bypass.name + " bypass"
		fields := managedidentity.Literal(`chain="xroute-divert" action=return disabled=no `) + bypass.fields
		lines = append(lines,
			fmt.Sprintf(`:if ([:len [/ip/firewall/mangle/find where comment="%s"]] = 0) do={ /ip/firewall/mangle/add %s comment="%s" }`, comment, fields, comment),
			fmt.Sprintf(`:local dnsBypass [/ip/firewall/mangle/find where comment="%s"]`, comment),
			managedidentity.Literal(`:if ([:len $dnsBypass] != 1) do={ :error "XROUTE diversion bypass missing or ambiguous" }`),
			`/ip/firewall/mangle/set $dnsBypass `+fields,
		)
	}
	// Move in reverse order to keep the original endpoint bypass next.
	lines = append(lines, `:local dnsBefore $endpointBypass`)
	for _, comment := range []string{managedidentity.Literal("XROUTE diversion internal bypass"), managedidentity.Literal("XROUTE diversion local bypass"), managedidentity.Literal("XROUTE managed DNS mark TCP"), managedidentity.Literal("XROUTE managed DNS mark UDP")} {
		lines = append(lines, fmt.Sprintf(`/ip/firewall/mangle/move [/ip/firewall/mangle/find where comment="%s"] destination=$dnsBefore`, comment))
		// Subsequent moves must precede the newly placed rule.
		lines = append(lines, fmt.Sprintf(`:set dnsBefore [/ip/firewall/mangle/find where comment="%s"]`, comment))
	}
	for _, proto := range []string{"udp", "tcp"} {
		comment := managedidentity.Literal("XROUTE managed DNS ") + strings.ToUpper(proto)
		// Source identity and the shared gate select eligible DNS. Public DNS
		// already receives sb-managed after internal/endpoint bypasses; send it
		// to the same Go listener instead of traversing the Xray TUN frontend.
		fields := fmt.Sprintf(managedidentity.Literal(`chain=dstnat action=dst-nat src-address-list="SB_MANAGED_CLIENTS" in-interface-list=!WAN connection-mark="xroute-managed" protocol=%s dst-port=53 to-addresses="%s" to-ports=%d disabled=%s`), proto, model.ContainerIP, ManagedDNSPort, disabled)
		lines = append(lines,
			fmt.Sprintf(`:if ([:len [/ip/firewall/nat/find where comment="%s"]] = 0) do={ /ip/firewall/nat/add %s comment="%s" }`, comment, fields, comment),
			fmt.Sprintf(`:local dnsNat [/ip/firewall/nat/find where comment="%s"]`, comment),
			managedidentity.Literal(`:if ([:len $dnsNat] != 1) do={ :error "XROUTE managed DNS NAT missing or ambiguous" }`),
			`/ip/firewall/nat/unset $dnsNat dst-address-type`,
			`/ip/firewall/nat/set $dnsNat `+fields,
			`:local firstDNSNat [/ip/firewall/nat/find]; :if (([:len $firstDNSNat] > 0) && ([:pick $firstDNSNat 0 1] != $dnsNat)) do={ /ip/firewall/nat/move $dnsNat destination=[:pick $firstDNSNat 0 1] }`,
		)
	}
	return lines
}
