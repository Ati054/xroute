package routeros

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	routerosassets "github.com/Ati054/xroute/routeros"

	"github.com/Ati054/xroute/internal/managedidentity"
)

var (
	fullUninstallWorker    = managedidentity.Literal("XROUTE-uninstall-worker")
	fullUninstallScheduler = managedidentity.Literal("XROUTE-uninstall-once")
	maxFixedScriptBytes    = 64 << 10
)

var lifecycleStorageRootPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{2,190}$`)
var reservedStorageRootPattern = regexp.MustCompile(`^(?:flash|disk[0-9]+|usb[0-9]+|slot[0-9]+|sata[0-9]+|nvme[0-9]+|skins|pub|hotspot|web-proxy)$`)

func validFixedManagedScript(name, source string) bool {
	var prefix string
	switch name {
	case recoveryRestartWorker:
		prefix = managedidentity.Literal("# XROUTE one-shot recovery restart\n")
	case fullUninstallWorker:
		prefix = managedidentity.Literal("# XROUTE exact-scope full uninstall\n")
	case imageUpdateWorker:
		prefix = managedidentity.Literal("# XROUTE autonomous one-image switch\n")
	case containerMemoryWorker:
		prefix = managedidentity.Literal("# XROUTE one-shot container memory update\n")
	case containerStartupWorker:
		prefix = managedidentity.Literal("# XROUTE one-shot storage startup migration\n")
	default:
		return false
	}
	return len(source) >= len(prefix) && len(source) <= maxFixedScriptBytes && strings.HasPrefix(source, prefix) && !strings.ContainsRune(source, '\x00')
}

// ScheduleFullUninstall arms a fixed project-owned worker after all user input
// has been reduced to one validated external-storage path. The scheduler is
// created last, so an interrupted request leaves only an inert script.
func (client *Client) ScheduleFullUninstall(ctx context.Context, storageRoot string) (map[string]any, error) {
	root, err := validateLifecycleStorageRoot(storageRoot)
	if err != nil {
		return nil, err
	}
	if _, err := client.RestoreServiceInfoLogs(ctx); err != nil {
		return nil, fmt.Errorf("restore RouterOS account logging before uninstall: %w", err)
	}
	worker := renderFullUninstallWorker(root)
	if _, err := client.installFixedManagedScript(ctx, fullUninstallWorker, worker); err != nil {
		return nil, err
	}
	comment := managedidentity.Literal("XROUTE autonomous full uninstall")
	rows, err := client.list(ctx, "/rest/system/scheduler?.proplist=.id,name,comment")
	if err != nil {
		return nil, err
	}
	var existingID string
	for _, row := range rows {
		if text(row["name"]) != fullUninstallScheduler {
			continue
		}
		if existingID != "" {
			return nil, errors.New("full uninstall scheduler is ambiguous")
		}
		if text(row["comment"]) != comment {
			return nil, errors.New("refusing to replace an unowned full uninstall scheduler")
		}
		existingID = text(row[".id"])
		if existingID == "" {
			return nil, errors.New("full uninstall scheduler identifier is unavailable")
		}
	}
	if existingID != "" {
		if _, err := client.request(ctx, http.MethodDelete, "/rest/system/scheduler/"+routerOSResourceID(existingID), nil); err != nil {
			return nil, err
		}
	}
	if _, err := client.request(ctx, http.MethodPut, "/rest/system/scheduler", map[string]any{
		"name": fullUninstallScheduler, "interval": "10s", "on-event": "/system/script/run " + fullUninstallWorker,
		"policy": "read,write,test,policy", "comment": comment, "disabled": "false",
	}); err != nil {
		return nil, err
	}
	return map[string]any{"scheduled": true, "delay_seconds": 10, "scheduler": fullUninstallScheduler}, nil
}

func validateLifecycleStorageRoot(value string) (string, error) {
	return ValidateStorageRoot(value, true)
}

// ValidateStorageRoot validates a dedicated project directory. A bare root
// directory is only usable on CHR; generated workers verify that on the router
// before making changes. Physical routers keep the external-volume requirement.
func ValidateStorageRoot(value string, allowSystemDisk bool) (string, error) {
	root := strings.Trim(strings.TrimSpace(value), "/")
	lower := strings.ToLower(root)
	if !lifecycleStorageRootPattern.MatchString(root) || !allowSystemDisk && !strings.Contains(root, "/") || strings.HasPrefix(lower, "flash") || reservedStorageRootPattern.MatchString(lower) {
		return "", errors.New("managed storage root is unsafe")
	}
	for _, part := range strings.Split(root, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("managed storage root is unsafe")
		}
	}
	return root, nil
}

func withStorageRootGuard(root string, lines []string) []string {
	if strings.Contains(root, "/") {
		return lines
	}
	guard := `:local storageHost [/system/resource/get architecture-name]; :if (([:pick [/system/resource/get board-name] 0 3] != "CHR") || (($storageHost != "x86") && ($storageHost != "x86_64"))) do={ :error "XRoute: a system-disk project directory is supported only on x86 CHR" }`
	return append([]string{lines[0], guard}, lines[1:]...)
}

func renderFullUninstallWorker(root string) string {
	lines := []string{
		managedidentity.Literal("# XROUTE exact-scope full uninstall"),
		`:local sbScheduler [/system/scheduler/find where name="` + fullUninstallScheduler + `"]`,
		`:if ([:len $sbScheduler] = 1) do={ /system/scheduler/remove $sbScheduler }`,
		`:local storageEntry [/file/find where name="` + root + `"]`,
		managedidentity.Literal(`:if ([:len $storageEntry] != 1) do={ :error "XROUTE exact project storage directory is missing or ambiguous" }`),
		managedidentity.Literal(`:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]`),
		`:if ([:len $gate] = 1) do={ /ip/firewall/mangle/disable $gate }`,
		routerosassets.ManagedConnectionCleanupSource(),
		managedidentity.Literal(`:foreach state in=[/system/script/find where comment~"^XROUTE WG egress peer state "] do={`),
		`  :local stateData [/system/script/get $state source]`,
		`  :local separator [:find $stateData "|"]`,
		managedidentity.Literal(`  :if (($separator = nil) || ($separator < 2)) do={ :error "XROUTE WireGuard peer restore state is invalid" }`),
		`  :local peerKey [:pick $stateData 1 $separator]`,
		`  :local originalAllowed [:pick $stateData ($separator + 1) [:len $stateData]]`,
		`  :local peer [/interface/wireguard/peers/find where public-key=$peerKey]`,
		managedidentity.Literal(`  :if ([:len $peer] != 1) do={ :error "XROUTE WireGuard peer restore target is missing or ambiguous" }`),
		`  /interface/wireguard/peers/set $peer allowed-address=$originalAllowed`,
		`}`,
		managedidentity.Literal(`/system/scheduler/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/ip/firewall/mangle/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/ip/firewall/filter/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/ip/firewall/nat/remove [find where comment~"^XROUTE "]`),
		`/ip/firewall/address-list/remove [find where list="SB_PUBLIC_ABUSE"]`,
		managedidentity.Literal(`/ip/firewall/address-list/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`:do { /ipv6/firewall/filter/remove [find where comment~"^XROUTE "] } on-error={}`),
		managedidentity.Literal(`:do { /ipv6/firewall/address-list/remove [find where comment~"^XROUTE "] } on-error={}`),
		managedidentity.Literal(`/ip/route/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`:do { /interface/list/member/remove [find where comment~"^XROUTE "] } on-error={}`),
		managedidentity.Literal(`:foreach containerId in=[/container/find where comment="XROUTE container" || comment="XROUTE container previous"] do={`),
		`  :local containerRoot [/container/get $containerId root-dir]`,
		`  :if (([:pick $containerRoot 0 ` + fmt.Sprint(len("/"+root+"/root-")) + `] != "/` + root + `/root-") && ($containerRoot != "/` + root + managedidentity.Literal(`/root")) do={ :error "XROUTE container root is outside managed storage" }`),
		`  :if ([:typeof [:find $containerRoot "/" ` + fmt.Sprint(len("/"+root+"/root-")) + managedidentity.Literal(`]] != "nil") do={ :error "XROUTE container root is nested" }`),
		`  :do { /container/stop $containerId } on-error={}`,
		`  :local stopped false`,
		`  :local stopAttempt 0`,
		`  :while (($stopped = false) && ($stopAttempt < 60)) do={`,
		`    :do { :if ([/container/get $containerId running] = false) do={ :set stopped true } } on-error={}`,
		`    :do { :if ([/container/get $containerId stopped] = true) do={ :set stopped true } } on-error={}`,
		`    :do { :if ([/container/get $containerId status] = "stopped") do={ :set stopped true } } on-error={}`,
		`    :if ($stopped = false) do={ :set stopAttempt ($stopAttempt + 1); :delay 2s }`,
		`  }`,
		managedidentity.Literal(`  :if ($stopped = false) do={ :error "XROUTE container did not stop; storage was preserved" }`),
		`  /container/remove $containerId`,
		`}`,
		managedidentity.Literal(`/container/envs/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/container/mounts/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/ip/address/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/interface/bridge/port/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/interface/veth/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/interface/bridge/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/interface/list/remove [find where comment~"^XROUTE "]`),
		managedidentity.Literal(`/routing/table/remove [find where comment="XROUTE routing table"]`),
		managedidentity.Literal(`/user/remove [find where comment="XROUTE control-plane REST user"]`),
		managedidentity.Literal(`/user/group/remove [find where comment="XROUTE REST group"]`),
		`/file/remove [find where name~"^sb-gateway-(before-|pre-)"]`,
		`/file/remove $storageEntry`,
		managedidentity.Literal(`:log warning "XROUTE: full uninstall complete; foreign RouterOS objects were preserved"`),
		managedidentity.Literal(`:foreach scriptId in=[/system/script/find where comment~"^XROUTE "] do={ /system/script/remove $scriptId }`),
	}
	return strings.Join(withStorageRootGuard(root, lines), "\n")
}
