package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/Ati054/xroute/internal/managedidentity"
)

// The caller owns mutationMu, including while arming the RouterOS scheduler.
func (server *Server) reconcilePendingContainerMemory(parent context.Context) (bool, error) {
	operation, err := server.repository.auxiliary(containerMemoryOperation)
	if err != nil || operation["pending"] != true {
		return false, err
	}
	config, err := server.repository.loadActive()
	if err != nil {
		return true, err
	}
	client, _, err := server.newRouterOSRESTClient(config)
	if err != nil {
		return true, nil
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	queued, err := client.ContainerMemoryUpdatePending(ctx)
	if err != nil {
		// An unreachable router may still have an armed worker.
		return true, nil
	}
	var container map[string]any
	if !queued {
		if rows, err := client.List(ctx, routerOSContainerInventoryPath); err == nil {
			if row := ownedMemoryContainer(rows); row != nil {
				container = containerSummary(row, nil, true)
			}
		}
	}
	if err := server.reconcileContainerMemoryReadback(operation, container, queued); err != nil {
		return true, err
	}
	return operation["pending"] == true, nil
}

func (server *Server) runContainerMemoryReconciliation(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if server.mutationMu.TryLock() {
			pending, err := server.reconcilePendingContainerMemory(ctx)
			server.mutationMu.Unlock()
			if err == nil && !pending {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func ownedMemoryContainer(rows []map[string]any) map[string]any {
	var current map[string]any
	for _, row := range rows {
		if text(row["comment"]) != managedidentity.Literal("XROUTE container") {
			continue
		}
		if current != nil {
			return nil
		}
		current = row
	}
	return current
}

func (server *Server) reconcileContainerMemoryReadback(operation, container map[string]any, queued bool) error {
	previousState, previousPending := operation["state"], operation["pending"]
	high, highOK := routerOSByteSize(container["memory_high"])
	maximum, maxOK := routerOSByteSize(container["memory_max"])
	wantHigh, _ := numberToInt64(operation["high_bytes"])
	wantMax, _ := numberToInt64(operation["max_bytes"])
	root := strings.Trim(text(operation["root_dir"]), "/")
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, text(operation["deadline"]))
	if !queued && highOK && maxOK && high == wantHigh && maximum == wantMax &&
		root != "" && strings.Trim(text(container["root_dir"]), "/") == root && container["healthy"] == true {
		if err := server.rememberContainerMemoryLimits(high, maximum); err != nil {
			return err
		}
		operation["pending"], operation["state"] = false, "completed"
	} else if deadlineErr == nil && !server.now().Before(deadline) {
		operation["pending"], operation["state"] = queued, "unconfirmed"
	}
	if previousState == operation["state"] && previousPending == operation["pending"] {
		return nil
	}
	return server.repository.saveAuxiliary(containerMemoryOperation, operation)
}
