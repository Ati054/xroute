package routeros

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Ati054/xroute/internal/managedidentity"
)

// RenderOfflineImageRecovery reuses the image worker without a running panel.
// The operator stops the broken container first. Persistent directories are
// not copied or removed. A failed candidate keeps the predecessor stopped.
func RenderOfflineImageRecovery(spec ImageUpdateSpec, archiveSize int64, identity string) (string, error) {
	spec, err := validateImageUpdateSpec(spec)
	if err != nil {
		return "", err
	}
	if spec.CandidateSource != "local-file" || archiveSize <= 0 || archiveSize > 2<<30 {
		return "", errors.New("offline recovery requires a bounded local archive")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`).MatchString(identity) {
		return "", errors.New("offline recovery router identity is invalid")
	}
	spec.KeepPrevious, spec.StoppedPredecessor = true, true
	worker := renderImageUpdateWorker(spec)
	if !validFixedManagedScript(imageUpdateWorker, worker) {
		return "", errors.New("offline recovery worker is invalid")
	}
	quote := func(value string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`).Replace(value) + `"`
	}
	rootPrefix := "/" + spec.StorageRoot + "/root-"
	lines := []string{
		managedidentity.Literal("# XROUTE offline image recovery; persistent directories are preserved."),
		imageHostArchitectureGuard(spec.Architecture),
		"{",
		`:if ([/system/identity/get name] != ` + quote(identity) + managedidentity.Literal(`) do={ :error "XROUTE recovery router identity mismatch" }`),
		managedidentity.Literal(`:local current [/container/find where comment="XROUTE container"]`),
		managedidentity.Literal(`:if ([:len $current] != 1) do={ :error "XROUTE recovery current container is ambiguous" }`),
		managedidentity.Literal(`:if ([/container/get $current stopped] != true) do={ :error "XROUTE stop the broken container before recovery" }`),
		managedidentity.Literal(`:if (([:len [/container/get $current mountlists]] = 0) || ([:len [/container/get $current envlist]] = 0)) do={ :error "XROUTE recovery persistent settings are missing" }`),
		`:local currentRoot [/container/get $current root-dir]`,
		`:if (([:pick $currentRoot 0 ` + fmt.Sprint(len(rootPrefix)) + `] != ` + quote(rootPrefix) + `) && ($currentRoot != ` + quote("/"+spec.StorageRoot+"/root") + managedidentity.Literal(`)) do={ :error "XROUTE recovery root does not match current container" }`),
		`:if ([:typeof [:find $currentRoot "/" ` + fmt.Sprint(len(rootPrefix)) + managedidentity.Literal(`]] = "num") do={ :error "XROUTE recovery current root is nested" }`),
		`:if ($currentRoot = ` + quote("/"+spec.CandidateRoot) + managedidentity.Literal(`) do={ :error "XROUTE recovery requires a different image version" }`),
		`:local liveIf [/interface/veth/find where name=[/container/get $current interface]]`,
		managedidentity.Literal(`:if ([:len $liveIf] != 1) do={ :error "XROUTE recovery live veth is ambiguous" }`),
		`:if ([:pick [:tostr [/interface/veth/get $liveIf address]] 0 ` + fmt.Sprint(len(spec.ContainerAddress)+1) + `] != ` + quote(spec.ContainerAddress+"/") + managedidentity.Literal(`) do={ :error "XROUTE recovery container address mismatch" }`),
		managedidentity.Literal(`:if (([:len [/system/scheduler/find where name="XROUTE-image-update"]] > 0) || ([:len [/system/script/job/find where script="XROUTE-image-update-worker"]] > 0) || ([:len [/system/scheduler/find where name~"^XROUTE-safe-rollback-"]] > 0) || ([:len [/system/script/job/find where script~"^XROUTE-(apply|rollback)-"]] > 0)) do={ :error "XROUTE recovery conflicts with an active transaction" }`),
		managedidentity.Literal(`:if (([:len [/container/find where comment="XROUTE container candidate"]] > 0) || ([:len [/container/find where comment="XROUTE container rollback"]] > 0) || ([:len [/interface/veth/find where name="veth-xroute-update"]] > 0) || ([:len [/interface/veth/find where name="veth-xroute-swap"]] > 0)) do={ :error "XROUTE recovery conflicts with an unfinished image update" }`),
		managedidentity.Literal(`:if (([:len [/system/script/find where name="XROUTE-image-update-transfer"]] > 0) || ([:len [/container/find where comment="XROUTE container failed"]] > 0)) do={ :error "XROUTE recovery conflicts with image transfer metadata" }`),
		`:if (([:len [/file/find where name=` + quote(spec.CandidateRoot) + `]] > 0) || ([:len [/file/find where name=` + quote("/"+spec.CandidateRoot) + managedidentity.Literal(`]] > 0)) do={ :error "XROUTE recovery candidate root already exists" }`),
		`:local archive [/file/find where name=` + quote(spec.CandidateReference) + `]`,
		managedidentity.Literal(`:if ([:len $archive] != 1) do={ :error "XROUTE recovery archive is missing or ambiguous" }`),
		`:if ([/file/get $archive size] != ` + fmt.Sprint(archiveSize) + managedidentity.Literal(`) do={ :error "XROUTE recovery archive size mismatch" }`),
		managedidentity.Literal(`:local script [/system/script/find where name="XROUTE-image-update-worker"]`),
		managedidentity.Literal(`:if ([:len $script] > 1) do={ :error "XROUTE recovery worker is ambiguous" }`),
		managedidentity.Literal(`:if ([:len $script] = 1) do={ :if ([/system/script/get $script comment] != "XROUTE managed script XROUTE-image-update-worker") do={ :error "XROUTE recovery worker is not owned" } }`),
		managedidentity.Literal(`:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]`),
		managedidentity.Literal(`:if ([:len $gate] > 1) do={ :error "XROUTE recovery diversion gate is ambiguous" }`),
		`:local workerSource ` + quote(worker),
		managedidentity.Literal(`:if ([:len $script] = 0) do={ /system/script/add name="XROUTE-image-update-worker" policy=read,write,test,policy comment="XROUTE managed script XROUTE-image-update-worker" source=$workerSource } else={ /system/script/set $script source=$workerSource }`),
		`:if ([:len $gate] = 1) do={ /ip/firewall/mangle/disable $gate }`,
		`/container/set $current start-on-boot=no`,
		`:do { /container/set $current restart-policy=no } on-error={ /container/set $current auto-restart-interval=0s }`,
		managedidentity.Literal(`/system/scheduler/add name="XROUTE-image-update" interval=5s on-event="/system/script/run XROUTE-image-update-worker" policy=read,write,test,policy comment="XROUTE autonomous image update" disabled=no`),
		managedidentity.Literal(`:put "XROUTE offline recovery scheduled; inspect container health and image-update log"`),
		"}",
	}
	return strings.Join(withStorageRootGuard(spec.StorageRoot, lines), "\n") + "\n", nil
}
