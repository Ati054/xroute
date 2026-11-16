package releasecontract

import "strings"

// ImageArchitecture maps the supported RouterOS and OCI names to an OCI
// architecture. A 32-bit x86 image is not compatible with an amd64 container.
func ImageArchitecture(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "arm64", "aarch64":
		return "arm64"
	case "amd64", "x86_64", "x86-64", "x86":
		return "amd64"
	default:
		return ""
	}
}

func ImagePlatformMatches(os, architecture, expected string) bool {
	// RouterOS host aliases must not turn a noncanonical or 32-bit OCI image
	// into an AMD64 candidate. aarch64 is retained for older ARM64 archives.
	if architecture != "arm64" && architecture != "aarch64" && architecture != "amd64" {
		return false
	}
	arch := ImageArchitecture(architecture)
	return os == "linux" && arch != "" && arch == ImageArchitecture(expected)
}
