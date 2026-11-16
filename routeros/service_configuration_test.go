package routerosassets

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Recent RouterOS versions return active connections alongside listeners.
// A configuration lookup must not become ambiguous when its own SSH/REST
// session appears in /ip/service.
func TestServiceConfigurationSelectorsExcludeConnections(t *testing.T) {
	files, err := filepath.Glob("*.rsc")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "guided-installer.rsc.in")
	selector := regexp.MustCompile(`/ip/service/find[^\]]*`)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range selector.FindAllString(string(body), -1) {
			if !strings.Contains(match, "dynamic=no") {
				t.Errorf("%s: configuration lookup includes live sessions: %s", file, match)
			}
		}
	}
}
