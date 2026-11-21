package runtimeconfig

import (
	"strings"
	"testing"
)

func TestUpgradeNginxStaticCompression(t *testing.T) {
	api := `location /api/ { proxy_pass http://127.0.0.1:8080; }`
	public := `server { listen 443 ssl; location / { proxy_pass http://127.0.0.1:8080; } }`
	for _, newline := range []string{"\n", "\r\n"} {
		original := strings.ReplaceAll("http {\nserver { listen 9443 ssl;\nlocation ^~ /_next/static/ { alias /opt/ui/_next/static/; }\n"+api+"}\n"+public+"\n}", "\n", newline)
		updated, err := UpgradeNginxStaticCompression(original)
		if err != nil || strings.Count(updated, "gzip on;") != 1 || !strings.Contains(updated, api) || !strings.Contains(updated, public) {
			t.Fatalf("static-only upgrade failed: %v", err)
		}
		if again, err := UpgradeNginxStaticCompression(updated); err != nil || again != updated {
			t.Fatalf("upgrade is not idempotent: %v", err)
		}
	}
	for _, body := range []string{
		`location ^~ /_next/static/ { gzip off; }`,
		`location ^~ /_next/static/ { include custom.conf; }`,
		`location ^~ /_next/static/ { proxy_pass http://127.0.0.1:8080; }`,
		`location ^~ /_next/static/ {} location ^~ /_next/static/ {}`,
	} {
		original := "http { server { listen 9443 ssl; " + body + " } }"
		if updated, err := UpgradeNginxStaticCompression(original); err == nil || updated != original {
			t.Fatal("custom/ambiguous configuration changed")
		}
	}
}
