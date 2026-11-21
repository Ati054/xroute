package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Ati054/xroute/internal/runtimeconfig"
)

const scopedPreviousPool = `{"health_policies":{"route":{"mode":"priority","candidates":["old"]},"remote":{"mode":"best","candidates":["remote-old"],"nodes":{"remote-old":{"fingerprint":"original","label":"Original label"}}}},"local_policy_ids":["route"]}`

func decodeHotScopePool(t *testing.T, body string) hotPolicyPool {
	t.Helper()
	var pool hotPolicyPool
	if err := json.Unmarshal([]byte(body), &pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestHotPolicyScopeSeparatesAvailabilityFromObservation(t *testing.T) {
	for _, scenario := range []struct {
		name, target       string
		required, observed []string
	}{
		{"unchanged", scopedPreviousPool, []string{"route"}, nil},
		{"renamed-remote", strings.ReplaceAll(scopedPreviousPool, "Original label", "New label"), []string{"route"}, nil},
		{"monitor-only", strings.ReplaceAll(scopedPreviousPool, `"mode":"best"`, `"mode":"best","policy":{"failure_threshold":7}`), []string{"route"}, nil},
		{"remote-endpoint", strings.ReplaceAll(scopedPreviousPool, `"fingerprint":"original"`, `"fingerprint":"changed"`), []string{"route"}, []string{"remote"}},
		{"remote-membership", strings.ReplaceAll(scopedPreviousPool, "remote-old", "remote-new"), []string{"route"}, []string{"remote"}},
		{"remote-mode", strings.ReplaceAll(scopedPreviousPool, `"mode":"best"`, `"mode":"priority"`), []string{"route"}, []string{"remote"}},
		{"remote-service-access", strings.ReplaceAll(scopedPreviousPool, `"mode":"best"`, `"mode":"best","policy":{"candidate_service_ids":["service"],"candidate_service_access":{"group":["service"]}}`), []string{"route"}, []string{"remote"}},
		{"remote-prefix", strings.ReplaceAll(scopedPreviousPool, `"local_policy_ids"`, `"policy_prefixes":{"remote":"new-prefix"},"local_policy_ids"`), []string{"route"}, []string{"remote"}},
		{"remote-becomes-local", strings.ReplaceAll(scopedPreviousPool, `"local_policy_ids":["route"]`, `"local_policy_ids":["route","remote"]`), []string{"remote", "route"}, nil},
		{"remote-change-without-local-clients", strings.ReplaceAll(strings.ReplaceAll(scopedPreviousPool, "remote-old", "remote-new"), `"local_policy_ids":["route"]`, `"local_policy_ids":[]`), nil, []string{"remote"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			scope := hotPolicyReadiness(decodeHotScopePool(t, scopedPreviousPool), decodeHotScopePool(t, scenario.target))
			if !reflect.DeepEqual(scope.RequiredPolicies, scenario.required) || !reflect.DeepEqual(scope.ObservedPolicies, scenario.observed) {
				t.Fatalf("scope=%+v, want required %v and observed %v", scope, scenario.required, scenario.observed)
			}
		})
	}
}

func TestHotPolicyActivationScopesBlockedSelectors(t *testing.T) {
	for _, method := range []string{"apply", "subscription"} {
		for _, scenario := range []struct {
			name, target string
			selections   map[string]string
			blocked      bool
		}{
			{"unrelated-remote", strings.ReplaceAll(scopedPreviousPool, `"old"`, `"new"`), map[string]string{"route": "new", "remote": "block"}, false},
			{"changed-remote", strings.ReplaceAll(scopedPreviousPool, "remote-old", "remote-new"), map[string]string{"route": "old", "remote": "block"}, false},
			{"new-unassigned-reverse", strings.Replace(scopedPreviousPool, `"health_policies":{`, `"health_policies":{"reverse":{"mode":"priority","candidates":["reverse-exit"]},`, 1), map[string]string{"route": "old", "reverse": "block"}, false},
			{"changed-local", strings.ReplaceAll(scopedPreviousPool, `"old"`, `"new"`), map[string]string{"route": "block", "remote": "remote-old"}, true},
			{"unchanged-local", strings.ReplaceAll(scopedPreviousPool, "remote-old", "remote-new"), map[string]string{"route": "block", "remote": "remote-new"}, true},
			{"missing-changed-remote", strings.ReplaceAll(scopedPreviousPool, "remote-old", "remote-new"), map[string]string{"route": "old"}, true},
		} {
			t.Run(method+"/"+scenario.name, func(t *testing.T) {
				runtime, candidate, controller := hotPolicyFixtureWithPools(t, scopedPreviousPool, scenario.target)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				done := make(chan error, 1)
				previousStamp := testPoolStamp(t, runtime.options.XrayHealthPool)
				go func() {
					var err error
					if method == "subscription" {
						_, err = runtime.activateSubscription(ctx, candidate)
					} else {
						_, err = runtime.activate(ctx, candidate)
					}
					done <- err
				}()
				waitTestPool(t, runtime.options.XrayHealthPool, scenario.target, previousStamp, done)
				acknowledgeTestPool(t, runtime, scenario.selections)
				if scenario.blocked {
					// More than a poll interval: a matching generation marker alone
					// must not release a required blocked or missing selector.
					select {
					case err := <-done:
						t.Fatalf("required unavailable selector acknowledged: %v", err)
					case <-time.After(300 * time.Millisecond):
					}
					acknowledgeTestPool(t, runtime)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if len(controller.restarts) != 0 {
					t.Fatalf("scoped hot acknowledgement restarted Xray: %v", controller.restarts)
				}
			})
		}
	}
}

func TestHotPolicyLocalBlockTimesOutAndRollsBack(t *testing.T) {
	target := strings.ReplaceAll(scopedPreviousPool, `"old"`, `"new"`)
	runtime, candidate, _ := hotPolicyFixtureWithPools(t, scopedPreviousPool, target)
	type result struct {
		receipt runtimeconfig.ActivationReceipt
		err     error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	done := make(chan result, 1)
	previousStamp := testPoolStamp(t, runtime.options.XrayHealthPool)
	go func() {
		receipt, err := runtime.activate(ctx, candidate)
		done <- result{receipt, err}
	}()
	waitTestPool(t, runtime.options.XrayHealthPool, target, previousStamp)
	acknowledgeTestPool(t, runtime, map[string]string{"route": "block", "remote": "block"})
	failed := <-done
	if !errors.Is(failed.err, context.DeadlineExceeded) || !hotPolicyChange(failed.receipt.Changed()) {
		t.Fatalf("blocked local policy did not return rollback receipt: %v %v", failed.err, failed.receipt.Changed())
	}
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer rollbackCancel()
	rollback := make(chan error, 1)
	previousStamp = testPoolStamp(t, runtime.options.XrayHealthPool)
	go func() { rollback <- runtime.rollbackSubscription(rollbackCtx, failed.receipt) }()
	waitTestPool(t, runtime.options.XrayHealthPool, scopedPreviousPool, previousStamp, rollback)
	acknowledgeTestPool(t, runtime, map[string]string{"route": "block", "remote": "block"})
	select {
	case err := <-rollback:
		t.Fatalf("rollback accepted a blocked local policy: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	acknowledgeTestPool(t, runtime, map[string]string{"route": "old", "remote": "block"})
	if err := <-rollback; err != nil {
		t.Fatal(err)
	}
}

func TestHotPolicyScopeCannotAcknowledgeAnotherPublication(t *testing.T) {
	runtime, candidate, _ := hotPolicyFixture(t)
	scope, err := hotPolicyScope(runtime.options.XrayHealthPool, candidate.Files["urltest-pool.json"])
	if err != nil {
		t.Fatal(err)
	}
	// This scope belongs to the unpublished target, not the currently live pool.
	acknowledgeTestPool(t, runtime)
	if err := runtime.waitHotRuntime(context.Background(), runtime.options.XrayHealthPool, scope); err == nil {
		t.Fatal("another publication was acknowledged using the target scope")
	}
	if err := os.WriteFile(runtime.options.XrayHealthPool, []byte("invalid-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := hotPolicyScope(runtime.options.XrayHealthPool, candidate.Files["urltest-pool.json"]); err == nil {
		t.Fatal("invalid previous pool silently omitted affected policies")
	}
}
