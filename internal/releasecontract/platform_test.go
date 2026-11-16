package releasecontract

import "testing"

func TestImageArchitectureAliasesAndCrossPlatformRejection(t *testing.T) {
	for _, entry := range []struct {
		host, image, os string
		want            bool
	}{
		{"arm64", "aarch64", "linux", true},
		{"x86", "amd64", "linux", true},
		{"x86_64", "amd64", "linux", true},
		{"amd64", "arm64", "linux", false},
		{"arm64", "amd64", "linux", false},
		{"amd64", "386", "linux", false},
		{"amd64", "x86", "linux", false},
		{"amd64", "X86", "linux", false},
		{"amd64", "x86_64", "linux", false},
		{"amd64", "amd64", "windows", false},
		{"unknown", "unknown", "linux", false},
	} {
		if got := ImagePlatformMatches(entry.os, entry.image, entry.host); got != entry.want {
			t.Errorf("host=%s image=%s/%s: got %t want %t", entry.host, entry.os, entry.image, got, entry.want)
		}
	}
}
