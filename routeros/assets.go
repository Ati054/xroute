// Package routerosassets shares the installer watchdog with native Apply.
package routerosassets

import (
	"embed"
	"strings"
	"sync"

	"github.com/Ati054/xroute/internal/managedidentity"
)

//go:embed watchdog.rsc cloudflare-update.rsc container-startup.rsc uninstall.rsc
var scripts embed.FS

var resolvedScripts = sync.OnceValue(func() map[string]string {
	result := make(map[string]string, 3)
	for _, name := range []string{"watchdog.rsc", "cloudflare-update.rsc", "container-startup.rsc", "uninstall.rsc"} {
		source, err := scripts.ReadFile(name)
		if err != nil {
			panic(err)
		}
		result[name] = managedidentity.Script(strings.ReplaceAll(string(source), "\r\n", "\n"))
	}
	return result
})

// ManagedConnectionCleanupSource tolerates conntrack records that expire after
// find returns, and never invokes remove with an empty item list.
func ManagedConnectionCleanupSource() string {
	return managedidentity.Literal(`:foreach sbFlow in=[/ip/firewall/connection/find where connection-mark="xroute-managed"] do={ :do { /ip/firewall/connection/remove $sbFlow } on-error={} }`)
}

// GuidedManagementCleanupSource is shared by manual and scheduled removal.
// The caller supplies the already validated storageRoot local variable.
func GuidedManagementCleanupSource() string {
	return scriptBody("uninstall.rsc", "# Guided management cleanup: begin\n", "# Guided management cleanup: end")
}

// ManagedBackupCleanupSource shares ownership checks between manual and
// scheduled removal, including installer files and retained Apply backups.
func ManagedBackupCleanupSource() string {
	return scriptBody("uninstall.rsc", "# Managed backup cleanup: begin\n", "# Managed backup cleanup: end")
}

// ContainerStartupInstallSource installs the same storage gate as fresh installs.
func ContainerStartupInstallSource() string {
	return strings.TrimSpace(resolvedScripts()["container-startup.rsc"])
}

// ContainerStorageReadySource preserves whitespace for RouterOS source readback.
func ContainerStorageReadySource() string {
	return rawScriptBody("container-startup.rsc", `/system/script/set $readyId source={`, "}\n:if ([:len $bootId]")
}

// ContainerBootSource preserves whitespace for RouterOS source readback.
func ContainerBootSource() string {
	return rawScriptBody("container-startup.rsc", `/system/script/set $bootId source={`, "}\n:if ([:len $scheduleId]")
}

// HealthWatchdogSource returns the same script body used by fresh installs.
func HealthWatchdogSource() string {
	return scriptBody("watchdog.rsc", managedidentity.Literal(`/system/script/add name="XROUTE-health-watchdog" policy=ftp,read,write,policy,test source={`),
		managedidentity.Literal(`} comment="XROUTE health watchdog"`))
}

// StartupFailOpenSource returns the boot safety script without re-importing it.
func StartupFailOpenSource() string {
	return scriptBody("watchdog.rsc", managedidentity.Literal(`/system/script/add name="XROUTE-startup-fail-open" policy=read,write,test source={`),
		managedidentity.Literal(`} comment="XROUTE startup fail-open"`))
}

// CloudflareUpdateSource returns the optional RouterOS updater body.
func CloudflareUpdateSource() string {
	return scriptBody("cloudflare-update.rsc", managedidentity.Literal(`/system/script/add name="XROUTE-cloudflare-update" policy=read,write,test,ftp source={`),
		managedidentity.Literal(`} comment="XROUTE Cloudflare updater"`))
}

func scriptBody(filename, start, end string) string {
	return strings.TrimSpace(rawScriptBody(filename, start, end))
}

func rawScriptBody(filename, start, end string) string {
	text, exists := resolvedScripts()[filename]
	if !exists {
		panic("unknown maintained RouterOS script")
	}
	_, body, ok := strings.Cut(text, start)
	if !ok {
		panic(filename + " script source marker missing")
	}
	body, _, ok = strings.Cut(body, end)
	if !ok {
		panic(filename + " script end marker missing")
	}
	return body
}
