package controlplane

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestImageUploadRejectsForeignArchitectureBeforePublishingCandidate(t *testing.T) {
	for _, host := range []string{"arm64", "amd64"} {
		t.Run(host, func(t *testing.T) {
			server := newTestServer(t)
			server.testImageArchitecture = host
			cookie, csrf := bootstrapSession(t, server)
			config, err := server.getDraft()
			if err != nil {
				t.Fatal(err)
			}
			config["storage"] = map[string]any{"root": "usb1/sb-gateway"}
			if _, err := server.repository.saveDraft(config); err != nil {
				t.Fatal(err)
			}
			wrong := "amd64"
			if host == "amd64" {
				wrong = "arm64"
			}
			// A misleading filename cannot make foreign executable code valid.
			response := uploadImageRequest(t, server, cookie, csrf, "xroute-9.8.7-linux-"+host+".tar", dockerImageArchive(t, wrong, "9.8.7", "layer"))
			if response.Code != http.StatusUnprocessableEntity || objectAt(decodeResponse(t, response), "error")["code"] != "image_architecture_mismatch" {
				t.Fatalf("wrong architecture response: %d %s", response.Code, response.Body.String())
			}
			files, err := os.ReadDir(filepath.Join(server.opts.DataDir, "lifecycle-uploads"))
			if err != nil || len(files) != 0 {
				t.Fatalf("rejected upload left files: %v %v", files, err)
			}
			response = uploadImageRequest(t, server, cookie, csrf, "candidate.tar", dockerImageArchive(t, host, "9.8.7", "layer"))
			if response.Code != http.StatusOK || decodeResponse(t, response)["architecture"] != host {
				t.Fatalf("matching architecture response: %d %s", response.Code, response.Body.String())
			}
			reference := text(decodeResponse(t, response)["routeros_path"])
			if _, err := server.validateVerifiedLocalImage("usb1/sb-gateway", reference); err != nil {
				t.Fatal(err)
			}
			// Recheck the receipt at scheduling time, even after a successful upload.
			server.testImageArchitecture = wrong
			if _, err := server.validateVerifiedLocalImage("usb1/sb-gateway", reference); err == nil {
				t.Fatal("foreign verification receipt accepted")
			}
		})
	}
}
