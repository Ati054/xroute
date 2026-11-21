package routeros

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Ati054/xroute/internal/managedidentity"
)

var containerMemoryWorker = managedidentity.Literal("XROUTE-container-memory-worker")
var ContainerMemoryScheduler = managedidentity.Literal("XROUTE-container-memory-once")

// The one-shot deletes its scheduler before changing container properties.
// A running worker still owns the operation after the scheduler disappears.
func (client *Client) ContainerMemoryUpdatePending(ctx context.Context) (bool, error) {
	schedulers, err := client.List(ctx, "/rest/system/scheduler?.proplist=name")
	if err != nil {
		return false, err
	}
	for _, scheduler := range schedulers {
		if text(scheduler["name"]) == ContainerMemoryScheduler {
			return true, nil
		}
	}
	jobs, err := client.List(ctx, "/rest/system/script/job?.proplist=script")
	if err != nil {
		return false, err
	}
	for _, job := range jobs {
		if text(job["script"]) == containerMemoryWorker {
			return true, nil
		}
	}
	return false, nil
}

func ValidContainerMemoryLimits(high, maximum int64) bool {
	return high >= 16<<20 && high <= maximum && maximum <= 8<<30
}

// ScheduleContainerMemoryUpdate keeps the operation on RouterOS when changing
// limits restarts the HTTP server. The one-shot is consumed before mutation.
func (client *Client) ScheduleContainerMemoryUpdate(ctx context.Context, root string, oldHigh, oldMax, high, maximum int64) (map[string]any, error) {
	root = strings.Trim(root, "/")
	if !lifecycleStorageRootPattern.MatchString(root) || strings.Contains(root, "..") || !ValidContainerMemoryLimits(high, maximum) || oldHigh < 0 || oldMax < 0 {
		return nil, errors.New("container memory settings are invalid")
	}
	rows, err := client.list(ctx, "/rest/system/scheduler?.proplist=.id,name,comment")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		name := text(row["name"])
		if name == ContainerMemoryScheduler || name == imageUpdateScheduler || name == recoveryRestartScheduler || name == fullUninstallScheduler || strings.HasPrefix(name, managedidentity.Literal("XROUTE-safe-rollback-")) {
			return nil, errors.New("another RouterOS lifecycle operation is active")
		}
	}
	if _, err := client.installFixedManagedScript(ctx, containerMemoryWorker, renderContainerMemoryWorker(root, oldHigh, oldMax, high, maximum)); err != nil {
		return nil, err
	}
	if _, err := client.request(ctx, http.MethodPut, "/rest/system/scheduler", map[string]any{
		"name": ContainerMemoryScheduler, "interval": "10s", "on-event": "/system/script/run " + containerMemoryWorker,
		"policy": "read,write,test,policy", "comment": managedidentity.Literal("XROUTE one-shot container memory update"), "disabled": "false",
	}); err != nil {
		return nil, err
	}
	return map[string]any{"scheduled": true, "delay_seconds": 10}, nil
}

func renderContainerMemoryWorker(root string, oldHigh, oldMax, high, maximum int64) string {
	return strings.Join([]string{
		managedidentity.Literal("# XROUTE one-shot container memory update"),
		`:local jobs [/system/script/job/find where script="` + containerMemoryWorker + `"]; :if ([:len $jobs] > 1) do={ :error "Memory worker already running" }`,
		`:local scheduler [/system/scheduler/find where name="` + ContainerMemoryScheduler + managedidentity.Literal(`" and comment="XROUTE one-shot container memory update"]; :if ([:len $scheduler] != 1) do={ :error "Memory scheduler ownership changed" }`),
		`/system/scheduler/disable $scheduler`,
		`/system/scheduler/remove $scheduler`,
		managedidentity.Literal(`:local target [/container/find where comment="XROUTE container"]; :if ([:len $target] != 1) do={ :error "Managed container is missing or ambiguous" }`),
		`:if ([/container/get $target root-dir] != "/` + root + `") do={ :error "Managed container root changed" }`,
		`:if ([/container/get $target stopped] = true) do={ :error "Managed container is not running" }`,
		`:local autoRestart false; :do { :set autoRestart ([/container/get $target restart-policy] = "always") } on-error={ :set autoRestart ([/container/get $target auto-restart-interval] > 0s) }; :if ($autoRestart != true) do={ :error "Automatic container restart must be enabled" }`,
		managedidentity.Literal(`:if (([:len [/system/scheduler/find where name="XROUTE-image-update"]] > 0) || ([:len [/system/scheduler/find where name~"^XROUTE-safe-rollback-"]] > 0) || ([:len [/system/script/job/find where script~"^XROUTE-(apply|rollback)-"]] > 0)) do={ :error "Memory update conflicts with a transaction" }`),
		fmt.Sprintf(`:if (([/container/get $target memory-high] != %d) || ([/container/get $target memory-max] != %d)) do={ :error "Container memory limits changed since confirmation" }`, oldHigh, oldMax),
		fmt.Sprintf(`:if ((%d < %d) && (%d < ([/container/get $target memory-current] + 16777216))) do={ :error "Reduced hard limit is below current usage plus headroom" }`, maximum, oldMax, maximum),
		managedidentity.Literal(`:local watchdog [/system/scheduler/find where name="XROUTE-health-watchdog" and comment="XROUTE health scheduler" and disabled=no]; :if ([:len $watchdog] != 1) do={ :error "Active managed watchdog is required" }`),
		managedidentity.Literal(`:local gate [/ip/firewall/mangle/find where comment="XROUTE diversion-gate"]; :if ([:len $gate] != 1) do={ :error "Diversion gate is missing or ambiguous" }`),
		`/ip/firewall/mangle/disable $gate`,
		fmt.Sprintf(`/container/set $target memory-high=%d memory-max=%d`, high, maximum),
		fmt.Sprintf(`:if (([/container/get $target memory-high] != %d) || ([/container/get $target memory-max] != %d)) do={ :error "Container memory readback failed" }`, high, maximum),
		managedidentity.Literal(`:log warning "XROUTE: container memory limits changed; watchdog must confirm traffic readiness"`),
	}, "\n")
}
