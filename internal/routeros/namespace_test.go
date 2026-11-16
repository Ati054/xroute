package routeros

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Ati054/xroute/internal/managedidentity"
	routerosassets "github.com/Ati054/xroute/routeros"
)

// Each namespace is fixed for the lifetime of an appliance process. Separate
// processes exercise the same production initializers used by real upgrades.
func TestNamespaceLifecycleFixtures(t *testing.T) {
	const root = "disk1/XROUTE-customer-xroute-api"
	const marker = "XROUTE_NAMESPACE_FIXTURE="
	if os.Getenv("XROUTE_NAMESPACE_FIXTURE") == "1" {
		fixtures := map[string]string{
			"watchdog":  routerosassets.HealthWatchdogSource(),
			"startup":   routerosassets.ContainerStartupInstallSource(),
			"protocol":  routerosassets.ProtocolPolicySource("192.0.2.2", true),
			"uninstall": renderFullUninstallWorker(root),
			"memory":    renderContainerMemoryWorker(root+"/root-1.7.0", 224<<20, 256<<20, 288<<20, 320<<20),
			"update": renderImageUpdateWorker(ImageUpdateSpec{
				Version: "1.7.1", StorageRoot: root, CandidateRoot: root + "/root-1.7.1",
				CandidateSource: "local-file", CandidateReference: root + "/data/xroute-1.7.1-linux-arm64.tar",
				ContainerAddress: "192.0.2.2",
			}),
		}
		body, err := json.Marshal(fixtures)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(marker + string(body))
		return
	}
	fixtures := map[string]map[string]string{}
	for _, namespace := range []string{"", managedidentity.CurrentPrefix} {
		command := exec.Command(os.Args[0], "-test.run=^TestNamespaceLifecycleFixtures$")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, managedidentity.Environment+"=") && !strings.HasPrefix(value, "XROUTE_NAMESPACE_FIXTURE=") {
				command.Env = append(command.Env, value)
			}
		}
		command.Env = append(command.Env, managedidentity.Environment+"="+namespace, "XROUTE_NAMESPACE_FIXTURE=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("namespace %q: %v: %s", namespace, err, output)
		}
		_, raw, ok := strings.Cut(string(output), marker)
		if !ok {
			t.Fatal("child fixture missing")
		}
		line, _, _ := strings.Cut(raw, "\n")
		var decoded map[string]string
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatal(err)
		}
		fixtures[namespace] = decoded
	}
	legacy, _ := managedidentity.Parse(managedidentity.LegacyPrefix)
	for name, current := range fixtures[managedidentity.CurrentPrefix] {
		previous := fixtures[""][name]
		if (name == "uninstall" || name == "memory" || name == "update") && (!strings.Contains(previous, root) || !strings.Contains(current, root)) {
			t.Fatalf("%s changed a user path", name)
		}
		// Remove only this known user value before comparing maintained text.
		if legacy.Source(strings.ReplaceAll(current, root, "{{root}}")) != strings.ReplaceAll(previous, root, "{{root}}") {
			t.Fatalf("%s namespaces do not implement the same lifecycle contract", name)
		}
	}
	for namespace, outputs := range fixtures {
		prefix := namespace
		if prefix == "" {
			prefix = managedidentity.LegacyPrefix
		}
		if !strings.Contains(outputs["update"], `envlist=$envlist`) || !strings.Contains(outputs["update"], `comment="`+prefix+` container candidate"`) {
			t.Fatalf("%q update loses its namespace or environment", namespace)
		}
	}
}
