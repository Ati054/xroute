package controlplane

import "testing"

func TestNewAMD64InstallationUsesNativeDefaultsWithoutChangingSavedConfiguration(t *testing.T) {
	server := newTestServer(t)
	server.testImageArchitecture = "amd64"
	config, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	if objectAt(config, "routeros")["architecture"] != "x86_64" {
		t.Fatal("fresh AMD64 install inherited ARM64 defaults")
	}
	objectAt(config, "routeros")["architecture"] = "saved-platform"
	if _, err := server.repository.saveDraft(config); err != nil {
		t.Fatal(err)
	}
	saved, err := server.getDraft()
	if err != nil || objectAt(saved, "routeros")["architecture"] != "saved-platform" {
		t.Fatal("saved architecture was overwritten")
	}
}

func TestStorageValidationAllowsCHRSystemDiskWithoutAllowingARMFlash(t *testing.T) {
	for _, entry := range []struct {
		architecture, root string
		valid              bool
	}{
		{"x86", "xroute", true}, {"x86_64", "xroute", true}, {"amd64", "xroute", true},
		{"arm64", "xroute", false}, {"arm64", "flash/xroute", false},
		{"x86", "disk1/xroute", true}, {"x86", "slot1/xroute", true}, {"x86", "disk1", false},
	} {
		config := routerOSReadyConfig(t)
		config["routeros"].(map[string]any)["architecture"] = entry.architecture
		config["storage"] = map[string]any{"root": entry.root}
		check := validateCurrentConfig(config)
		if check.Valid != entry.valid {
			t.Errorf("%s %q: valid=%t errors=%+v", entry.architecture, entry.root, check.Valid, check.Errors)
		}
	}
}
