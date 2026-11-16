package controlplane

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Ati054/xroute/internal/managedidentity"
)

func TestRouterOSImportUsesInstalledManagementSelectionAsHint(t *testing.T) {
	for _, namespace := range []string{managedidentity.Prefix()} {
		t.Run(namespace, func(t *testing.T) {
			value := "/interface bridge\nadd name=home\n" +
				"/interface list member\nadd interface=home list=SB_MANAGEMENT_INGRESS comment=\"" + namespace + " management ingress\"\n" +
				"add interface=ether1 list=WAN\n" +
				"add interface=ether1 list=LAN\n" +
				"add interface=disabled-lan list=SB_MANAGEMENT_INGRESS disabled=yes comment=\"" + namespace + " management ingress\"\n" +
				"/ip address\nadd address=192.168.77.1/24 interface=home\nadd address=192.168.78.1/24 interface=home\nadd address=192.168.99.1/24 interface=ether1\n" +
				"/ip firewall address-list\nadd address=192.168.78.0/24 list=SB_MANAGEMENT_SOURCES comment=\"" + namespace + " management source\"\n" +
				"add address=192.168.77.0/24 list=SB_MANAGEMENT_SOURCES disabled=yes comment=\"" + namespace + " management source\"\n"
			facts, err := parseRouterOSExport(value)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts.managementIngress) != 1 || !facts.managementIngress["home"] {
				t.Fatalf("management hint includes WAN or disabled interfaces: %#v", facts.managementIngress)
			}
			if len(facts.managementNetworks) != 1 || !facts.managementNetworks["192.168.78.0/24"] {
				t.Fatalf("installer-selected source was lost or broadened: %#v", facts.managementNetworks)
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
