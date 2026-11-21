package runtimeconfig

import (
	"fmt"
	"sort"
	"strings"

	routerosassets "github.com/Ati054/xroute/routeros"

	"github.com/Ati054/xroute/internal/managedidentity"
)

var routerOSCandidateHeader = managedidentity.Literal("# XROUTE generated candidate; managed objects only")
var routerOSSectionPrefix = managedidentity.Literal("# XROUTE section:")

var routerOSSectionOrder = []string{"panel-port-check", "public-port-check", "core", "dns", "watchdog", "address-lists", "protocol-policy", "outage-exceptions", "public-ingress", "panel-ingress", "wireguard-egress", "ipv6", "finalize"}

// RenderRouterOSTrafficCandidate renders the idempotent RouterOS traffic-path
// portion of the current runtime candidate. The script only alters
// this installation's owned objects and leaves the
// diversion gate fail-open until the watchdog confirms container readiness.
func RenderRouterOSTrafficCandidate(config map[string]any, nodes []map[string]any, ruleSetRoot ...string) (string, error) {
	if err := ValidateDedicatedSubscriptionOrigin(config); err != nil {
		return "", err
	}
	nodes, err := augmentRuntimeNodes(config, nodes)
	if err != nil {
		return "", err
	}
	model, err := BuildRouterOSRenderModel(config, nodes)
	if err != nil {
		return "", err
	}
	outage, err := routerOSOutageGroups(config, ruleSetRoot...)
	if err != nil {
		return "", err
	}
	sections := map[string][]string{
		"panel-port-check":  renderRouterOSPanelPortCheck(model),
		"public-port-check": renderRouterOSPublicPortCheck(model),
		"panel-ingress":     renderRouterOSPanelIngress(model),
		"core":              renderRouterOSCore(model),
		"dns":               renderRouterOSDNS(model),
		"watchdog":          renderRouterOSWatchdog(model),
		"address-lists":     renderRouterOSAddressLists(model),
		"protocol-policy":   strings.Split(strings.TrimSpace(routerosassets.ProtocolPolicySource(model.ContainerIP, model.AllowWANPing)), "\n"),
		"outage-exceptions": renderRouterOSOutage(outage),
		"public-ingress":    renderRouterOSPublicIngress(model),
		"wireguard-egress":  renderRouterOSWireGuard(model),
		"ipv6":              renderRouterOSIPv6(model),
		"finalize":          renderRouterOSFinalize(),
	}
	lines := []string{routerOSCandidateHeader}
	for _, name := range routerOSSectionOrder {
		lines = append(lines, routerOSSectionPrefix+name)
		lines = append(lines, sections[name]...)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func renderRouterOSPublicPortCheck(model RouterOSRenderModel) []string {
	lines := []string{
		`:local publicIngressRangesOverlap do={ :local spec [:tostr $1]; :local candidateLow $2; :local candidateHigh $3; :if ([:len $spec] = 0) do={ :return true }; :foreach item in=[:toarray $spec] do={ :local dash [:find $item "-"]; :local low 0; :local high 0; :if ([:typeof $dash] = "nil") do={ :set low [:tonum $item]; :set high $low } else={ :set low [:tonum [:pick $item 0 $dash]]; :set high [:tonum [:pick $item ($dash + 1) [:len $item]]] }; :if (($candidateLow <= $high) && ($candidateHigh >= $low)) do={ :return true } }; :return false }`,
	}
	guarded := 0
	for _, ingress := range model.PublicIngress {
		if !ingress.GuardForeignUDP {
			continue
		}
		guarded++
		lines = append(lines,
			fmt.Sprintf(managedidentity.Literal(`:foreach wireguard in=[/interface/wireguard/find] do={ :local wireguardPort [/interface/wireguard/get $wireguard listen-port]; :if (($wireguardPort >= %d) && ($wireguardPort <= %d)) do={ :error ("XROUTE UDP hopping range %d-%d conflicts with WireGuard " . [/interface/wireguard/get $wireguard name] . " on port " . $wireguardPort . "; exclude the port") } }; :foreach rule in=[/ip/firewall/nat/find where disabled=no] do={ :local chain [/ip/firewall/nat/get $rule chain]; :local comment [/ip/firewall/nat/get $rule comment]; :if (($chain = "dstnat") && ([:pick $comment 0 7] != "XROUTE ")) do={ :local protocol [:tostr [/ip/firewall/nat/get $rule protocol]]; :if (($protocol = "udp") || ($protocol = "17") || ([:len $protocol] = 0)) do={ :local ports [/ip/firewall/nat/get $rule dst-port]; :if ([$publicIngressRangesOverlap $ports %d %d]) do={ :error ("XROUTE UDP hopping range %d-%d overlaps NAT " . $rule . " " . $comment . " on " . $ports . "; exclude the occupied port") } } } }`), ingress.PublicPort, ingress.PublicPortEnd, ingress.PublicPort, ingress.PublicPortEnd, ingress.PublicPort, ingress.PublicPortEnd, ingress.PublicPort, ingress.PublicPortEnd),
		)
	}
	if guarded == 0 {
		return []string{"# UDP hopping does not require a foreign-port check."}
	}
	return lines
}

func renderRouterOSPublicIngress(model RouterOSRenderModel) []string {
	natComments, allowComments := make([]string, 0, len(model.PublicIngress)), make([]string, 0, len(model.PublicIngress))
	guardMatches := make(map[string]RouterOSPublicIngress)
	for _, ingress := range model.PublicIngress {
		natComments = append(natComments, managedidentity.Literal("XROUTE public dstnat ")+ingress.Key)
		allowComments = append(allowComments, managedidentity.Literal("XROUTE public allow ")+ingress.Key)
		key := ingress.Protocol + ":" + fmt.Sprint(ingress.TargetPort)
		guardMatches[key] = ingress
	}
	guardKeys := make([]string, 0, len(guardMatches))
	for key := range guardMatches {
		guardKeys = append(guardKeys, key)
	}
	sort.Strings(guardKeys)
	guardComments := make([]string, 0, len(guardKeys))
	for _, key := range guardKeys {
		guardComments = append(guardComments, managedidentity.Literal("XROUTE public guard jump ")+strings.ReplaceAll(key, ":", "-"))
	}
	sourceComments := make([]string, 0, len(model.PublicSourceLists))
	for _, list := range model.PublicSourceLists {
		sourceComments = append(sourceComments, list.Comment)
	}

	lines := []string{
		managedidentity.Literal(`:foreach obsoleteComment in={"XROUTE Cloudflare origin dstnat";"XROUTE Reality dstnat";"XROUTE gRPC Reality dstnat";"XROUTE Hysteria2 dstnat"} do={ :local obsoleteRules [/ip/firewall/nat/find where comment=$obsoleteComment]; :if ([:len $obsoleteRules] > 1) do={ :error ("XROUTE obsolete public NAT rule is ambiguous: " . $obsoleteComment) }; :if ([:len $obsoleteRules] = 1) do={ /ip/firewall/nat/remove $obsoleteRules } }`),
		managedidentity.Literal(`:foreach obsoleteComment in={"XROUTE Cloudflare to origin";"XROUTE Reality to container";"XROUTE gRPC Reality to container";"XROUTE Hysteria2 to container"} do={ :local obsoleteRules [/ip/firewall/filter/find where comment=$obsoleteComment]; :if ([:len $obsoleteRules] > 1) do={ :error ("XROUTE obsolete public filter rule is ambiguous: " . $obsoleteComment) }; :if ([:len $obsoleteRules] = 1) do={ /ip/firewall/filter/remove $obsoleteRules } }`),
	}
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/firewall/nat", managedidentity.Literal("XROUTE public dstnat "), "wantedPublicDstnatComments", natComments)...)
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/firewall/filter", managedidentity.Literal("XROUTE public allow "), "wantedPublicAllowComments", allowComments)...)
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/firewall/filter", managedidentity.Literal("XROUTE public guard jump "), "wantedPublicGuardJumpComments", guardComments)...)
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/firewall/address-list", managedidentity.Literal("XROUTE public source "), "wantedPublicSourceComments", sourceComments)...)
	lines = append(lines, renderRouterOSAddressList("PublicTrusted", "SB_PUBLIC_TRUSTED_V4", managedidentity.Literal("XROUTE public trusted source"), model.PublicTrusted4)...)
	for _, list := range model.PublicSourceLists {
		variable := "PublicSource" + strings.ToUpper(strings.TrimPrefix(list.Name, "SB_PUBLIC_"))
		lines = append(lines, renderRouterOSAddressList(variable, list.Name, list.Comment, list.CIDRs)...)
	}

	for _, ingress := range model.PublicIngress {
		natComment, allowComment := managedidentity.Literal("XROUTE public dstnat ")+ingress.Key, managedidentity.Literal("XROUTE public allow ")+ingress.Key
		publicPort := fmt.Sprint(ingress.PublicPort)
		if ingress.PublicPortEnd > ingress.PublicPort {
			publicPort = fmt.Sprintf("%d-%d", ingress.PublicPort, ingress.PublicPortEnd)
		}
		lines = append(lines,
			fmt.Sprintf(`:if ([:len [/ip/firewall/nat/find where comment="%s"]] = 0) do={ /ip/firewall/nat/add chain=dstnat action=dst-nat in-interface-list=WAN protocol=%s dst-port=%s to-addresses="%s" to-ports=%d disabled=yes comment="%s" }`, natComment, ingress.Protocol, publicPort, model.ContainerIP, ingress.TargetPort, natComment),
			fmt.Sprintf(`:local publicDstnat [/ip/firewall/nat/find where comment="%s"]`, natComment),
			fmt.Sprintf(managedidentity.Literal(`:if ([:len $publicDstnat] != 1) do={ :error "XROUTE public NAT rule is missing or ambiguous: %s" }`), ingress.Key),
			`/ip/firewall/nat/unset $publicDstnat dst-address`,
			`/ip/firewall/nat/unset $publicDstnat dst-address-type`,
			`/ip/firewall/nat/unset $publicDstnat src-address-list`,
		)
		destinationMatcher := `dst-address-type=local`
		if ingress.DestinationAddress != "" {
			destinationMatcher = fmt.Sprintf(`dst-address="%s"`, ingress.DestinationAddress)
		}
		sourceMatcher := ""
		if ingress.SourceAddressList != "" {
			sourceMatcher = fmt.Sprintf(` src-address-list="%s"`, ingress.SourceAddressList)
		}
		lines = append(lines,
			fmt.Sprintf(`/ip/firewall/nat/set $publicDstnat chain=dstnat action=dst-nat in-interface-list=WAN protocol=%s dst-port=%s %s%s to-addresses="%s" to-ports=%d disabled=no`, ingress.Protocol, publicPort, destinationMatcher, sourceMatcher, model.ContainerIP, ingress.TargetPort),
			fmt.Sprintf(managedidentity.Literal(`:if ([:len [/ip/firewall/filter/find where comment="%s"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface-list=WAN dst-address="%s" protocol=%s dst-port=%d connection-state=new,established,related connection-nat-state=dstnat disabled=yes comment="%s" }`), allowComment, model.ContainerIP, ingress.Protocol, ingress.TargetPort, allowComment),
			fmt.Sprintf(`:local publicAllow [/ip/firewall/filter/find where comment="%s"]`, allowComment),
			fmt.Sprintf(managedidentity.Literal(`:if ([:len $publicAllow] != 1) do={ :error "XROUTE public allow rule is missing or ambiguous: %s" }`), ingress.Key),
			`/ip/firewall/filter/unset $publicAllow src-address-list`,
			fmt.Sprintf(managedidentity.Literal(`/ip/firewall/filter/set $publicAllow chain="xroute-forward" action=accept in-interface-list=WAN dst-address="%s" protocol=%s dst-port=%d connection-state=new,established,related connection-nat-state=dstnat%s disabled=no`), model.ContainerIP, ingress.Protocol, ingress.TargetPort, sourceMatcher),
		)
	}

	guardDisabled := "yes"
	if model.ConnectionGuard.Enabled && len(model.PublicIngress) != 0 {
		guardDisabled = "no"
	}
	for _, key := range guardKeys {
		ingress := guardMatches[key]
		comment := managedidentity.Literal("XROUTE public guard jump ") + strings.ReplaceAll(key, ":", "-")
		lines = append(lines,
			fmt.Sprintf(managedidentity.Literal(`:if ([:len [/ip/firewall/filter/find where comment="%s"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=jump jump-target="xroute-public-guard" in-interface-list=WAN dst-address="%s" protocol=%s dst-port=%d connection-state=new connection-nat-state=dstnat disabled=yes comment="%s" }`), comment, model.ContainerIP, ingress.Protocol, ingress.TargetPort, comment),
			fmt.Sprintf(`:local publicGuardJump [/ip/firewall/filter/find where comment="%s"]`, comment),
			fmt.Sprintf(managedidentity.Literal(`/ip/firewall/filter/set $publicGuardJump chain="xroute-forward" action=jump jump-target="xroute-public-guard" in-interface-list=WAN dst-address="%s" protocol=%s dst-port=%d connection-state=new connection-nat-state=dstnat disabled=%s`), model.ContainerIP, ingress.Protocol, ingress.TargetPort, guardDisabled),
		)
	}

	guardRules := []struct {
		name, add, set string
	}{
		{"trusted-cloudflare", `action=return src-address-list="SB_CLOUDFLARE_V4"`, `action=return src-address-list="SB_CLOUDFLARE_V4"`},
		{"trusted-manual", `action=return src-address-list="SB_PUBLIC_TRUSTED_V4"`, `action=return src-address-list="SB_PUBLIC_TRUSTED_V4"`},
		{"blocked", `action=drop src-address-list="SB_PUBLIC_ABUSE"`, `action=drop src-address-list="SB_PUBLIC_ABUSE"`},
		{"rate", fmt.Sprintf(`action=return dst-limit=%d/1s,%d,src-address/10s`, model.ConnectionGuard.NewConnectionRate, model.ConnectionGuard.Burst), fmt.Sprintf(`action=return dst-limit=%d/1s,%d,src-address/10s`, model.ConnectionGuard.NewConnectionRate, model.ConnectionGuard.Burst)},
		{"quarantine", fmt.Sprintf(`action=add-src-to-address-list address-list="SB_PUBLIC_ABUSE" address-list-timeout=%ds`, model.ConnectionGuard.QuarantineSeconds), fmt.Sprintf(`action=add-src-to-address-list address-list="SB_PUBLIC_ABUSE" address-list-timeout=%ds`, model.ConnectionGuard.QuarantineSeconds)},
		{"drop-trigger", `action=drop`, `action=drop`},
	}
	// CDN egress addresses aggregate many clients; applying the per-client
	// scanner quota to them would quarantine legitimate CDN traffic.
	autoLists := map[string]bool{}
	for _, ingress := range model.PublicIngress {
		if strings.HasPrefix(ingress.SourceAddressList, "SB_CDN_") {
			autoLists[ingress.SourceAddressList] = true
		}
	}
	wantedAutoComments := []string{}
	for name := range autoLists {
		wantedAutoComments = append(wantedAutoComments, managedidentity.Literal("XROUTE public guard trusted-cdn-")+name)
	}
	sort.Strings(wantedAutoComments)
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/firewall/filter", managedidentity.Literal("XROUTE public guard trusted-cdn-"), "wantedCDNGuardComments", wantedAutoComments)...)
	for _, comment := range wantedAutoComments {
		name := strings.TrimPrefix(comment, managedidentity.Literal("XROUTE public guard trusted-cdn-"))
		action := fmt.Sprintf(`action=return src-address-list="%s"`, name)
		guardRules = append(guardRules, struct{ name, add, set string }{"trusted-cdn-" + name, action, action})
	}
	for _, rule := range guardRules {
		comment := managedidentity.Literal("XROUTE public guard ") + rule.name
		lines = append(lines,
			fmt.Sprintf(managedidentity.Literal(`:if ([:len [/ip/firewall/filter/find where comment="%s"]] = 0) do={ /ip/firewall/filter/add chain="xroute-public-guard" %s disabled=yes comment="%s" }`), comment, rule.add, comment),
			fmt.Sprintf(`:local publicGuardRule [/ip/firewall/filter/find where comment="%s"]`, comment),
			fmt.Sprintf(managedidentity.Literal(`:if ([:len $publicGuardRule] != 1) do={ :error "XROUTE public guard rule is missing or ambiguous: %s" }`), rule.name),
			`/ip/firewall/filter/unset $publicGuardRule src-address-list`,
			`/ip/firewall/filter/unset $publicGuardRule dst-limit`,
			fmt.Sprintf(managedidentity.Literal(`/ip/firewall/filter/set $publicGuardRule chain="xroute-public-guard" %s disabled=%s`), rule.set, guardDisabled),
		)
	}
	lines = append(lines,
		managedidentity.Literal(`:local publicBlocked [/ip/firewall/filter/find where comment="XROUTE public guard blocked"]; :foreach rule in=[/ip/firewall/filter/find where comment~"^XROUTE public guard trusted-cdn-"] do={ /ip/firewall/filter/move $rule destination=$publicBlocked }`),
		managedidentity.Literal(`:local allPublicGuardRules [/ip/firewall/filter/find where chain="xroute-public-guard"]`),
		managedidentity.Literal(`:local ownedPublicGuardRules [/ip/firewall/filter/find where chain="xroute-public-guard" and comment~"^XROUTE public guard "]`),
		managedidentity.Literal(`:if ([:len $allPublicGuardRules] != [:len $ownedPublicGuardRules]) do={ :error "XROUTE public guard chain contains an unowned rule" }`),
		`:local firstNatRule [/ip/firewall/nat/find where dynamic=no]`,
		managedidentity.Literal(`:foreach rule in=[/ip/firewall/nat/find where comment~"^XROUTE public dstnat "] do={ :if (([:len $firstNatRule] > 0) && ([:pick $firstNatRule 0 1] != $rule)) do={ /ip/firewall/nat/move $rule destination=[:pick $firstNatRule 0 1] } }`),
		managedidentity.Literal(`:local forwardReturn [/ip/firewall/filter/find where comment="XROUTE forward policy return"]`),
		managedidentity.Literal(`:if ([:len $forwardReturn] != 1) do={ :error "XROUTE forward return rule missing or ambiguous" }`),
		managedidentity.Literal(`:foreach rule in=[/ip/firewall/filter/find where comment~"^XROUTE public allow "] do={ /ip/firewall/filter/move $rule destination=$forwardReturn }`),
		managedidentity.Literal(`:local firstPublicAllow [/ip/firewall/filter/find where comment~"^XROUTE public allow "]`),
		managedidentity.Literal(`:if ([:len $firstPublicAllow] > 0) do={ :foreach rule in=[/ip/firewall/filter/find where comment~"^XROUTE public guard jump "] do={ /ip/firewall/filter/move $rule destination=[:pick $firstPublicAllow 0 1] } }`),
		fmt.Sprintf(`:if ("%s" = "yes") do={ /ip/firewall/address-list/remove [/ip/firewall/address-list/find where list="SB_PUBLIC_ABUSE"] }`, guardDisabled),
	)
	return lines
}

func renderRouterOSCore(model RouterOSRenderModel) []string {
	lines := []string{
		fmt.Sprintf(`:local wantedIngress %s`, routerOSArray(model.ManagementIngress)),
		managedidentity.Literal(`:foreach interfaceName in=$wantedIngress do={ :if ([:len [/interface/find where name=$interfaceName]] != 1) do={ :error ("XROUTE trusted ingress interface missing: " . $interfaceName) }; :if ([:len [/interface/list/member/find where list="WAN" and interface=$interfaceName]] > 0) do={ :error ("XROUTE trusted ingress interface belongs to WAN: " . $interfaceName) } }`),
		managedidentity.Literal(`:foreach member in=[/interface/list/member/find where list="SB_MANAGEMENT_INGRESS"] do={ :if ([/interface/list/member/get $member comment] != "XROUTE management ingress") do={ :error "XROUTE management ingress member is unowned" } }`),
		managedidentity.Literal(`:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]`),
		managedidentity.Literal(`:if ([:len $gate] != 1) do={ :error "XROUTE diversion gate missing or ambiguous" }`),
		`/ip/firewall/mangle/disable $gate`,
		managedidentity.Literal(`:foreach obsoleteComment in={"XROUTE RouterOS www-ssl management allow";"XROUTE RouterOS www-ssl deny"} do={ :local obsoleteRules [/ip/firewall/filter/find where comment=$obsoleteComment]; :if ([:len $obsoleteRules] > 1) do={ :error ("XROUTE obsolete management rule is ambiguous: " . $obsoleteComment) }; :if ([:len $obsoleteRules] = 1) do={ /ip/firewall/filter/remove $obsoleteRules } }`),
		fmt.Sprintf(`:local managedTable [/routing/table/find where name="%s"]`, model.RoutingTable),
		fmt.Sprintf(managedidentity.Literal(`:if ([:len $managedTable] = 0) do={ /routing/table/add name="%s" fib comment="XROUTE routing table"; :set managedTable [/routing/table/find where name="%s"] }`), model.RoutingTable, model.RoutingTable),
		managedidentity.Literal(`:if (([:len $managedTable] != 1) || ([/routing/table/get $managedTable comment] != "XROUTE routing table")) do={ :error "XROUTE routing table is missing, ambiguous, or unowned" }`),
		`/routing/table/set $managedTable fib`,
		managedidentity.Literal(`:foreach interfaceName in=$wantedIngress do={ :if ([:len [/interface/list/member/find where list="SB_MANAGEMENT_INGRESS" and interface=$interfaceName]] = 0) do={ /interface/list/member/add list="SB_MANAGEMENT_INGRESS" interface=$interfaceName comment="XROUTE management ingress" } }`),
		managedidentity.Literal(`:foreach member in=[/interface/list/member/find where list="SB_MANAGEMENT_INGRESS" and comment="XROUTE management ingress"] do={ :local entryName [/interface/list/member/get $member interface]; :local keep false; :foreach expected in=$wantedIngress do={ :if ($expected = $entryName) do={ :set keep true } }; :if ($keep = false) do={ /interface/list/member/remove $member } }`),
		fmt.Sprintf(managedidentity.Literal(`:local sbBridge [/interface/bridge/find where name="%s" and comment="XROUTE container bridge"]`), model.BridgeName),
		managedidentity.Literal(`:if ([:len $sbBridge] != 1) do={ :error "XROUTE container bridge missing or ambiguous" }`),
		`:local sbBridgeName [/interface/bridge/get $sbBridge name]`,
		fmt.Sprintf(managedidentity.Literal(`:if ([:len [/interface/veth/find where name="%s" and comment="XROUTE container veth"]] != 1) do={ :error "XROUTE container veth missing or ambiguous" }`), model.VethName),
		managedidentity.Literal(`:local containerDefault [/ip/route/find where comment="XROUTE container default"]`),
		fmt.Sprintf(managedidentity.Literal(`:if ([:len $containerDefault] = 0) do={ /ip/route/add dst-address="0.0.0.0/0" gateway="%s@main" routing-table="%s" distance=1 check-gateway=ping comment="XROUTE container default"; :set containerDefault [/ip/route/find where comment="XROUTE container default"] }`), model.ContainerIP, model.RoutingTable),
		managedidentity.Literal(`:if ([:len $containerDefault] != 1) do={ :error "XROUTE container default route missing or ambiguous" }`),
		fmt.Sprintf(`/ip/route/set $containerDefault dst-address="0.0.0.0/0" gateway="%s@main" routing-table="%s" distance=1 check-gateway=ping disabled=no`, model.ContainerIP, model.RoutingTable),
		managedidentity.Literal(`:if ([:len [/ip/firewall/nat/find where comment="XROUTE container LAN masquerade"]] = 0) do={ /ip/firewall/nat/add chain=srcnat action=masquerade src-address="`) + model.ContainerNetwork + managedidentity.Literal(`" dst-address-list="SB_CONTAINER_ALLOWED_INTERNAL" comment="XROUTE container LAN masquerade" }`),
		managedidentity.Literal(`:local containerLanMasquerade [/ip/firewall/nat/find where comment="XROUTE container LAN masquerade"]`),
		managedidentity.Literal(`:if ([:len $containerLanMasquerade] != 1) do={ :error "XROUTE container LAN masquerade is missing or ambiguous" }`),
		`/ip/firewall/nat/set $containerLanMasquerade chain=srcnat action=masquerade src-address="` + model.ContainerNetwork + `" dst-address-list="SB_CONTAINER_ALLOWED_INTERNAL" disabled=no`,
		managedidentity.Literal(`:local endpointBypass [/ip/firewall/mangle/find where comment="XROUTE endpoint bypass"]`),
		managedidentity.Literal(`:local markConnection [/ip/firewall/mangle/find where comment="XROUTE mark connection"]`),
		managedidentity.Literal(`:local markRouting [/ip/firewall/mangle/find where comment="XROUTE mark routing"]`),
		managedidentity.Literal(`:if (([:len $endpointBypass] != 1) || ([:len $markConnection] != 1) || ([:len $markRouting] != 1)) do={ :error "XROUTE diversion rules missing or ambiguous" }`),
		`/ip/firewall/mangle/unset $gate in-interface-list`,
		`/ip/firewall/mangle/unset $gate dst-address-list`,
		`/ip/firewall/mangle/unset $gate dst-address-type`,
		fmt.Sprintf(managedidentity.Literal(`/ip/firewall/mangle/set $gate chain=prerouting action=jump jump-target="xroute-divert" src-address-list="SB_MANAGED_CLIENTS" src-address="!%s" disabled=yes`), model.ContainerIP),
		managedidentity.Literal(`/ip/firewall/mangle/set $endpointBypass chain="xroute-divert" action=return dst-address-list="SB_BYPASS_ENDPOINTS" disabled=no`),
		managedidentity.Literal(`/ip/firewall/mangle/set $markConnection chain="xroute-divert" action=mark-connection connection-state=new connection-mark=no-mark new-connection-mark="xroute-managed" passthrough=yes disabled=no`),
		fmt.Sprintf(managedidentity.Literal(`/ip/firewall/mangle/set $markRouting chain="xroute-divert" action=mark-routing connection-mark="xroute-managed" new-routing-mark="%s" passthrough=no disabled=no`), model.RoutingTable),
		`/ip/firewall/mangle/move $markConnection destination=$markRouting`,
		`/ip/firewall/mangle/move $endpointBypass destination=$markConnection`,
	}
	return append(lines, renderRouterOSManagedDNS(model)...)
}

func renderRouterOSFailClosedDrop() []string {
	return []string{
		managedidentity.Literal(`:local forwardReturn [/ip/firewall/filter/find where comment="XROUTE forward policy return"]`),
		managedidentity.Literal(`:if ([:len $forwardReturn] != 1) do={ :error "XROUTE forward return rule missing or ambiguous" }`),
		managedidentity.Literal(`:local failClosedDrop [/ip/firewall/filter/find where comment="XROUTE fail-closed public drop"]`),
		managedidentity.Literal(`:if ([:len $failClosedDrop] > 1) do={ :error "XROUTE fail-closed rule ambiguous" }`),
		managedidentity.Literal(`:if ([:len $failClosedDrop] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=drop src-address-list="SB_FAIL_CLOSED_CLIENTS" dst-address-list="!SB_INTERNAL_NETWORKS" place-before=$forwardReturn comment="XROUTE fail-closed public drop"; :set failClosedDrop [/ip/firewall/filter/find where comment="XROUTE fail-closed public drop"] }`),
		managedidentity.Literal(`/ip/firewall/filter/set $failClosedDrop chain="xroute-forward" action=drop src-address-list="SB_FAIL_CLOSED_CLIENTS" dst-address-list="!SB_INTERNAL_NETWORKS" disabled=no`),
		`/ip/firewall/filter/move $failClosedDrop destination=$forwardReturn`,
	}
}

func renderRouterOSDNS(model RouterOSRenderModel) []string {
	return []string{
		fmt.Sprintf(`:if (([:tostr [/ip/dns/get servers]] != [:tostr [:toarray "%s"]]) || ([/ip/dns/get use-doh-server] != "%s") || ([/ip/dns/get verify-doh-cert] != true) || ([/ip/dns/get allow-remote-requests] != true)) do={ /ip/dns/set servers="%s" use-doh-server="%s" verify-doh-cert=yes allow-remote-requests=yes; /ip/dns/cache/flush }`, model.DNSServers, model.DNSDoHURL, model.DNSServers, model.DNSDoHURL),
	}
}

func renderRouterOSWatchdog(model RouterOSRenderModel) []string {
	source := fmt.Sprintf(`:global \"SB_HEALTH_URL\" \"http://%s:9080/traffic-ready\";\r\n:global \"SB_HEALTH_FAILURE_THRESHOLD\" %d;\r\n:global \"SB_HEALTH_RECOVERY_THRESHOLD\" %d;\r\n:global \"SB_HEALTH_COOLDOWN_TICKS\" %d;\r\n`, model.ContainerIP, model.WatchdogFailure, model.WatchdogRecovery, model.WatchdogCooldownTicks)
	return []string{
		managedidentity.Literal(`:local watchdogConfig [/system/script/find where name="XROUTE-watchdog-config"]`),
		managedidentity.Literal(`:if ([:len $watchdogConfig] != 1) do={ :error "XROUTE watchdog config script missing or ambiguous" }`),
		fmt.Sprintf(`:local watchdogConfigSource "%s"`, source),
		`/system/script/set $watchdogConfig source=$watchdogConfigSource`,
		managedidentity.Literal(`:local watchdogScript [/system/script/find where name="XROUTE-health-watchdog"]`),
		managedidentity.Literal(`:if ([:len $watchdogScript] != 1) do={ :error "XROUTE health watchdog missing or ambiguous" }`),
		`/system/script/set $watchdogScript source={` + routerosassets.HealthWatchdogSource() + `}`,
		managedidentity.Literal(`:local watchdogScheduler [/system/scheduler/find where name="XROUTE-health-watchdog"]`),
		managedidentity.Literal(`:if ([:len $watchdogScheduler] != 1) do={ :error "XROUTE watchdog scheduler missing or ambiguous" }`),
		fmt.Sprintf(`/system/scheduler/set $watchdogScheduler interval=%ds`, model.WatchdogInterval),
		managedidentity.Literal(`/system/script/run XROUTE-watchdog-config`),
	}
}

func renderRouterOSAddressLists(model RouterOSRenderModel) []string {
	wgSources := make([]string, 0, len(model.WireGuardExits))
	for _, item := range model.WireGuardExits {
		wgSources = append(wgSources, item.SourceAddress+"/32")
	}
	lists := []struct {
		variable, name, comment string
		values                  []string
	}{
		{"Managed", "SB_MANAGED_CLIENTS", managedidentity.Literal("XROUTE managed client"), model.Managed4},
		{"FailClosed", "SB_FAIL_CLOSED_CLIENTS", managedidentity.Literal("XROUTE fail-closed client"), model.FailClosed4},
		{"Internal", "SB_INTERNAL_NETWORKS", managedidentity.Literal("XROUTE internal network"), append(append([]string{}, model.Internal4...), model.ContainerNetwork)},
		{"ContainerAllowed", "SB_CONTAINER_ALLOWED_INTERNAL", managedidentity.Literal("XROUTE explicit container LAN access"), model.ContainerAllowed4},
		{"Management", "SB_MANAGEMENT_SOURCES", managedidentity.Literal("XROUTE management source"), model.ManagementSources4},
		{"Bypass", "SB_BYPASS_ENDPOINTS", managedidentity.Literal("XROUTE loop bypass"), model.BypassEndpoints4},
		{"WireGuardEgress", "SB_WG_EGRESS_SOURCES", managedidentity.Literal("XROUTE WireGuard egress source"), wgSources},
	}
	lines := make([]string, 0, len(lists)*10)
	for _, list := range lists {
		lines = append(lines, renderRouterOSAddressList(list.variable, list.name, list.comment, list.values)...)
	}
	return lines
}

func renderRouterOSAddressList(variable, name, comment string, values []string) []string {
	wanted, commentVariable := "wanted"+variable, "comment"+variable
	return []string{
		fmt.Sprintf(`:local %s "%s"`, commentVariable, comment),
		fmt.Sprintf(`:local %s %s`, wanted, routerOSArray(values)),
		fmt.Sprintf(`:foreach entry in=[/ip/firewall/address-list/find where list="%s" and comment=$%s] do={`, name, commentVariable),
		`  :local entryAddress [/ip/firewall/address-list/get $entry address]`,
		`  :local keep false`,
		fmt.Sprintf(`  :foreach expected in=$%s do={ :if ($expected = $entryAddress) do={ :set keep true } }`, wanted),
		`  :if ($keep = false) do={ /ip/firewall/address-list/remove $entry }`,
		`}`,
		fmt.Sprintf(`:foreach candidateAddress in=$%s do={`, wanted),
		fmt.Sprintf(`  :if ([:len [/ip/firewall/address-list/find where list="%s" and address=$candidateAddress]] = 0) do={ /ip/firewall/address-list/add list="%s" address=$candidateAddress comment=$%s }`, name, name, commentVariable),
		`}`,
	}
}

func renderRouterOSWireGuard(model RouterOSRenderModel) []string {
	interfaces, ruleComments, routeComments, returnComments, tableComments := []string{}, []string{}, []string{}, []string{}, []string{}
	for _, item := range model.WireGuardExits {
		interfaces = append(interfaces, item.Interface)
		ruleComments = append(ruleComments, managedidentity.Literal("XROUTE WG egress rule ")+item.ID)
		routeComments = append(routeComments, managedidentity.Literal("XROUTE WG egress route ")+item.ID)
		returnComments = append(returnComments, managedidentity.Literal("XROUTE WG egress return ")+item.ID)
		tableComments = append(tableComments, managedidentity.Literal("XROUTE WG egress table ")+item.ID)
	}
	lines := []string{
		managedidentity.Literal(`:local sbBridge [/interface/bridge/find where comment="XROUTE container bridge"]`),
		managedidentity.Literal(`:if ([:len $sbBridge] != 1) do={ :error "XROUTE container bridge missing or ambiguous" }`),
		`:local sbBridgeName [/interface/bridge/get $sbBridge name]`,
		managedidentity.Literal(`:if ([:len [/interface/list/find where name="SB_WG_EGRESS"]] = 0) do={ /interface/list/add name="SB_WG_EGRESS" comment="XROUTE WireGuard egress interfaces" }`),
		`:local wgEgressList [/interface/list/find where name="SB_WG_EGRESS"]`,
		managedidentity.Literal(`:if (([:len $wgEgressList] != 1) || ([/interface/list/get $wgEgressList comment] != "XROUTE WireGuard egress interfaces")) do={ :error "XROUTE WireGuard egress interface-list is missing, ambiguous, or unowned" }`),
		fmt.Sprintf(`:local wantedWgInterfaces %s`, routerOSArray(interfaces)),
		managedidentity.Literal(`:foreach member in=[/interface/list/member/find where list="SB_WG_EGRESS" and comment="XROUTE WireGuard egress interface"] do={ :local interfaceName [/interface/list/member/get $member interface]; :local keep false; :foreach expected in=$wantedWgInterfaces do={ :if ($expected = $interfaceName) do={ :set keep true } }; :if ($keep = false) do={ /interface/list/member/remove $member } }`),
		managedidentity.Literal(`:foreach interfaceName in=$wantedWgInterfaces do={ :local interfaceId [/interface/wireguard/find where name=$interfaceName and disabled=no]; :if ([:len $interfaceId] != 1) do={ :error ("XROUTE selected WireGuard exit is missing, disabled, or ambiguous: " . $interfaceName) }; :if ([:len [/interface/list/member/find where list="SB_WG_EGRESS" and interface=$interfaceName and comment="XROUTE WireGuard egress interface"]] = 0) do={ /interface/list/member/add list="SB_WG_EGRESS" interface=$interfaceName comment="XROUTE WireGuard egress interface" } }`),
	}
	lines = append(lines, renderRouterOSOwnedCleanup("/routing/rule", managedidentity.Literal("XROUTE WG egress rule "), "wantedWgRuleComments", ruleComments)...)
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/route", managedidentity.Literal("XROUTE WG egress route "), "wantedWgRouteComments", routeComments)...)
	lines = append(lines, renderRouterOSOwnedCleanup("/ip/route", managedidentity.Literal("XROUTE WG egress return "), "wantedWgReturnComments", returnComments)...)
	lines = append(lines, renderRouterOSOwnedCleanup("/routing/table", managedidentity.Literal("XROUTE WG egress table "), "wantedWgTableComments", tableComments)...)
	for _, item := range model.WireGuardExits {
		tableComment := managedidentity.Literal("XROUTE WG egress table ") + item.ID
		routeComment := managedidentity.Literal("XROUTE WG egress route ") + item.ID
		returnComment := managedidentity.Literal("XROUTE WG egress return ") + item.ID
		ruleComment := managedidentity.Literal("XROUTE WG egress rule ") + item.ID
		lines = append(lines,
			fmt.Sprintf(`:local fullTunnelPeers [/interface/wireguard/peers/find where interface="%s" and disabled=no and allowed-address~"0.0.0.0/0"]`, item.Interface),
			fmt.Sprintf(managedidentity.Literal(`:if ([:len $fullTunnelPeers] != 1) do={ :error "XROUTE WireGuard exit %s requires exactly one enabled IPv4 full-tunnel peer" }`), item.ID),
			fmt.Sprintf(`:if ([:len [/routing/table/find where name="%s"]] = 0) do={ /routing/table/add name="%s" fib comment="%s" }`, item.RoutingTable, item.RoutingTable, tableComment),
			fmt.Sprintf(`:local wgTable [/routing/table/find where name="%s"]`, item.RoutingTable),
			fmt.Sprintf(managedidentity.Literal(`:if (([:len $wgTable] != 1) || ([/routing/table/get $wgTable comment] != "%s")) do={ :error "XROUTE WireGuard egress table is ambiguous or unowned: %s" }`), tableComment, item.ID),
			fmt.Sprintf(`:if ([:len [/ip/route/find where comment="%s"]] = 0) do={ /ip/route/add dst-address="0.0.0.0/0" gateway="%s" routing-table="%s" distance=1 comment="%s" }`, routeComment, item.Interface, item.RoutingTable, routeComment),
			fmt.Sprintf(`:local wgDefault [/ip/route/find where comment="%s"]`, routeComment),
			fmt.Sprintf(`/ip/route/set $wgDefault dst-address="0.0.0.0/0" gateway="%s" routing-table="%s" distance=1 disabled=no`, item.Interface, item.RoutingTable),
			fmt.Sprintf(`:if ([:len [/ip/route/find where comment="%s"]] = 0) do={ /ip/route/add dst-address="%s/32" gateway="%s@main" routing-table=main distance=1 comment="%s" }`, returnComment, item.SourceAddress, model.ContainerIP, returnComment),
			fmt.Sprintf(`:local wgReturn [/ip/route/find where comment="%s"]`, returnComment),
			fmt.Sprintf(`/ip/route/set $wgReturn dst-address="%s/32" gateway="%s@main" routing-table=main distance=1 disabled=no`, item.SourceAddress, model.ContainerIP),
			fmt.Sprintf(`:if ([:len [/routing/rule/find where comment="%s"]] = 0) do={ /routing/rule/add src-address="%s/32" action=lookup-only-in-table table="%s" comment="%s" }`, ruleComment, item.SourceAddress, item.RoutingTable, ruleComment),
			fmt.Sprintf(`:local wgRule [/routing/rule/find where comment="%s"]`, ruleComment),
			fmt.Sprintf(`/routing/rule/set $wgRule src-address="%s/32" action=lookup-only-in-table table="%s" disabled=no`, item.SourceAddress, item.RoutingTable),
		)
	}
	disabled := "yes"
	if len(model.WireGuardExits) > 0 {
		disabled = "no"
	}
	return append(lines,
		managedidentity.Literal(`:if ([:len [/ip/firewall/nat/find where comment="XROUTE WireGuard egress masquerade"]] = 0) do={ /ip/firewall/nat/add chain=srcnat action=masquerade src-address-list="SB_WG_EGRESS_SOURCES" out-interface-list="SB_WG_EGRESS" disabled=yes comment="XROUTE WireGuard egress masquerade" }`),
		managedidentity.Literal(`:local wgMasquerade [/ip/firewall/nat/find where comment="XROUTE WireGuard egress masquerade"]`),
		fmt.Sprintf(`/ip/firewall/nat/set $wgMasquerade chain=srcnat action=masquerade src-address-list="SB_WG_EGRESS_SOURCES" out-interface-list="SB_WG_EGRESS" disabled=%s`, disabled),
		managedidentity.Literal(`:if ([:len [/ip/firewall/filter/find where comment="XROUTE WireGuard egress allow"]] = 0) do={ /ip/firewall/filter/add chain="xroute-forward" action=accept in-interface=$sbBridgeName src-address-list="SB_WG_EGRESS_SOURCES" out-interface-list="SB_WG_EGRESS" disabled=yes comment="XROUTE WireGuard egress allow" }`),
		managedidentity.Literal(`:local wgAllow [/ip/firewall/filter/find where comment="XROUTE WireGuard egress allow"]`),
		fmt.Sprintf(managedidentity.Literal(`/ip/firewall/filter/set $wgAllow chain="xroute-forward" action=accept in-interface=$sbBridgeName src-address-list="SB_WG_EGRESS_SOURCES" out-interface-list="SB_WG_EGRESS" disabled=%s`), disabled),
	)
}

func renderRouterOSOwnedCleanup(menu, prefix, variable string, comments []string) []string {
	return []string{
		fmt.Sprintf(`:local %s %s`, variable, routerOSArray(comments)),
		fmt.Sprintf(`:foreach entry in=[%s/find where comment~"^%s"] do={`, menu, prefix),
		fmt.Sprintf(`  :local entryComment [%s/get $entry comment]`, menu),
		`  :local keep false`,
		fmt.Sprintf(`  :foreach expected in=$%s do={ :if ($expected = $entryComment) do={ :set keep true } }`, variable),
		fmt.Sprintf(`  :if ($keep = false) do={ %s/remove $entry }`, menu),
		`}`,
	}
}

func renderRouterOSIPv6(model RouterOSRenderModel) []string {
	lists := []struct {
		variable, name, comment string
		values                  []string
	}{
		{"ManagedV6", "SB_MANAGED_CLIENTS_V6", managedidentity.Literal("XROUTE managed IPv6 client"), model.Managed6},
		{"FailClosedV6", "SB_FAIL_CLOSED_CLIENTS_V6", managedidentity.Literal("XROUTE fail-closed IPv6 client"), model.FailClosed6},
		{"InternalV6", "SB_INTERNAL_NETWORKS_V6", managedidentity.Literal("XROUTE internal IPv6 network"), model.Internal6},
	}
	lines := []string{}
	for _, list := range lists {
		wanted, commentVariable := "wanted"+list.variable, "comment"+list.variable
		lines = append(lines,
			fmt.Sprintf(`:local %s "%s"`, commentVariable, list.comment),
			fmt.Sprintf(`:local %s %s`, wanted, routerOSArray(list.values)),
			fmt.Sprintf(`:foreach entry in=[/ipv6/firewall/address-list/find where list="%s" and comment=$%s] do={ :local address [/ipv6/firewall/address-list/get $entry address]; :local keep false; :foreach expected in=$%s do={ :if ($expected = $address) do={ :set keep true } }; :if ($keep = false) do={ /ipv6/firewall/address-list/remove $entry } }`, list.name, commentVariable, wanted),
			fmt.Sprintf(`:foreach address in=$%s do={ :if ([:len [/ipv6/firewall/address-list/find where list="%s" and address=$address]] = 0) do={ /ipv6/firewall/address-list/add list="%s" address=$address comment=$%s } }`, wanted, list.name, list.name, commentVariable),
		)
	}
	disabled := "yes"
	if model.IPv6Mode == "block_managed" {
		disabled = "no"
	}
	return append(lines,
		managedidentity.Literal(`:if ([:len [/ipv6/firewall/filter/find where comment="XROUTE managed IPv6 WAN block"]] = 0) do={ /ipv6/firewall/filter/add chain=forward action=drop src-address-list="SB_MANAGED_CLIENTS_V6" dst-address-list="!SB_INTERNAL_NETWORKS_V6" out-interface-list=WAN disabled=yes comment="XROUTE managed IPv6 WAN block" }`),
		managedidentity.Literal(`:local managedIPv6Block [/ipv6/firewall/filter/find where comment="XROUTE managed IPv6 WAN block"]`),
		fmt.Sprintf(`/ipv6/firewall/filter/set $managedIPv6Block chain=forward action=drop src-address-list="SB_MANAGED_CLIENTS_V6" dst-address-list="!SB_INTERNAL_NETWORKS_V6" out-interface-list=WAN disabled=%s`, disabled),
		managedidentity.Literal(`:if ([:len [/ipv6/firewall/filter/find where comment="XROUTE fail-closed IPv6 public drop"]] = 0) do={ /ipv6/firewall/filter/add chain=forward action=drop src-address-list="SB_FAIL_CLOSED_CLIENTS_V6" dst-address-list="!SB_INTERNAL_NETWORKS_V6" comment="XROUTE fail-closed IPv6 public drop" }`),
	)
}

func renderRouterOSFinalize() []string {
	return []string{
		routerosassets.ManagedConnectionCleanupSource(),
		managedidentity.Literal(`:do { /system/script/run XROUTE-startup-fail-open } on-error={ :error "XROUTE startup fail-open script unavailable" }`),
		managedidentity.Literal(`:log info "XROUTE candidate reconciled; watchdog controls diversion gate"`),
	}
}

func routerOSArray(values []string) string {
	if len(values) == 0 {
		return `[:toarray ""]`
	}
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = `"` + value + `"`
	}
	return "{" + strings.Join(quoted, ";") + "}"
}
