package controlplane

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Ati054/xroute/internal/managedidentity"
)

func TestRouterOSExportContinuationPreservesTokensAndQuotedSpaces(t *testing.T) {
	commands := routerOSExportCommands("/interface list member\r\n" +
		"add interface=ether2 list=\\\r\n    SB_MANAGEMENT_INGRESS comment=\"two  spaces\"\r\n" +
		"/container add root-dir=usb1/xroute/root-1.7.2 comment=\"XROUTE con\\\n    tainer\"\n" +
		"/ip firewall address-list\nadd address=192.0.2.0/24 \\\n    list=SB_INTERNAL_NETWORKS\n")
	if len(commands) != 3 || commands[0].properties["list"] != "SB_MANAGEMENT_INGRESS" ||
		commands[0].properties["comment"] != "two  spaces" || commands[1].properties["comment"] != "XROUTE container" ||
		commands[2].properties["address"] != "192.0.2.0/24" || commands[2].properties["list"] != "SB_INTERNAL_NETWORKS" {
		t.Fatalf("export continuations changed values: %#v", commands)
	}
}

func TestRouterOSSetupConnectionUsesManagedExport(t *testing.T) {
	namespace := managedidentity.Prefix()
	export := "/ip address\nadd address=172.31.255.1/30 interface=bridge-test comment=\"" + namespace + " container gateway\"\n" +
		"/ip firewall filter\nadd action=accept protocol=tcp dst-address=172.31.255.1 dst-port=59443 comment=\"" + namespace + " RouterOS REST allow\"\n" +
		"/user\nadd name=test-rest group=xroute-api comment=\"" + namespace + " control-plane REST user\"\n" +
		"/ip service\nset ssh port=2223\n"
	facts, err := parseRouterOSExport(export)
	if err != nil {
		t.Fatal(err)
	}
	conn := facts.setupConnection
	if conn["base_url"] != "https://172.31.255.1:59443" || conn["username"] != "test-rest" || conn["ssh_port"] != 2223 {
		t.Fatalf("setup connection = %#v", conn)
	}
	ambiguous, err := parseRouterOSExport(export + "/ip service set ssh port=2224\n")
	if err != nil || ambiguous.setupConnection["base_url"] != conn["base_url"] || ambiguous.setupConnection["ssh_port"] != nil {
		t.Fatalf("ambiguous SSH settings must leave the SSH port blank: %+v, %v", ambiguous, err)
	}
	guidedExport := strings.ReplaceAll(export, "/user\nadd name=test-rest group=xroute-api comment=\""+namespace+" control-plane REST user\"\n",
		"/system script\nadd name="+namespace+"-guided-management-state comment=\""+namespace+" guided management restore state\"\n")
	guided, err := parseRouterOSExport(guidedExport)
	if err != nil || guided.setupConnection["username"] != managedidentity.Literal("xroute-api") {
		t.Fatalf("guided setup connection: %+v, %v", guided, err)
	}
	manualExport := strings.ReplaceAll(export, "/user\nadd name=test-rest group=xroute-api comment=\""+namespace+" control-plane REST user\"\n",
		"/user group\nadd name=xroute-api comment=\""+namespace+" REST group\"\n")
	manualExport = strings.Replace(manualExport, "dst-port=59443", "dst-port=58443", 1)
	manual, err := parseRouterOSExport(manualExport)
	if err != nil || manual.setupConnection["base_url"] != "https://172.31.255.1:58443" ||
		manual.setupConnection["ssh_port"] != 2223 || manual.setupConnection["username"] != nil {
		t.Fatalf("manual setup connection: %+v, %v", manual, err)
	}
	wrongDestination, err := parseRouterOSExport(strings.Replace(manualExport, "dst-address=172.31.255.1", "dst-address=172.31.255.2", 1))
	if err != nil || wrongDestination.setupConnection != nil {
		t.Fatalf("REST rule at another address produced setup parameters: %+v, %v", wrongDestination, err)
	}
}

func TestRouterOSExportContinuationKeepsQuotedLeadingSpace(t *testing.T) {
	commands := routerOSExportCommands("# wrapped comment \\\n/interface bridge\nadd name=home comment=\"first\\\n      second\"\n")
	if len(commands) != 1 || commands[0].properties["comment"] != "first  second" {
		t.Fatalf("quoted space changed: %#v", commands)
	}
}

