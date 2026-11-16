package main

import (
	"crypto/sha512"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The console installer carries the same maintained bootstrap scripts as the
// offline bundle. It never downloads executable code from a moving branch.
func writeGuidedInstaller(root, output, version, architecture string, archiveBytes int64, archiveSHA256 string) error {
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`).MatchString(version) ||
		(architecture != "arm64" && architecture != "amd64") || archiveBytes < 1<<20 ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(archiveSHA256) {
		return fmt.Errorf("invalid guided installer release identity")
	}
	body, err := os.ReadFile(filepath.Join(root, "routeros", "guided-installer.rsc.in"))
	if err != nil {
		return err
	}
	var files strings.Builder
	for _, name := range []string{"variables.example.rsc", "bootstrap.rsc", "preflight.rsc", "webfig-bootstrap.rsc", "fasttrack-patch.rsc", "container-startup.rsc", "install.rsc", "watchdog.rsc"} {
		data, err := os.ReadFile(filepath.Join(root, "routeros", name))
		if err != nil {
			return err
		}
		data = bundleScriptContents(name, data, architecture)
		if len(data) > 60000 {
			return fmt.Errorf("installer script %s exceeds RouterOS file-content limit", name)
		}
		normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
		sum := sha512.Sum512([]byte(normalized))
		fmt.Fprintf(&files, ":if ([:len [/file/find where name=($installRoot . %s)]] = 0) do={ /file/add name=($installRoot . %s) type=file contents=%s\n } else={ :if ([:convert [/file/get [/file/find where name=($installRoot . %s)] contents] transform=sha512 to=hex] != %s) do={ :error \"XRoute: staged installer differs from this release; no file was overwritten\" } }\n", quoteRSC("/"+name), quoteRSC("/"+name), quoteRSC(normalized), quoteRSC("/"+name), quoteRSC(fmt.Sprintf("%x", sum)))
	}
	result := strings.NewReplacer("@@VERSION@@", version, "@@ARCH@@", architecture,
		"@@IMAGE_BYTES@@", strconv.FormatInt(archiveBytes, 10), "@@IMAGE_SHA256@@", archiveSHA256,
		"@@FILES@@", files.String()).Replace(strings.ReplaceAll(string(body), "\r\n", "\n"))
	if strings.Contains(result, "@@") {
		return fmt.Errorf("unresolved guided installer placeholder")
	}
	return atomicWrite(filepath.Join(output, "xroute-"+version+"-install-"+architecture+".rsc"), []byte(result))
}

func quoteRSC(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\r", `\r`, "\n", `\n`, "\t", `\t`).Replace(value) + `"`
}
