package imagebundle

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestValidatePlatformChecksArchiveMetadataRatherThanFilename(t *testing.T) {
	for _, architecture := range []string{"arm64", "amd64"} {
		t.Run(architecture, func(t *testing.T) {
			layer := tarPayload(t, "fixture.txt", []byte("architecture fixture"))
			layerName := hash(layer) + "/layer.tar"
			config, _ := json.Marshal(map[string]any{
				"architecture": architecture, "os": "linux", "rootfs": map[string]any{"diff_ids": []string{"sha256:" + hash(layer)}},
				"config": map[string]any{"Labels": map[string]string{
					"org.opencontainers.image.title": "sb-gateway", "org.opencontainers.image.version": "1.7.0",
					"io.sb-gateway.lifecycle.version": "1", "io.sb-gateway.config.schema": "1", "io.sb-gateway.config.minimum-schema": "1",
				}},
			})
			configName := hash(config) + ".json"
			manifest, _ := json.Marshal([]manifestEntry{{Config: configName, RepoTags: []string{"xroute:test"}, Layers: []string{layerName}}})
			path := filepath.Join(t.TempDir(), "misleading-linux-arm64.tar")
			writeArchive(t, path, map[string][]byte{configName: config, layerName: layer, "manifest.json": manifest})
			got, err := ValidatePlatform(path, "xroute:test", "1.7.0", architecture)
			if err != nil || got.Platform != "linux/"+architecture {
				t.Fatalf("details=%+v err=%v", got, err)
			}
			other := "amd64"
			if architecture == "amd64" {
				other = "arm64"
			}
			if _, err := ValidatePlatform(path, "xroute:test", "1.7.0", other); err == nil {
				t.Fatal("foreign platform accepted")
			}
		})
	}
}
