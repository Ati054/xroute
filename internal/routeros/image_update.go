package routeros

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	routerosassets "github.com/Ati054/xroute/routeros"

	"github.com/Ati054/xroute/internal/managedidentity"
)

var (
	imageUpdateWorker    = managedidentity.Literal("XROUTE-image-update-worker")
	imageUpdateScheduler = managedidentity.Literal("XROUTE-image-update")
	imageUpdateTransfer  = managedidentity.Literal("XROUTE-image-update-transfer")
)

var (
	imageVersionPattern  = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}(?:[-+][A-Za-z0-9._-]+)?$`)
	localImagePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{3,220}\.tar$`)
	registryImagePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:+-]{2,220}@sha256:[a-f0-9]{64}$`)
)

type ImageUpdateSpec struct {
	// CatalogSchema is a verified target-image capability, never inferred from
	// its version. Zero is legacy/unknown and cannot consume catalog state.
	CatalogSchema      int
	Architecture       string
	Version            string
	StorageRoot        string
	CandidateRoot      string
	CandidateSource    string
	CandidateReference string
	ContainerAddress   string
	KeepPrevious       bool
	// Offline recovery must not restart an already broken predecessor.
	StoppedPredecessor bool
}

// SetContainerMemoryLimits changes the limits of one already-owned container.
// RouterOS 7.24.2 restarts a running container on this change. Callers must
// validate ownership and must not use this from read-only status reconciliation.
func (client *Client) SetContainerMemoryLimits(ctx context.Context, id string, memoryHigh, memoryMax int64) error {
	if !regexp.MustCompile(`^\*[0-9A-Fa-f]+$`).MatchString(id) {
		return errors.New("RouterOS container identifier is invalid")
	}
	if memoryHigh < 16<<20 || memoryMax < memoryHigh || memoryMax > 8<<30 {
		return errors.New("RouterOS container memory limits are invalid")
	}
	_, err := client.request(ctx, http.MethodPatch, "/rest/container/"+routerOSResourceID(id), map[string]any{
		"memory-high": memoryHigh,
		"memory-max":  memoryMax,
	})
	return err
}

// ScheduleImageUpdate installs an idempotent RouterOS-owned state machine and
// only then arms its scheduler. Extraction, health probation and rollback keep
// running even while the container control plane is unavailable.
func (client *Client) ScheduleImageUpdate(ctx context.Context, spec ImageUpdateSpec) (map[string]any, error) {
	validated, err := validateImageUpdateSpec(spec)
	if err != nil {
		return nil, err
	}
	if _, err := client.installFixedManagedScript(ctx, imageUpdateWorker, renderImageUpdateWorker(validated)); err != nil {
		return nil, err
	}
	comment := managedidentity.Literal("XROUTE autonomous image update")
	rows, err := client.list(ctx, "/rest/system/scheduler?.proplist=.id,name,comment")
	if err != nil {
		return nil, err
	}
	var existingID string
	for _, row := range rows {
		if text(row["name"]) != imageUpdateScheduler {
			continue
		}
		if existingID != "" {
			return nil, errors.New("image update scheduler is ambiguous")
		}
		if text(row["comment"]) != comment {
			return nil, errors.New("refusing to replace an unowned image update scheduler")
		}
		existingID = text(row[".id"])
		if existingID == "" {
			return nil, errors.New("image update scheduler identifier is unavailable")
		}
	}
	if existingID != "" {
		if _, err := client.request(ctx, http.MethodDelete, "/rest/system/scheduler/"+routerOSResourceID(existingID), nil); err != nil {
			return nil, err
		}
	}
	if _, err := client.request(ctx, http.MethodPut, "/rest/system/scheduler", map[string]any{
		"name": imageUpdateScheduler, "interval": "5s", "on-event": "/system/script/run " + imageUpdateWorker,
		"policy": "read,write,test,policy", "comment": comment, "disabled": "false",
	}); err != nil {
		return nil, err
	}
	return map[string]any{"scheduled": true, "delay_seconds": 10, "scheduler": imageUpdateScheduler}, nil
}

func validateImageUpdateSpec(spec ImageUpdateSpec) (ImageUpdateSpec, error) {
	if spec.Architecture == "" {
		spec.Architecture = "arm64"
	}
	if spec.Architecture != "arm64" && spec.Architecture != "amd64" {
		return ImageUpdateSpec{}, errors.New("image architecture is unsupported")
	}
	if spec.CatalogSchema < 0 || spec.CatalogSchema > 1 {
		return ImageUpdateSpec{}, errors.New("image catalog data capability is invalid")
	}
	spec.Version = strings.TrimSpace(spec.Version)
	root, err := validateLifecycleStorageRoot(spec.StorageRoot)
	if err != nil {
		return ImageUpdateSpec{}, err
	}
	spec.StorageRoot = root
	spec.CandidateRoot = strings.Trim(strings.TrimSpace(spec.CandidateRoot), "/")
	wantRoot := root + "/root-" + spec.Version
	if !imageVersionPattern.MatchString(spec.Version) || spec.CandidateRoot != wantRoot {
		return ImageUpdateSpec{}, errors.New("image update version or candidate root is invalid")
	}
	spec.CandidateReference = strings.TrimSpace(spec.CandidateReference)
	switch spec.CandidateSource {
	case "local-file":
		if !localImagePattern.MatchString(spec.CandidateReference) || !strings.HasPrefix(spec.CandidateReference, root+"/data/lifecycle-uploads/") {
			return ImageUpdateSpec{}, errors.New("local image reference is outside the managed upload directory")
		}
	case "registry":
		if !registryImagePattern.MatchString(spec.CandidateReference) {
			return ImageUpdateSpec{}, errors.New("registry image reference must use an immutable sha256 digest")
		}
	default:
		return ImageUpdateSpec{}, errors.New("image update source is invalid")
	}
	ip := net.ParseIP(strings.TrimSpace(spec.ContainerAddress))
	if ip == nil || ip.To4() == nil {
		return ImageUpdateSpec{}, errors.New("container health address must be IPv4")
	}
	spec.ContainerAddress = ip.String()
	return spec, nil
}

func renderImageUpdateWorker(spec ImageUpdateSpec) string {
	quote := func(value string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`).Replace(value) + `"`
	}
	// Re-arm cleanup, never initial extraction, if the job dies between deleting
	// terminal metadata and deleting its scheduler.
	terminalEvent := `:local t [/system/script/find where name="` + imageUpdateTransfer + managedidentity.Literal(`"]; :if ([:len $t] > 1) do={ :error "XROUTE terminal transfer is ambiguous" }; :if ([:len $t] = 1) do={ :if ([/system/script/get $t comment] != "XROUTE image transfer metadata") do={ :error "XROUTE terminal transfer is not owned" }; :local r [:deserialize from=json value=[/system/script/get $t source]]; :if ((($r->"phase") != "restored") || (($r->"candidate-root") != "/`) + spec.CandidateRoot + `") || (($r->"reference") != "` + spec.CandidateReference + managedidentity.Literal(`")) do={ :error "XROUTE terminal transfer does not match" }; /system/script/remove $t }; /system/scheduler/remove [find where name="`) + imageUpdateScheduler + managedidentity.Literal(`" and comment="XROUTE autonomous image update"]`)
	startPrevious := func(id string) string {
		if spec.StoppedPredecessor {
			return `/container/set ` + id + ` start-on-boot=no; :do { /container/set ` + id + ` restart-policy=no } on-error={ /container/set ` + id + ` auto-restart-interval=0s }`
		}
		return `/container/set ` + id + ` start-on-boot=no; :if ([$storageReady ` + id + managedidentity.Literal(`] != true) do={ :error "XROUTE rollback storage is not ready" }; /container/start `) + id
	}
	retainPrevious := "false"
	startupDisabled := "false"
	if spec.StoppedPredecessor {
		startupDisabled = "true"
	}
	if spec.KeepPrevious {
		retainPrevious = "true"
	}
	addImage := `file="` + spec.CandidateReference + `"`
	cleanupArchive := `/file/remove [find where name="` + spec.CandidateReference + `"]`
	if spec.CandidateSource == "registry" {
		addImage = `remote-image="` + spec.CandidateReference + `"`
		cleanupArchive = ""
	}
	// A stale retained copy must never prevent emergency restoration of the
	// current transaction. Validate it only before a new update or commit cleanup.
	previousGuard := strings.Join([]string{
		managedidentity.Literal(`:if ([:len $previous] > 1) do={ :error "XROUTE previous image is ambiguous" }`),
		managedidentity.Literal(`:if ([:len $previousIf] > 0) do={ :if (([:len $previousIf] != 1) || ([/interface/veth/get $previousIf comment] != "XROUTE previous image hold")) do={ :error "XROUTE previous image interface is not owned" } }`),
		`:if ([:len $previous] = 1) do={`,
		`  :local previousRoot [/container/get $previous root-dir]`,
		`  :if (([:pick $previousRoot 0 ` + fmt.Sprint(len("/"+spec.StorageRoot+"/root-")) + `] != "/` + spec.StorageRoot + `/root-") && ($previousRoot != "/` + spec.StorageRoot + managedidentity.Literal(`/root")) do={ :error "XROUTE previous image root is not owned" }`),
		`  :if ([:typeof [:find $previousRoot "/" ` + fmt.Sprint(len("/"+spec.StorageRoot+"/root-")) + managedidentity.Literal(`]] = "num") do={ :error "XROUTE previous image root is nested" }`),
		`  :if ($previousRoot = "/` + spec.CandidateRoot + managedidentity.Literal(`") do={ :error "XROUTE candidate version is retained as previous image" }`),
		managedidentity.Literal(`  :if ([/container/get $previous stopped] != true) do={ :error "XROUTE previous image is not stopped" }`),
		managedidentity.Literal(`  :if ([:len $current] = 1) do={ :if ($previousRoot = [/container/get $current root-dir]) do={ :error "XROUTE previous image aliases current root" } }`),
		managedidentity.Literal(`  :foreach other in=[/container/find] do={ :if (($other != $previous) && ([/container/get $other root-dir] = $previousRoot)) do={ :error "XROUTE previous image root is shared" } }`),
		`}`,
	}, "\n")
	lines := []string{
		managedidentity.Literal("# XROUTE autonomous one-image switch"),
		imageHostArchitectureGuard(spec.Architecture),
		catalogImageWorkerCompatibilitySource(),
		`:local jobs [/system/script/job/find where script="` + imageUpdateWorker + `"]`,
		managedidentity.Literal(`:if ([:len [/system/script/job/find where script="XROUTE-container-startup"]] > 0) do={ :return true }`),
		`:if ([:len $jobs] > 1) do={ :return true }`,
		managedidentity.Literal(`:local current [/container/find where comment="XROUTE container"]`),
		managedidentity.Literal(`:local candidate [/container/find where comment="XROUTE container candidate"]`),
		managedidentity.Literal(`:local rollback [/container/find where comment="XROUTE container rollback"]`),
		managedidentity.Literal(`:local failed [/container/find where comment="XROUTE container failed"]`),
		managedidentity.Literal(`:local previous [/container/find where comment="XROUTE container previous"]`),
		managedidentity.Literal(`:local previousIf [/interface/veth/find where name="veth-xroute-previous"]`),
		`:local retainPrevious ` + retainPrevious,
		`:local scheduler [/system/scheduler/find where name="` + imageUpdateScheduler + `"]`,
		managedidentity.Literal(`:local hold [/interface/veth/find where comment="XROUTE image update hold"]`),
		managedidentity.Literal(`:local swap [/interface/veth/find where comment="XROUTE image update swap"]`),
		`:local candidateRoot "/` + spec.CandidateRoot + `"`,
		`:local transfer [/system/script/find where name="` + imageUpdateTransfer + `"]`,
		`:local receipt`,
		`:local phase ""`,
		`:local healthURL "http://` + spec.ContainerAddress + `:9080/healthz"`,
		`:local trafficURL "http://` + spec.ContainerAddress + `:9080/traffic-ready"`,
		`:global sbGatewayImageProbation`,
		`:global sbGatewayImageMisses`,
		managedidentity.Literal(`:if (([:len $current] > 1) || ([:len $candidate] > 1) || ([:len $rollback] > 1) || ([:len $scheduler] > 1) || ([:len $hold] > 1) || ([:len $swap] > 1)) do={ :error "XROUTE image update state is ambiguous" }`),
		managedidentity.Literal(`:if (([:len $failed] > 1) || ([:len $transfer] > 1)) do={ :error "XROUTE image transfer state is ambiguous" }`),
		managedidentity.Literal(`:if ([:len $hold] = 1) do={ :if ([/interface/veth/get $hold name] != "veth-xroute-update") do={ :error "XROUTE hold interface does not match" } }`),
		managedidentity.Literal(`:if ([:len $swap] = 1) do={ :if ([/interface/veth/get $swap name] != "veth-xroute-swap") do={ :error "XROUTE swap interface does not match" } }`),
		managedidentity.Literal(`:local startupProbe [/system/script/find where name="XROUTE-storage-ready"]`),
		managedidentity.Literal(`:local startupBoot [/system/script/find where name="XROUTE-container-startup"]`),
		managedidentity.Literal(`:local startupSchedule [/system/scheduler/find where name="XROUTE-container-startup"]`),
		`:local installStartup true`,
		`:if (([:len $startupProbe] = 1) && ([:len $startupBoot] = 1) && ([:len $startupSchedule] = 1)) do={`,
		managedidentity.Literal(`  :if (([/system/script/get $startupProbe comment] = "XROUTE storage readiness") && ([/system/script/get $startupBoot comment] = "XROUTE storage-aware startup") && ([/system/scheduler/get $startupSchedule comment] = "XROUTE storage-aware startup scheduler") && ([/system/script/get $startupProbe source] = `) + quote(routerosassets.ContainerStorageReadySource()) + `) && ([/system/script/get $startupBoot source] = ` + quote(routerosassets.ContainerBootSource()) + managedidentity.Literal(`) && ([/system/scheduler/get $startupSchedule interval] = 0s) && ([/system/scheduler/get $startupSchedule start-time] = "startup") && ([/system/scheduler/get $startupSchedule on-event] = "/system/script/run XROUTE-container-startup")) do={ :set installStartup false }`),
		`}`,
		`:if ($installStartup = true) do={ :local setup [:parse ` + quote(routerosassets.ContainerStartupInstallSource()) + `]; $setup ` + startupDisabled + ` }`,
		`:if (` + startupDisabled + managedidentity.Literal(` = true) do={ /system/scheduler/disable [find where name="XROUTE-container-startup" and comment="XROUTE storage-aware startup scheduler"] }`),
		managedidentity.Literal(`:local storageReady [:parse [/system/script/get [find where name="XROUTE-storage-ready" and comment="XROUTE storage readiness"] source]]`),
		// The inert JSON receipt survives interrupted detach/attach and role swaps.
		// An interrupted forward transfer restores the predecessor rather than
		// guessing whether the candidate was started before the job disappeared.
		`:if ([:len $transfer] = 1) do={`,
		managedidentity.Literal(`  :if ([/system/script/get $transfer comment] != "XROUTE image transfer metadata") do={ :error "XROUTE image transfer metadata is not owned" }`),
		`  :set receipt [:deserialize from=json value=[/system/script/get $transfer source]]`,
		`  :set phase ($receipt->"phase")`,
		`  :if (([:typeof $receipt] != "array") || (($receipt->"candidate-root") != $candidateRoot) || (($receipt->"reference") != "` + spec.CandidateReference + managedidentity.Literal(`") || (($phase != "transfer") && ($phase != "probation") && ($phase != "commit") && ($phase != "rollback") && ($phase != "restored"))) do={ :error "XROUTE image transfer metadata does not match" }`),
		`  :local oldRoot ($receipt->"previous-root")`,
		`  :if (([:typeof $oldRoot] != "str") || ($oldRoot = $candidateRoot) || (([:pick $oldRoot 0 ` + fmt.Sprint(len("/"+spec.StorageRoot+"/root-")) + `] != "/` + spec.StorageRoot + `/root-") && ($oldRoot != "/` + spec.StorageRoot + managedidentity.Literal(`/root"))) do={ :error "XROUTE image transfer previous root is invalid" }`),
		`  :if ([:typeof [:find $oldRoot "/" ` + fmt.Sprint(len("/"+spec.StorageRoot+"/root-")) + managedidentity.Literal(`]] = "num") do={ :error "XROUTE image transfer previous root is nested" }`),
		`  :if (([:typeof ($receipt->"catalog-schema")] != "nil") || (` + fmt.Sprint(spec.CatalogSchema) + ` != 0)) do={ :if (($receipt->"catalog-schema") != ` + fmt.Sprint(spec.CatalogSchema) + managedidentity.Literal(`) do={ :error "XROUTE image transfer catalog capability changed" } }`),
		`  :local savedMountlists ($receipt->"mountlists")`,
		`  :set savedMountlists [$catalogTargetMountlists $oldRoot $savedMountlists $catalogContract]`,
		`  :local savedIf ($receipt->"live-if")`,
		managedidentity.Literal(`  :if ([:typeof $savedIf] = "array") do={ :if ([:len $savedIf] != 1) do={ :error "XROUTE image transfer live interface is ambiguous" }; :foreach value in=$savedIf do={ :set savedIf $value } }`),
		managedidentity.Literal(`  :if (([:typeof $savedMountlists] != "array") || ([:len $savedMountlists] = 0) || ([:typeof $savedIf] != "str") || ([:typeof ($receipt->"previous-start-on-boot")] != "bool")) do={ :error "XROUTE image transfer persistent settings are invalid" }`),
		managedidentity.Literal(`  :foreach sbTransferMountlist in=$savedMountlists do={ :if (([:typeof $sbTransferMountlist] != "str") || ([:len $sbTransferMountlist] = 0) || ([:len [/container/mounts/find where list=$sbTransferMountlist]] = 0)) do={ :error "XROUTE image transfer mountlist is missing" } }`),
		`  :local savedVeth [/interface/veth/find where name=$savedIf]`,
		managedidentity.Literal(`  :if ([:len $savedVeth] != 1) do={ :error "XROUTE image transfer live interface is missing" }`),
		`  :if ([:pick [:tostr [/interface/veth/get $savedVeth address]] 0 ` + fmt.Sprint(len(spec.ContainerAddress)+1) + `] != "` + spec.ContainerAddress + managedidentity.Literal(`/") do={ :error "XROUTE image transfer live address does not match" }`),
		`  :local old [/container/find where root-dir=$oldRoot]`,
		`  :local next [/container/find where root-dir=$candidateRoot]`,
		managedidentity.Literal(`  :if (([:len $old] > 1) || ([:len $next] > 1) || (([:len $old] = 0) && ($phase != "commit")) || (([:len $next] = 0) && ($phase != "rollback") && ($phase != "restored"))) do={ :error "XROUTE image transfer roots are ambiguous" }`),
		managedidentity.Literal(`  :foreach item in=$old do={ :local role [/container/get $item comment]; :if (($role != "XROUTE container") && ($role != "XROUTE container rollback") && ($role != "XROUTE container previous")) do={ :error "XROUTE image transfer predecessor is not owned" } }`),
		managedidentity.Literal(`  :foreach item in=$next do={ :local role [/container/get $item comment]; :if (($role != "XROUTE container") && ($role != "XROUTE container candidate") && ($role != "XROUTE container failed")) do={ :error "XROUTE image transfer candidate is not owned" } }`),
		managedidentity.Literal(`  :foreach item in=[/container/find] do={ :if (($item != $old) && ($item != $next)) do={ :if ([/container/get $item interface] = $savedIf) do={ :error "XROUTE image transfer live interface is shared" }; :local role [/container/get $item comment]; :if (($role = "XROUTE container") || ($role = "XROUTE container candidate") || ($role = "XROUTE container rollback") || ($role = "XROUTE container failed")) do={ :error "XROUTE image transfer role belongs to another root" } } }`),
		`  :if (($phase = "transfer") || ($phase = "rollback") || ($phase = "restored")) do={`,
		managedidentity.Literal(`    :if ($phase = "restored") do={ :if (([:len $next] != 0) || ([/container/get $old comment] != "XROUTE container") || ([/container/get $old interface] != $savedIf) || ([:serialize to=json value=[/container/get $old mountlists]] != [:serialize to=json value=$savedMountlists])) do={ :error "XROUTE terminal restored state does not match" } }`),
		`    :if ($phase != "restored") do={`,
		`    :set ($receipt->"phase") "rollback"`,
		`    /system/script/set $transfer source=[:serialize to=json value=$receipt]`,
		managedidentity.Literal(`    :local recoveryGate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]`),
		managedidentity.Literal(`    :if ([:len $recoveryGate] > 1) do={ :error "XROUTE recovery diversion gate is ambiguous" }`),
		`    :if ([:len $recoveryGate] = 1) do={ /ip/firewall/mangle/disable $recoveryGate }`,
		`    :foreach item in=$next do={ :do { /container/set $item restart-policy=no } on-error={ /container/set $item auto-restart-interval=0s }; /container/set $item start-on-boot=no }`,
		`    :foreach item in=$old do={ :do { /container/stop $item } on-error={} }`,
		`    :foreach item in=$next do={ :do { /container/stop $item } on-error={} }`,
		`    :local stopTry 0`,
		`    :local stopped false`,
		`    :while (($stopped = false) && ($stopTry < 60)) do={`,
		`      :set stopped true`,
		`      :foreach item in=$old do={ :if ([/container/get $item stopped] != true) do={ :set stopped false } }`,
		`      :foreach item in=$next do={ :if ([/container/get $item stopped] != true) do={ :set stopped false } }`,
		`      :if ($stopped = false) do={ :set stopTry ($stopTry + 1); :delay 2s }`,
		`    }`,
		managedidentity.Literal(`    :if ($stopped = false) do={ :error "XROUTE image transfer containers did not stop" }`),
		`    :foreach item in=$next do={ /container/set $item mountlists=""; /container/remove $item }`,
		`    :set rollback $old`,
		`    /container/set $rollback mountlists=$savedMountlists`,
		managedidentity.Literal(`    /container/set $rollback interface=$savedIf comment="XROUTE container"`),
		`    ` + startPrevious("$rollback"),
		`    }`,
		`    :if ([:len $swap] = 1) do={ /interface/veth/remove $swap }`,
		`    :if ([:len $hold] = 1) do={ /interface/veth/remove $hold }`,
		`    :set ($receipt->"phase") "restored"`,
		`    /system/script/set $transfer source=[:serialize to=json value=$receipt]`,
		`    :if ([:len $scheduler] = 1) do={ /system/scheduler/set $scheduler on-event=` + quote(terminalEvent) + ` }`,
		`    /system/script/remove $transfer`,
		`    :if ([:len $scheduler] = 1) do={ /system/scheduler/remove $scheduler }`,
		`    :set sbGatewayImageProbation`,
		`    :set sbGatewayImageMisses`,
		managedidentity.Literal(`    :log warning "XROUTE: image transfer restored predecessor; persistent mounts restored"`),
		`    :return true`,
		`  }`,
		`}`,
		managedidentity.Literal(`:if ([:len $failed] > 0) do={ :error "XROUTE failed image has no recoverable transfer metadata" }`),

		// Completed cleanup: the candidate already owns the canonical role.
		`:if (([:len $current] = 1) && ([:len $candidate] = 0) && ([:len $rollback] = 0)) do={`,
		`  :if ([/container/get $current root-dir] = $candidateRoot) do={`,
		managedidentity.Literal(`    /system/scheduler/enable [find where name="XROUTE-container-startup" and comment="XROUTE storage-aware startup scheduler"]`),
		`    :if ([:len $hold] = 1) do={ /interface/veth/remove $hold }`,
		`    :if ([:len $swap] = 1) do={ /interface/veth/remove $swap }`,
		`    :if ([:len $transfer] = 1) do={ /system/script/remove $transfer }`,
		`    :if ([:len $scheduler] = 1) do={ /system/scheduler/remove $scheduler }`,
		`    :return true`,
		`  }`,
		`}`,

		// Interrupted before role swap: discard only the exact candidate.
		`:if (([:len $current] = 1) && ([:len $candidate] = 1) && ([:len $rollback] = 0)) do={`,
		`  :local recoveryMountlists [/container/get $current mountlists]`,
		`  :if ([:len $recoveryMountlists] = 0) do={ :set recoveryMountlists [/container/get $candidate mountlists] }`,
		`  :do { /container/stop $candidate } on-error={}`,
		`  :local candidateStopped false`,
		`  :local stopTry 0`,
		`  :while (($candidateStopped = false) && ($stopTry < 30)) do={`,
		`    :do { :if ([/container/get $candidate running] = false) do={ :set candidateStopped true } } on-error={}`,
		`    :do { :if ([/container/get $candidate status] = "stopped") do={ :set candidateStopped true } } on-error={}`,
		`    :do { :if ([/container/get $candidate stopped] = true) do={ :set candidateStopped true } } on-error={}`,
		`    :if ($candidateStopped = false) do={ :set stopTry ($stopTry + 1); :delay 2s }`,
		`  }`,
		managedidentity.Literal(`  :if ($candidateStopped = false) do={ :error "XROUTE interrupted candidate did not stop" }`),
		`  /container/set $candidate mountlists=""`,
		`  :if (([:len [/container/get $current mountlists]] = 0) && ([:len $recoveryMountlists] > 0)) do={ /container/set $current mountlists=$recoveryMountlists }`,
		`  /container/remove $candidate`,
		`  :if ([:len $hold] = 1) do={ /interface/veth/remove $hold }`,
		`  :if ([:len $swap] = 1) do={ /interface/veth/remove $swap }`,
		`  :do { :if ([/container/get $current stopped] = true) do={ ` + startPrevious("$current") + ` } } on-error={}`,
		`  :if ([:len $scheduler] = 1) do={ /system/scheduler/remove $scheduler }`,
		managedidentity.Literal(`  :log warning "XROUTE: interrupted image extraction was cleaned up"`),
		`  :return true`,
		`}`,

		// Probation: three consecutive traffic-admitted scheduler passes commit; any
		// container restart or sixty misses restore the old image. Large GeoIP
		// configs can need a longer cold Xray start on single-core RouterOS.
		`:if (([:len $current] = 1) && ([:len $candidate] = 0) && ([:len $rollback] = 1)) do={`,
		`  :local healthy false`,
		`  :do { :local probe ([/tool/fetch url=$healthURL output=user as-value]->"data"); :if ([:typeof [:find $probe "\"ready\":true"]] = "num") do={ :set healthy true } } on-error={}`,
		`  :local trafficReady false`,
		`  :do { :local trafficProbe ([/tool/fetch url=$trafficURL output=user as-value]->"data"); :if ([:typeof [:find $trafficProbe "\"traffic_ready\":true"]] = "num") do={ :set trafficReady true } } on-error={}`,
		managedidentity.Literal(`  :local probationGate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]`),
		`  :local trafficAdmitted false`,
		`  :if ([:len $probationGate] = 1) do={ :if ($trafficReady = true) do={ /ip/firewall/mangle/enable $probationGate } else={ /ip/firewall/mangle/disable $probationGate } }`,
		`  :if (([:len $probationGate] = 1) && ($trafficReady = true)) do={ :set trafficAdmitted ([/ip/firewall/mangle/get $probationGate disabled] = false) }`,
		`  :local restartCount 0`,
		`  :do { :set restartCount [/container/get $current restart-count] } on-error={}`,
		`  :if ([:typeof $sbGatewayImageProbation] = "nil") do={ :set sbGatewayImageProbation 0 }`,
		`  :if ([:typeof $sbGatewayImageMisses] = "nil") do={ :set sbGatewayImageMisses 0 }`,
		`  :if (($healthy = true) && ($trafficReady = true) && ($trafficAdmitted = true) && ($restartCount = 0)) do={ :set sbGatewayImageProbation ($sbGatewayImageProbation + 1); :set sbGatewayImageMisses 0 } else={ :set sbGatewayImageProbation 0; :set sbGatewayImageMisses ($sbGatewayImageMisses + 1) }`,
		`  :if ($sbGatewayImageProbation >= 3) do={`,
		`    :if ([:len $transfer] = 1) do={ :set ($receipt->"phase") "commit"; /system/script/set $transfer source=[:serialize to=json value=$receipt] }`,
		catalogImageArrivalSource(spec.CatalogSchema),
		managedidentity.Literal(`    :if ([/container/get $current start-on-boot] != false) do={ :error "XROUTE candidate boot policy changed during probation" }`),
		previousGuard,
		`    :if ([:len $previous] = 1) do={ /container/remove $previous }`,
		`    :if ($retainPrevious = true) do={`,
		managedidentity.Literal(`      :if ([:len $previousIf] = 0) do={ /interface/veth/add name="veth-xroute-previous" address=192.0.2.9/30 gateway=192.0.2.10 comment="XROUTE previous image hold" }`),
		`      :do { /container/set $rollback restart-policy=no } on-error={ /container/set $rollback auto-restart-interval=0s }`,
		managedidentity.Literal(`      /container/set $rollback interface="veth-xroute-previous" start-on-boot=no comment="XROUTE container previous"`),
		`    } else={`,
		`      /container/remove $rollback`,
		`      :if ([:len $previousIf] = 1) do={ /interface/veth/remove $previousIf }`,
		`    }`,
		`    :if ([:len $hold] = 1) do={ /interface/veth/remove $hold }`,
		`    :if ([:len $swap] = 1) do={ /interface/veth/remove $swap }`,
		managedidentity.Literal(`    /system/scheduler/enable [find where name="XROUTE-container-startup" and comment="XROUTE storage-aware startup scheduler"]`),
		`    :if ([:len $transfer] = 1) do={ /system/script/remove $transfer }`,
		`    :if ([:len $scheduler] = 1) do={ /system/scheduler/remove $scheduler }`,
		`    :set sbGatewayImageProbation`,
		`    :set sbGatewayImageMisses`,
		managedidentity.Literal(`    :log info "XROUTE: image update committed after probation"`),
		`    :return true`,
		`  }`,
		`  :if (($sbGatewayImageMisses < 60) && ($restartCount = 0)) do={ :return true }`,
		`  :if ([:len $probationGate] = 1) do={ /ip/firewall/mangle/disable $probationGate }`,
		`  :if ([:len $transfer] = 1) do={`,
		`    :set ($receipt->"phase") "rollback"`,
		`    /system/script/set $transfer source=[:serialize to=json value=$receipt]`,
		`    :return true`,
		`  }`,
		`  :local failed $current`,
		`  :local failedMountlists [/container/get $failed mountlists]`,
		`  :if ([:len $failedMountlists] = 0) do={ :set failedMountlists [/container/get $rollback mountlists] }`,
		managedidentity.Literal(`  :if ([:len $failedMountlists] = 0) do={ :error "XROUTE rollback mountlists are missing" }`),
		`  :set failedMountlists [$catalogTargetMountlists [/container/get $rollback root-dir] $failedMountlists $catalogContract]`,
		`  :do { /container/stop $failed } on-error={}`,
		`  :local failedStopped false`,
		`  :local failedStopTry 0`,
		`  :while (($failedStopped = false) && ($failedStopTry < 60)) do={`,
		`    :do { :if ([/container/get $failed running] = false) do={ :set failedStopped true } } on-error={}`,
		`    :do { :if ([/container/get $failed status] = "stopped") do={ :set failedStopped true } } on-error={}`,
		`    :do { :if ([/container/get $failed stopped] = true) do={ :set failedStopped true } } on-error={}`,
		`    :if ($failedStopped = false) do={ :set failedStopTry ($failedStopTry + 1); :delay 2s }`,
		`  }`,
		managedidentity.Literal(`  :if ($failedStopped = false) do={ :error "XROUTE failed candidate did not stop for rollback" }`),
		`  :local liveIf [/container/get $failed interface]`,
		`  /container/set $failed mountlists=""`,
		`  /container/set $rollback mountlists=$failedMountlists`,
		managedidentity.Literal(`  :if ([:len $swap] = 0) do={ /interface/veth/add name="veth-xroute-swap" address=192.0.2.5/30 gateway=192.0.2.6 comment="XROUTE image update swap"; :set swap [/interface/veth/find where comment="XROUTE image update swap"] }`),
		managedidentity.Literal(`  /container/set $rollback interface="veth-xroute-swap"`),
		managedidentity.Literal(`  /container/set $failed interface="veth-xroute-update" comment="XROUTE container failed"`),
		managedidentity.Literal(`  /container/set $rollback interface=$liveIf comment="XROUTE container"`),
		`  /interface/veth/remove $swap`,
		`  ` + startPrevious("$rollback"),
		`  /container/remove $failed`,
		`  :if ([:len $hold] = 1) do={ /interface/veth/remove $hold }`,
		`  :if ([:len $scheduler] = 1) do={ /system/scheduler/remove $scheduler }`,
		`  :set sbGatewayImageProbation`,
		`  :set sbGatewayImageMisses`,
		managedidentity.Literal(`  :log error "XROUTE: image update rolled back after failed probation"`),
		`  :return true`,
		`}`,

		managedidentity.Literal(`:if (([:len $current] != 1) || ([:len $candidate] != 0) || ([:len $rollback] != 0)) do={ :error "XROUTE image update state is not recoverable" }`),
		previousGuard,
		managedidentity.Literal(`:if ([:len $previous] = 1) do={ :local preserveCompat false; :if ([:typeof $catalogContract] = "array") do={ :local previousRoot [:tostr [/container/get $previous root-dir]]; :if ([:pick $previousRoot 0 1] = "/") do={ :set previousRoot [:pick $previousRoot 1 [:len $previousRoot]] }; :if (($previousRoot = ($catalogContract->"previous_root")) && (($catalogContract->"previous_catalog_schema") = 0)) do={ :local expectedCompat [$catalogTargetMountlists $previousRoot [/container/get $previous mountlists] $catalogContract]; :if ([:serialize to=json value=[/container/get $previous mountlists]] != [:serialize to=json value=$expectedCompat]) do={ :error "XROUTE previous compatibility mount binding changed" }; :set preserveCompat true } }; :if ($preserveCompat = false) do={ /container/set $previous mountlists="" } }`),
		`:if (([:typeof $catalogContract] = "array") && (` + fmt.Sprint(spec.CatalogSchema) + managedidentity.Literal(` < 1)) do={ :error "XROUTE target image catalog capability is unconfirmed; use matching private compatibility handoff" }`),
		managedidentity.Literal(`:if ([:len $hold] = 0) do={ /interface/veth/add name="veth-xroute-update" address=192.0.2.1/30 gateway=192.0.2.2 comment="XROUTE image update hold"; :set hold [/interface/veth/find where comment="XROUTE image update hold"] }`),
		`:local liveIf [:tostr [/container/get $current interface]]`,
		`:local mountlists [/container/get $current mountlists]`,
		`:local envlist [/container/get $current envlist]`,
		`:local dns [/container/get $current dns]`,
		`:local logging [/container/get $current logging]`,
		`:local memoryHigh [/container/get $current memory-high]`,
		`:local memoryMax [/container/get $current memory-max]`,
		`/container/add ` + addImage + managedidentity.Literal(` root-dir=$candidateRoot interface="veth-xroute-update" envlist=$envlist dns=$dns logging=$logging start-on-boot=no memory-high=$memoryHigh memory-max=$memoryMax comment="XROUTE container candidate"`),
		managedidentity.Literal(`:set candidate [/container/find where comment="XROUTE container candidate"]`),
		managedidentity.Literal(`:if ([:len $candidate] != 1) do={ :error "XROUTE candidate was not created" }`),
		`:local extracted false`,
		`:local extractTry 0`,
		`:while (($extracted = false) && ($extractTry < 180)) do={`,
		`  :do { :if ([/container/get $candidate status] = "stopped") do={ :set extracted true } } on-error={}`,
		`  :do { :if ([/container/get $candidate stopped] = true) do={ :set extracted true } } on-error={}`,
		`  :if ($extracted = false) do={ :set extractTry ($extractTry + 1); :delay 2s }`,
		`}`,
		managedidentity.Literal(`:if ($extracted = false) do={ :error "XROUTE candidate extraction timed out" }`),
		managedidentity.Literal(`:if (([:typeof $mountlists] != "array") || ([:len $mountlists] = 0)) do={ :error "XROUTE persistent mountlists are missing" }`),
		`:set receipt {"candidate-root"=$candidateRoot;"previous-root"=[/container/get $current root-dir];"previous-id"=[:tostr $current];"candidate-id"=[:tostr $candidate];"reference"="` + spec.CandidateReference + `";"catalog-schema"=` + fmt.Sprint(spec.CatalogSchema) + `;"mountlists"=$mountlists;"live-if"=$liveIf;"previous-start-on-boot"=[/container/get $current start-on-boot];"phase"="transfer"}`,
		`/system/script/add name="` + imageUpdateTransfer + managedidentity.Literal(`" policy=read comment="XROUTE image transfer metadata" source=[:serialize to=json value=$receipt]`),
		`:set transfer [/system/script/find where name="` + imageUpdateTransfer + `"]`,
		managedidentity.Literal(`:if ([:len $transfer] != 1) do={ :error "XROUTE image transfer metadata was not created" }`),
		`:local readback [:deserialize from=json value=[/system/script/get $transfer source]]`,
		managedidentity.Literal(`:if ([:serialize to=json value=$readback] != [:serialize to=json value=$receipt]) do={ :error "XROUTE image transfer metadata readback failed" }`),
		`:do { /container/set $candidate restart-policy=always restart-interval=10s } on-error={ /container/set $candidate auto-restart-interval=10s }`,
		managedidentity.Literal(`:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]`),
		`:if ([:len $gate] = 1) do={ /ip/firewall/mangle/disable $gate }`,
		routerosassets.ManagedConnectionCleanupSource(),
		`:if ([/container/get $current stopped] != true) do={ /container/stop $current }`,
		`:local currentStopped false`,
		`:local currentStopTry 0`,
		`:while (($currentStopped = false) && ($currentStopTry < 60)) do={`,
		`  :do { :if ([/container/get $current running] = false) do={ :set currentStopped true } } on-error={}`,
		`  :do { :if ([/container/get $current status] = "stopped") do={ :set currentStopped true } } on-error={}`,
		`  :do { :if ([/container/get $current stopped] = true) do={ :set currentStopped true } } on-error={}`,
		`  :if ($currentStopped = false) do={ :set currentStopTry ($currentStopTry + 1); :delay 2s }`,
		`}`,
		managedidentity.Literal(`:if ($currentStopped = false) do={ :error "XROUTE current container did not stop; diversion remains fail-open" }`),
		`/container/set $current start-on-boot=no`,
		`:do {`,
		`  /container/set $current mountlists=""`,
		`  /container/set $candidate mountlists=$mountlists`,
		`} on-error={`,
		`  :do { /container/set $candidate mountlists="" } on-error={}`,
		`  :do { /container/set $current mountlists=$mountlists } on-error={}`,
		`  :do { ` + startPrevious("$current") + ` } on-error={}`,
		managedidentity.Literal(`  :error "XROUTE persistent mount ownership transfer failed; diversion remains fail-open"`),
		`}`,
		managedidentity.Literal(`:if ([:len $swap] = 0) do={ /interface/veth/add name="veth-xroute-swap" address=192.0.2.5/30 gateway=192.0.2.6 comment="XROUTE image update swap"; :set swap [/interface/veth/find where comment="XROUTE image update swap"] }`),
		managedidentity.Literal(`/container/set $candidate interface="veth-xroute-swap"`),
		managedidentity.Literal(`/container/set $current interface="veth-xroute-update" comment="XROUTE container rollback"`),
		managedidentity.Literal(`/container/set $candidate interface=$liveIf comment="XROUTE container"`),
		`/interface/veth/remove $swap`,
		// RouterOS 7.24.2 restarts a running container when start-on-boot changes.
		// Set it while stopped, after the canonical mounts/interface transfer.
		`/container/set $candidate start-on-boot=no`,
		managedidentity.Literal(`:if ([$storageReady $candidate] != true) do={ :error "XROUTE candidate storage is not ready" }`),
		`/container/start $candidate`,
		`:set ($receipt->"phase") "probation"`,
		`/system/script/set $transfer source=[:serialize to=json value=$receipt]`,
		`:set sbGatewayImageProbation 0`,
		`:set sbGatewayImageMisses 0`,
		cleanupArchive,
		managedidentity.Literal(`:log warning "XROUTE: candidate image started; health probation active"`),
	}
	filtered := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(withStorageRootGuard(spec.StorageRoot, filtered), "\n")
}

func imageHostArchitectureGuard(architecture string) string {
	if architecture == "amd64" {
		return `:local imageHost [/system/resource/get architecture-name]; :if (($imageHost != "x86") && ($imageHost != "x86_64")) do={ :error "XRoute: AMD64 image does not match this router architecture" }`
	}
	return `:if ([/system/resource/get architecture-name] != "arm64") do={ :error "XRoute: ARM64 image does not match this router architecture" }`
}
