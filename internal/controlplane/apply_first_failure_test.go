package controlplane

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ati054/xroute/internal/routeros"
	"github.com/Ati054/xroute/internal/runtimeconfig"
)

type firstApplyFailureRuntime struct {
	fakeRuntimeApplier
	prepared     []map[string]any
	failRecovery bool
}

func (runtime *firstApplyFailureRuntime) prepare(config map[string]any, nodes []map[string]any) (runtimeconfig.RuntimeCandidate, error) {
	runtime.prepared = append(runtime.prepared, cloneJSONObject(config))
	return runtime.fakeRuntimeApplier.prepare(config, nodes)
}

func (runtime *firstApplyFailureRuntime) activate(ctx context.Context, candidate runtimeconfig.RuntimeCandidate) (runtimeconfig.ActivationReceipt, error) {
	if runtime.failRecovery && runtime.activateCalls == 1 {
		return runtimeconfig.ActivationReceipt{}, errors.New("baseline runtime unavailable")
	}
	return runtime.fakeRuntimeApplier.activate(ctx, candidate)
}

func TestFirstApplyFailureRestoresRunnableBaselineWithoutPublishingActive(t *testing.T) {
	for _, failRecovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "recovery_pending"}[failRecovery], func(t *testing.T) {
			server := newTestServer(t)
			root := t.TempDir()
			store, err := runtimeconfig.NewCandidateStore(filepath.Join(root, "state"), map[string]string{
				"xray.json": filepath.Join(root, "generated", "xray.json"),
			})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := store.Prepare(strings.Repeat("b", 64), map[string][]byte{"xray.json": []byte("{}")})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := store.Activate(candidate)
			if err != nil {
				t.Fatal(err)
			}
			runtime := &firstApplyFailureRuntime{
				fakeRuntimeApplier: fakeRuntimeApplier{revision: candidate.Revision, activationReceipt: receipt},
				failRecovery:       failRecovery,
			}
			server.runtime = runtime
			server.applyRouterOS = func(ctx context.Context, _ map[string]any, _, _ string, health func(context.Context) error, _ func(context.Context, routeros.BackupRef) error) (routerOSApplyOutput, error) {
				if err := health(ctx); err != nil {
					return routerOSApplyOutput{}, err
				}
				return routerOSApplyOutput{}, &routeros.TransactionFailure{State: routeros.TransactionRolledBack, Err: errors.New("post-apply verification failed")}
			}
			_, status, applyErr := server.applyConfiguration(context.Background(), routerOSReadyConfig(t), "test")
			if status != http.StatusConflict || applyErr == nil {
				t.Fatalf("status=%d err=%v", status, applyErr)
			}
			if runtime.rollbackCalls != 0 || len(runtime.prepared) != 2 {
				t.Fatalf("runtime calls: %#v", runtime)
			}
			for _, name := range []string{"local_clients", "policies", "remote_users", "transports"} {
				if len(objects(runtime.prepared[1][name])) != 0 {
					t.Fatalf("baseline retained %s", name)
				}
			}
			if active, err := server.repository.activeRevision(); err != nil || active != "" {
				t.Fatalf("failed first Apply published active=%q err=%v", active, err)
			}
			operation, err := server.repository.auxiliary("apply-operation")
			if err != nil {
				t.Fatal(err)
			}
			if failRecovery {
				if operation["pending"] != true || runtime.commitCalls != 0 {
					t.Fatalf("unconfirmed recovery accepted: %#v", operation)
				}
			} else if len(operation) != 0 || runtime.commitCalls != 1 || runtime.activateCalls != 2 {
				t.Fatalf("baseline restoration did not finish: operation=%#v runtime=%#v", operation, runtime)
			}
		})
	}
}