func TestRouterOSImportRestoresInstallerDirectoryAndInternalReview(t *testing.T) {
	server := newTestServer(t)
	cookie, csrf := bootstrapSession(t, server)
	config, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	objectAt(config, "storage")["root"] = ""
	config["networks"] = []any{}
	if _, err := server.repository.saveDraft(config); err != nil {
		t.Fatal(err)
	}
	export := "/container\nadd root-dir=/usb1/xroute/root-1.7.2 comment=\"" + managedidentity.Literal("XROUTE container") + "\"\n" +
		"/ip firewall address-list\nadd address=192.0.2.0/24 list=SB_INTERNAL_NETWORKS comment=\"" + managedidentity.Literal("XROUTE internal network") + "\"\n" +
		"add address=198.51.100.0/24 list=SB_INTERNAL_NETWORKS comment=unowned\n"
	response := performRequest(t, server, http.MethodPost, apiPrefix+"/routeros/import", map[string]any{"export_text": export}, map[string]string{csrfHeader: csrf}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("import: %d %s", response.Code, response.Body.String())
	}
	stored, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	if objectAt(stored, "storage")["root"] != "usb1/xroute" {
		t.Fatal("installer directory not restored")
	}
	statuses := reviewedNetworkStatuses(objectAt(stored, "routeros"))
	if statuses["192.0.2.0/24"] != "accepted" || statuses["198.51.100.0/24"] == "accepted" {
		t.Fatalf("review trust broadened: %#v", statuses)
	}
}

func TestRouterOSImportUsesInstalledManagementSelectionAsHint(t *testing.T) {
	for _, namespace := range []string{managedidentity.Prefix()} {
		t.Run(namespace, func(t *testing.T) {
			value := "/interface bridge\nadd name=home\n" +
				"/interface list member\nadd interface=home list=SB_MANAGEMENT_INGRESS comment=\"" + namespace + " management ingress\"\n" +
				"add interface=ether1 list=WAN\n" +
				"add interface=ether1 list=LAN\n" +
				"add interface=vpn-in list=SB_MANAGEMENT_INGRESS comment=\"" + namespace + " management ingress\"\n" +
				"add interface=disabled-lan list=SB_MANAGEMENT_INGRESS disabled=yes comment=\"" + namespace + " management ingress\"\n" +
				"/ip address\nadd address=192.168.77.1/24 interface=home\nadd address=192.168.78.1/24 interface=home\nadd address=192.168.99.1/24 interface=ether1\n" +
				"/ip firewall address-list\nadd address=192.168.78.0/24 list=SB_MANAGEMENT_SOURCES comment=\"" + namespace + " management source\"\n" +
				"add address=10.24.4.0/24 list=SB_MANAGEMENT_SOURCES comment=\"" + namespace + " management source\"\n" +
				"add address=192.168.77.0/24 list=SB_MANAGEMENT_SOURCES disabled=yes comment=\"" + namespace + " management source\"\n"
			facts, err := parseRouterOSExport(value)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts.managementIngress) != 2 || !facts.managementIngress["home"] || !facts.managementIngress["vpn-in"] {
				t.Fatalf("management hint includes WAN or disabled interfaces: %#v", facts.managementIngress)
			}
			if len(facts.managementNetworks) != 1 || !facts.managementNetworks["192.168.78.0/24"] {
				t.Fatalf("installer-selected source was lost or broadened: %#v", facts.managementNetworks)
			}
			access := facts.managedPanelAccess
			if got := fmt.Sprint(access["allowed_ingress_interfaces"]); got != "[home vpn-in]" {
				t.Fatalf("installed interface list = %s", got)
			}
			if got := fmt.Sprint(access["allowed_source_cidrs"]); got != "[10.24.4.0/24 192.168.78.0/24]" {
				t.Fatalf("installed CIDR list = %s", got)
			}
		})
	}
}

func TestRouterOSImportCurrentNamespaceManagementHint(t *testing.T) {
	// Ownership is immutable after process initialization, just as on a router.
	command := exec.Command(os.Args[0], "-test.run=^TestRouterOSImportUsesInstalledManagementSelectionAsHint$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, managedidentity.Environment+"=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, managedidentity.Environment+"=XROUTE")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("new namespace management hint: %v: %s", err, output)
	}
}

