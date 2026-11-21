package runtimeconfig

import (
	"errors"
	"strings"
)

// UpgradeNginxStaticCompression compresses only the management UI's immutable
// assets. API responses and public proxy locations keep their saved policies.
func UpgradeNginxStaticCompression(config string) (string, error) {
	blocks, err := nginxRefreshBlocks(config)
	if err != nil {
		return config, err
	}
	target := -1
	for index, block := range blocks {
		if strings.Join(block.words, " ") != "location ^~ /_next/static/" || block.parent < 1 {
			continue
		}
		server := blocks[block.parent]
		if strings.Join(server.words, " ") != "server" || server.parent < 1 || strings.Join(blocks[server.parent].words, " ") != "http" {
			continue
		}
		management := false
		for _, directive := range server.directives {
			if len(directive.words) >= 2 && directive.words[0] == "listen" && (directive.words[1] == "9443" || strings.HasSuffix(directive.words[1], ":9443")) {
				management = true
			}
		}
		if !management {
			continue
		}
		if target >= 0 {
			return config, errors.New("cannot upgrade ambiguous Nginx static locations")
		}
		target = index
	}
	if target < 0 {
		return config, nil
	}
	wanted := []string{"gzip on", "gzip_vary on", "gzip_min_length 1024", "gzip_types text/css application/javascript application/json image/svg+xml"}
	seen := map[string]bool{}
	for _, directive := range blocks[target].directives {
		name := directive.words[0]
		if name == "include" || name == "proxy_pass" || name == "fastcgi_pass" {
			return config, errors.New("cannot upgrade custom Nginx static content")
		}
		if !strings.HasPrefix(name, "gzip") {
			continue
		}
		matched := false
		for _, value := range wanted {
			if strings.Join(directive.words, " ") == value && !seen[name] {
				matched = true
				seen[name] = true
				break
			}
		}
		if !matched {
			return config, errors.New("cannot upgrade custom Nginx static compression")
		}
	}
	var insertion strings.Builder
	for _, value := range wanted {
		if !seen[strings.Fields(value)[0]] {
			insertion.WriteString("\n            " + value + ";")
		}
	}
	at := blocks[target].body
	return config[:at] + insertion.String() + config[at:], nil
}
