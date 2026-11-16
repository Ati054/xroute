package releasecontract

import "testing"

func TestNamespaceCompatibilityPreservesLegacyButGuardsNewInstallations(t *testing.T) {
	for _, capability := range []string{"", "1", "2"} {
		labels := map[string]string{RouterOSNamespaceLabel: capability}
		if !namespaceCompatible(labels, "SB-GATEWAY") {
			t.Fatal("legacy update contract changed")
		}
		if namespaceCompatible(labels, "XROUTE") != (capability == RouterOSNamespaceVersion) {
			t.Fatalf("new installation compatibility for %q is unsafe", capability)
		}
		if namespaceCompatible(labels, "other") {
			t.Fatal("unknown ownership accepted")
		}
	}
}

func TestCompatibleAllowsSkippedReleaseSchemas(t *testing.T) {
	labels := map[string]string{
		"org.opencontainers.image.title": ImageTitle,
		LifecycleVersionLabel:            LifecycleVersion,
		ConfigSchemaLabel:                "4",
		MinimumConfigSchemaLabel:         "1",
	}
	if schema, minimum, ok := Compatible(labels, 1); !ok || schema != 4 || minimum != 1 {
		t.Fatalf("compatible skipped release rejected: schema=%d minimum=%d ok=%v", schema, minimum, ok)
	}
}

func TestCompatibleRejectsContractAndMigrationGaps(t *testing.T) {
	tests := []map[string]string{
		{
			"org.opencontainers.image.title": ImageTitle,
			LifecycleVersionLabel:            "2",
			ConfigSchemaLabel:                "1",
			MinimumConfigSchemaLabel:         "1",
		},
		{
			"org.opencontainers.image.title": ImageTitle,
			LifecycleVersionLabel:            LifecycleVersion,
			ConfigSchemaLabel:                "4",
			MinimumConfigSchemaLabel:         "2",
		},
	}
	for _, labels := range tests {
		if _, _, ok := Compatible(labels, 1); ok {
			t.Fatalf("incompatible release contract accepted: %#v", labels)
		}
	}
}