func TestRouterOSImportBuildsReviewWithoutTrustingDiscoveredNetworks(t *testing.T) {
	server := newTestServer(t)
	cookie, csrf := bootstrapSession(t, server)
	config, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	config["networks"] = []any{map[string]any{"id": "home", "name": "Home", "cidrs": []any{"192.168.77.0/24"}}}
	objectAt(config, "system")["deployment_ready"] = true
	if _, err := server.repository.saveDraft(config); err != nil {
		t.Fatal(err)
	}

	export := "# 2026-09-03 by RouterOS 7.21.5\n" +
		"/interface bridge\nadd name=bridge\n" +
		"/interface list member\nadd interface=bridge list=LAN\n" +
		"/ip address\nadd address=192.168.77.1/24 interface=bridge\n" +
		"/ip route\nadd dst-address=192.168.55.0/24 gateway=wireguard1\n" +
		"/ip socks\nset enabled=yes port=3128\n" +
		"/system package update\nset channel=long-term\n"
	response := performRequest(t, server, http.MethodPost, apiPrefix+"/routeros/import", map[string]any{
		"export_text": export, "source_name": "audit.rsc",
	}, map[string]string{csrfHeader: csrf}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("import failed: %d %s", response.Code, response.Body.String())
	}
	body := decodeResponse(t, response)
	inventory := body["network_inventory"].([]any)
	statuses := map[string]string{}
	for _, raw := range inventory {
		item := raw.(map[string]any)
		statuses[item["cidr"].(string)] = item["status"].(string)
	}
	if statuses["192.168.77.0/24"] != "accepted" || statuses["192.168.55.0/24"] != "pending" {
		t.Fatalf("unexpected review statuses: %#v", statuses)
	}
	topology := body["topology_suggestions"].(map[string]any)
	if topology["source"] != "offline_nonoverlap_candidate" || topology["requires_live_confirmation"] != true {
		t.Fatalf("unsafe topology result: %#v", topology)
	}
	management := body["management_ingress_suggestions"].([]any)
	if len(management) != 1 || management[0] != "bridge" {
		t.Fatalf("LAN management suggestion missing: %#v", management)
	}
	warnings := body["warnings"].([]any)
	if len(warnings) != 2 || warnings[1] != "socks_configured" {
		t.Fatalf("posture warning missing: %#v", warnings)
	}
	stored, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	if objectAt(stored, "system")["deployment_ready"] != false || objectAt(stored, "routeros")["channel"] != "long-term" {
		t.Fatalf("import review did not invalidate readiness: %#v", stored)
	}
}

func TestRouterOSImportRejectsSecretsBeforeChangingDraft(t *testing.T) {
	server := newTestServer(t)
	cookie, csrf := bootstrapSession(t, server)
	before, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision, err := revisionFor(before)
	if err != nil {
		t.Fatal(err)
	}
	for _, export := range []string{
		"/interface wireguard\nadd name=wg-private private-key=not-safe\n",
		"# show-sensitive=yes\n/ip service\nset api password=not-safe\n",
	} {
		response := performRequest(t, server, http.MethodPost, apiPrefix+"/routeros/import", map[string]any{"export_text": export}, map[string]string{csrfHeader: csrf}, cookie)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("secret-bearing export accepted: %d %s", response.Code, response.Body.String())
		}
	}
	after, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	afterRevision, err := revisionFor(after)
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision != beforeRevision {
		t.Fatalf("rejected import changed draft: %s -> %s", beforeRevision, afterRevision)
	}
}

func TestRouterOSImportPreservesReviewedStatusAndExistingManagedTopology(t *testing.T) {
	server := newTestServer(t)
	cookie, csrf := bootstrapSession(t, server)
	config, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	router := objectAt(config, "routeros")
	router["import_review"] = map[string]any{"networks": []any{map[string]any{"cidr": "10.9.0.0/24", "status": "ignored"}}}
	if _, err := server.repository.saveDraft(config); err != nil {
		t.Fatal(err)
	}
	export := "/interface bridge\nadd name=bridge-sb comment=SB-GATEWAY-core\n" +
		"/interface veth\nadd name=veth-sb address=172.31.255.2/30 gateway=172.31.255.1 comment=SB-GATEWAY-core\n" +
		"/ip address\nadd address=172.31.255.1/30 interface=bridge-sb comment=SB-GATEWAY-core\n" +
		"/container envs\nadd key=SB_TUN_ADDRESS value=172.31.254.1/30 comment=SB-GATEWAY-core\n" +
		"/ip route\nadd dst-address=10.9.0.0/24 gateway=ether2\n"
	response := performRequest(t, server, http.MethodPost, apiPrefix+"/routeros/import", map[string]any{"export_text": export}, map[string]string{csrfHeader: csrf}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("import failed: %d %s", response.Code, response.Body.String())
	}
	body := decodeResponse(t, response)
	item := body["network_inventory"].([]any)[0].(map[string]any)
	if item["status"] != "ignored" {
		t.Fatalf("review decision was lost: %#v", item)
	}
	topology := body["topology_suggestions"].(map[string]any)
	if topology["source"] != "existing_project_export" || topology["container_dns"] != "172.31.255.1" {
		t.Fatalf("managed topology was not reused: %#v", topology)
	}
}
