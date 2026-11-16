package runtimeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ati054/xroute/internal/rulesets"
)

func TestIranianCardOutageRulesAcceptOnlyReviewedNationalSuffixes(t *testing.T) {
	for _, suffix := range []string{"ir", "xn--mgba3a4f16a"} {
		if got, err := normalizeOutageDomain(suffix); err != nil || got != suffix {
			t.Fatalf("national suffix %s rejected: %q %v", suffix, got, err)
		}
	}
	for _, local := range []string{"lan", "localhost", "internal", "ir;bad"} {
		if got, err := normalizeOutageDomain(local); err == nil || got != "" {
			t.Fatalf("unreviewed local value accepted: %q", local)
		}
	}
}

func TestIranianLocalCardKeepsTrafficAndDNSOnConfiguredPath(t *testing.T) {
	packs, err := rulesets.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := rulesets.EnsureSeeds(root, packs); err != nil {
		t.Fatal(err)
	}
	// A synthetic bank outside the national suffix proves dependency expansion.
	if err := os.WriteFile(filepath.Join(root, "ir-banks.json"), []byte(`{"version":3,"rules":[{"domain_suffix":["bank.example","bankmellat.ir"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"vless_with_wan_exceptions", "wan_with_vless_exceptions"} {
		t.Run(mode, func(t *testing.T) {
			policy := map[string]any{"id": "route", "enabled": true, "mode": "best", "traffic_mode": mode, "torrent_direct": false,
				"selection_order": []any{"group:fixture"}, "node_groups": []any{map[string]any{"id": "fixture", "subscription_ids": []any{"fixture"}}}}
			wantTarget, wantDNS := "direct-wan", "direct-public-dns"
			if mode == "vless_with_wan_exceptions" {
				policy["direct_services"] = []any{"ir-local"}
			} else {
				policy["service_routes"] = map[string]any{"ir-local": "route"}
				wantTarget, wantDNS = "route", "policy-dns-route"
			}
			config := map[string]any{
				"dns":      map[string]any{"internal_server": "192.0.2.1", "direct_resolver": map[string]any{"provider": "yandex", "protocol": "doh"}, "vpn_resolver": map[string]any{"provider": "cloudflare", "protocol": "dot"}},
				"policies": []any{policy}, "local_clients": []any{map[string]any{"id": "device", "enabled": true, "policy_id": "route", "source_cidrs": []any{"192.0.2.10/32"}}},
			}
			selectable := map[string]struct{}{"route": {}}
			source, err := BuildXrayRouteSource(config, []map[string]any{{"id": "node", "subscription_id": "fixture"}}, []map[string]any{{"tag": "node"}, {"tag": "route"}, {"tag": "direct-wan"}, {"tag": "block"}})
			if err != nil {
				t.Fatal(err)
			}
			traffic, err := ConvertXrayRules(source["rules"], selectable, xrayRuleSetDescriptors(source["rule_set"]), root)
			if err != nil {
				t.Fatal(err)
			}
			dns, err := BuildPolicyDNSSource(config, selectable)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := CompilePolicyDNS(dns, PolicyDNSCompileOptions{Selectable: selectable, RuleSetRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			matches := func(host string, domains []string) bool {
				for _, domain := range domains {
					domain = strings.TrimPrefix(domain, "domain:")
					if host == domain || strings.HasSuffix(host, "."+domain) {
						return true
					}
				}
				return false
			}
			for _, host := range []string{"example.ir", "www.example.ir", "example.xn--mgba3a4f16a", "bank.example", "login.bank.example"} {
				foundTraffic, foundDNS := false, false
				for _, rule := range traffic {
					if !containsText(stringSlice(rule["source"]), "192.0.2.10/32") || !matches(host, stringSlice(rule["domain"])) {
						continue
					}
					foundTraffic = true
					if rule["outboundTag"] != wantTarget && rule["balancerTag"] != wantTarget {
						t.Fatalf("%s wrong path: %#v", host, rule)
					}
				}
				for _, lane := range compiled.Runtime.Lanes {
					for _, rule := range lane.Rules {
						if !matches(host, rule.DomainSuffix) {
							continue
						}
						foundDNS = true
						if rule.Server != wantDNS {
							t.Fatalf("%s wrong DNS path: %s", host, rule.Server)
						}
					}
				}
				if !foundTraffic || !foundDNS {
					t.Fatalf("%s missing traffic=%v DNS=%v", host, foundTraffic, foundDNS)
				}
			}
			for _, rule := range traffic {
				if matches("example.ir.evil.example", stringSlice(rule["domain"])) {
					t.Fatal("national suffix matched an unrelated domain")
				}
			}
		})
	}
}
