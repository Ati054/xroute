package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuidedInstallerUsesMatchingOfflineSources(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"arm64", "amd64"} {
		t.Run(arch, func(t *testing.T) {
			out := t.TempDir()
			if err := writeGuidedInstaller(root, out, "1.7.0", arch, 92501504, strings.Repeat("a", 64)); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(out, "xroute-1.7.0-install-"+arch+".rsc"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if strings.Contains(text, "@@") || strings.Contains(text, "check-certificate=no") || strings.Contains(text, "/disk/format") {
				t.Fatal("unsafe or incomplete installer")
			}
			if strings.Contains(text, "api.github.com") || strings.Contains(text, "/releases/download/") {
				t.Fatal("the installer must use the uploaded archive")
			}
			beforeConfirmation, _, found := strings.Cut(text, `:local confirmed [$choose 1]`)
			if !found || !strings.Contains(beforeConfirmation, `:if ([:len $uploaded] != 1) do={ :error`) ||
				!strings.Contains(beforeConfirmation, `:if ([/file/get $uploaded size] != $imageBytes) do={ :error`) {
				t.Fatal("missing or incomplete archive must be rejected before confirmation")
			}
			for _, name := range []string{"variables.example.rsc", "bootstrap.rsc", "preflight.rsc", "webfig-bootstrap.rsc", "fasttrack-patch.rsc", "container-startup.rsc", "install.rsc", "watchdog.rsc"} {
				data, err := os.ReadFile(filepath.Join(root, "routeros", name))
				if err != nil {
					t.Fatal(err)
				}
				want := quoteRSC(strings.ReplaceAll(string(bundleScriptContents(name, data, arch)), "\r\n", "\n"))
				if strings.Count(text, "contents="+want+"\n") != 1 {
					t.Fatalf("%s does not match the offline installer", name)
				}
			}
			// Selecting options cannot mutate the router before confirmation.
			before, _, ok := strings.Cut(text, "# All executable installer content")
			if !ok {
				t.Fatal("missing installation boundary")
			}
			for _, operation := range []string{"/file/add", "/ip/service/set", "/certificate/add", "/container/add", "/ip/address/add"} {
				if strings.Contains(before, operation) {
					t.Fatalf("mutation before consent: %s", operation)
				}
			}
		})
	}
}

func TestGuidedInstallerRejectsUntrustedIdentity(t *testing.T) {
	for _, tc := range []struct {
		version, arch, hash string
		bytes               int64
	}{
		{"1.7.0\"; :error injected", "arm64", strings.Repeat("a", 64), 90000000},
		{"1.7.0", "arm", strings.Repeat("a", 64), 90000000},
		{"1.7.0", "arm64", "bad", 90000000},
		{"1.7.0", "arm64", strings.Repeat("a", 64), 1},
	} {
		if err := writeGuidedInstaller("unused", t.TempDir(), tc.version, tc.arch, tc.bytes, tc.hash); err == nil {
			t.Fatal("accepted invalid release identity")
		}
	}
}

func TestQuoteRSCProtectsEmbeddedSource(t *testing.T) {
	if got, want := quoteRSC("$user\n\"quoted\"\\path"), `"\$user\n\"quoted\"\\path"`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
