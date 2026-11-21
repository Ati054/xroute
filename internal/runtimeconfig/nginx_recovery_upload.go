package runtimeconfig

import (
	"errors"
	"fmt"
	"strings"
)

const recoveryUploadPath = "/api/v1/recovery/backups/upload"

// UpgradeNginxRecoveryUpload copies the saved management API policy into the
// exact recovery upload location. Other API and public listeners stay intact.
func UpgradeNginxRecoveryUpload(config string, maxBytes int64) (string, error) {
	blocks, err := nginxRefreshBlocks(config)
	if err != nil {
		return config, err
	}
	management, err := nginxManagementServer(blocks)
	if err != nil || management < 0 {
		return config, err
	}
	api, upload := -1, -1
	for index, block := range blocks {
		if block.parent != management || len(block.words) < 2 || block.words[0] != "location" {
			continue
		}
		words := block.words
		path := words[len(words)-1]
		switch {
		case len(words) == 2 && path == "/api/":
			if api >= 0 {
				return config, errors.New("cannot upgrade ambiguous Nginx API locations")
			}
			api = index
		case len(words) == 3 && words[1] == "=" && path == recoveryUploadPath:
			if upload >= 0 {
				return config, errors.New("cannot upgrade duplicate Nginx recovery upload locations")
			}
			upload = index
		case len(words) == 3 && (words[1] == "~" || words[1] == "~*"):
			if words[1] != "~" || path != nginxSubscriptionRefreshPattern && path != "^/api/v1/drafts/(apply|rollback)$" {
				return config, errors.New("cannot upgrade custom Nginx API regex precedence")
			}
		case (len(words) == 2 && len(path) > len("/api/") || len(words) == 3 && words[1] == "^~") &&
			strings.HasPrefix(recoveryUploadPath, path):
			return config, errors.New("cannot upgrade overlapping Nginx recovery locations")
		}
	}
	if api < 0 || upload >= 0 {
		return config, nil
	}
	block := blocks[api]
	upstream, authorization := 0, 0
	for _, directive := range block.directives {
		words := directive.words
		switch words[0] {
		case "include", "client_max_body_size", "client_body_timeout",
			"proxy_request_buffering", "proxy_read_timeout", "proxy_send_timeout":
			return config, errors.New("cannot upgrade custom Nginx API upload policy")
		case "proxy_pass":
			if len(words) == 2 && words[1] == "http://127.0.0.1:8080" {
				upstream++
			}
		case "proxy_set_header":
			if len(words) == 3 && strings.EqualFold(words[1], "Authorization") {
				if words[2] != "$http_authorization" {
					return config, errors.New("cannot upgrade custom Nginx API authentication")
				}
				authorization++
			}
		}
	}
	if upstream != 1 || authorization != 1 || maxBytes <= 0 {
		return config, errors.New("cannot verify Nginx API upload policy")
	}
	for _, child := range blocks {
		if child.parent == api {
			return config, errors.New("cannot upgrade nested Nginx API routing")
		}
	}
	location := fmt.Sprintf("location = %s {\n            client_max_body_size %d;\n"+
		"            client_body_timeout 1800s;\n            proxy_request_buffering off;\n"+
		"            proxy_read_timeout 1800s;\n            proxy_send_timeout 1800s;", recoveryUploadPath, maxBytes)
	return config[:block.start] + location + config[block.body:block.end] + "\n\n        " + config[block.start:], nil
}
