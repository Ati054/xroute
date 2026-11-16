package controlplane

import (
	"bytes"
	"context"
	"testing"
)

func TestRegistryCatalogSelectsMatchingArchitectureAndRejectsForeignConfig(t *testing.T) {
	for _, architecture := range []string{"arm64", "amd64"} {
		for _, indexed := range []bool{false, true} {
			t.Run(architecture+map[bool]string{false: "/manifest", true: "/index"}[indexed], func(t *testing.T) {
				label := "1"
				config := bytes.ReplaceAll(registryCatalogFixtureConfig(t, &label), []byte(`"architecture":"arm64"`), []byte(`"architecture":"`+architecture+`"`))
				fixture := newRegistryCatalogFixture(t, config, indexed, architecture)
				got, err := inspectRegistryImageCatalogSchemaWithClient(context.Background(), fixture.reference, fixture.version, fixture.server.Client(), architecture)
				if err != nil || got != 1 {
					t.Fatalf("matching platform: schema=%d err=%v", got, err)
				}
				other := "amd64"
				if architecture == "amd64" {
					other = "arm64"
				}
				foreign := newRegistryCatalogFixture(t, config, false, architecture)
				if _, err := inspectRegistryImageCatalogSchemaWithClient(context.Background(), foreign.reference, foreign.version, foreign.server.Client(), other); err == nil {
					t.Fatal("foreign platform accepted")
				}
			})
		}
	}
}
