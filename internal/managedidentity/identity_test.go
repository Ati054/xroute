package managedidentity

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamespaceBinding(t *testing.T) {
	for _, value := range []string{"", LegacyPrefix, CurrentPrefix} {
		n, err := Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		want := value
		if want == "" {
			want = LegacyPrefix
		}
		if n.prefix != want {
			t.Fatalf("%q resolved to %q", value, n.prefix)
		}
	}
	for _, value := range []string{"xroute", " XROUTE", "XROUTE ", "other"} {
		if _, err := Parse(value); err == nil {
			t.Fatalf("accepted invalid namespace %q", value)
		}
	}
}

func TestMaintainedSourcePreservesGuardAndPrefixLengths(t *testing.T) {
	legacy, _ := Parse(LegacyPrefix)
	current, _ := Parse(CurrentPrefix)
	source := `:local xrouteFileNamespace "XROUTE"
# XRoute namespace compatibility guard: begin
:if ($xrouteFileNamespace = "SB-GATEWAY") do={ :put "XROUTE container" }
# XRoute namespace compatibility guard: end
:if ([:pick $comment 0 7] = "XROUTE ") do={ :put "XROUTE-health-watchdog" }
:if ([:pick $comment 0 13] != "XROUTE panel ") do={ :put "veth-xroute" }
:put "/opt/sb-gateway/scripts/run-xray.sh xroute-1.7.0-linux-arm64.tar SB_ROUTEROS_NAMESPACE"
`
	if current.Source(source) != source {
		t.Fatal("current source changed")
	}
	got := legacy.Source(source)
	for _, want := range []string{
		`:local xrouteFileNamespace "SB-GATEWAY"`,
		`:if ($xrouteFileNamespace = "SB-GATEWAY") do={ :put "XROUTE container" }`,
		`[:pick $comment 0 11] = "SB-GATEWAY "`,
		`[:pick $comment 0 17] != "SB-GATEWAY panel "`,
		`"SB-GATEWAY-health-watchdog"`, `"veth-sb"`,
		`/opt/sb-gateway/scripts/run-xray.sh xroute-1.7.0-linux-arm64.tar SB_ROUTEROS_NAMESPACE`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	user := "XROUTE xroute-api /disk1/xroute-config customer value"
	for _, n := range []Namespace{current, legacy} {
		got := fmt.Sprintf(n.Source(`:put "XROUTE"; :put %q`), user)
		if !strings.HasSuffix(got, fmt.Sprintf("%q", user)) {
			t.Fatal("user input was translated")
		}
	}
}

// Translation is restricted to maintained literals. A rendered document can
// contain credentials, paths or user names that happen to include our prefix.
func TestLiteralResolverNeverReceivesRenderedOrUserText(t *testing.T) {
	for _, dir := range []string{"../routeros", "../runtimeconfig", "../controlplane", "../../routeros"} {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Literal" {
					return true
				}
				id, ok := selector.X.(*ast.Ident)
				if !ok || id.Name != "managedidentity" {
					return true
				}
				literal, valid := call.Args[0].(*ast.BasicLit)
				if !valid || literal.Kind != token.STRING {
					t.Errorf("%s: namespace translation receives nonliteral input", path)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
