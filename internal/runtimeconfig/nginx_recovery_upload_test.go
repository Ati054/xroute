package runtimeconfig

import (
	"fmt"
	"strings"
	"testing"
)

func TestNginxRecoveryUploadMigrationKeepsOrdinaryAPILimit(t *testing.T) {
	const maxBytes = 2128 << 20
	template := "http { client_max_body_size 2m; server { listen 443 ssl; " + nginxRefreshTestAPI +
		" } server { listen 9443 ssl; " + nginxRefreshTestAPI + " } }"
	got, err := UpgradeNginxRecoveryUpload(template, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "location = "+recoveryUploadPath) != 1 ||
		!strings.Contains(got, fmt.Sprintf("client_max_body_size %d;", maxBytes)) ||
		!strings.Contains(got, "client_body_timeout 1800s;") ||
		!strings.Contains(got, "proxy_read_timeout 1800s;") ||
		!strings.Contains(got, "proxy_send_timeout 1800s;") ||
		!strings.Contains(got, "proxy_request_buffering off;") ||
		!strings.Contains(got, "X-Synthetic-Guard") {
		t.Fatal("migration omitted upload limits or saved API policy")
	}
	if strings.Count(got, nginxRefreshTestAPI) != 2 || !strings.HasPrefix(got, "http { client_max_body_size 2m;") {
		t.Fatal("migration changed another API location or its size limit")
	}
	if again, err := UpgradeNginxRecoveryUpload(got, maxBytes); err != nil || again != got {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	for _, policy := range []string{
		"location /api/v1/recovery/ { deny all; } ",
		"location ~ ^/api/ { return 403; } ",
	} {
		input := "http { server { listen 9443 ssl; " + policy + nginxRefreshTestAPI + " } }"
		if output, err := UpgradeNginxRecoveryUpload(input, maxBytes); err == nil || output != input {
			t.Fatal("migration overrode an existing location")
		}
	}
}
