package controlplane

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ati054/xroute/internal/runtimeconfig"
)

type draftOrderRuntime struct {
	fakeRuntimeApplier
	prepared chan map[string]any
}

func (runtime *draftOrderRuntime) prepare(config map[string]any, _ []map[string]any) (runtimeconfig.RuntimeCandidate, error) {
	runtime.prepared <- cloneJSONObject(config)
	return runtimeconfig.RuntimeCandidate{}, errors.New("stop before runtime activation")
}

func TestApplyReadsImplicitDraftAfterConcurrentSave(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "implicit"
		if explicit {
			name = "explicit"
		}
		t.Run(name, func(t *testing.T) {
			server := newTestServer(t)
			cookie, csrf := bootstrapSession(t, server)
			old := routerOSReadyConfig(t)
			old["system"].(map[string]any)["name"] = "old-save"
			if _, err := server.repository.saveDraft(old); err != nil {
				t.Fatal(err)
			}
			probe := &draftOrderRuntime{prepared: make(chan map[string]any, 1)}
			server.runtime = probe
			body := map[string]any{}
			if explicit {
				body["config"] = cloneJSONObject(old)
			}

			// A writer owns configMu while persisting a new draft. Apply must
			// take its implicit snapshot after that writer finishes, even if
			// the Apply request arrived before the save reached storage.
			server.configMu.Lock()
			locked := true
			done := make(chan *httptest.ResponseRecorder, 1)
			t.Cleanup(func() {
				if locked {
					server.configMu.Unlock()
				}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("Apply did not finish after releasing the writer")
				}
			})
			go func() {
				done <- performRequest(t, server, http.MethodPost, apiPrefix+"/drafts/apply", body, map[string]string{csrfHeader: csrf}, cookie)
			}()
			deadline := time.After(5 * time.Second)
			for server.mutationMu.TryLock() {
				server.mutationMu.Unlock()
				select {
				case <-deadline:
					t.Fatal("Apply did not enter the serialized mutation")
				case <-time.After(time.Millisecond):
				}
			}
			newest := cloneJSONObject(old)
			newest["system"].(map[string]any)["name"] = "new-save"
			if _, err := server.repository.saveDraft(newest); err != nil {
				t.Fatal(err)
			}
			server.configMu.Unlock()
			locked = false

			select {
			case prepared := <-probe.prepared:
				want := "new-save"
				if explicit {
					want = "old-save"
				}
				if got := prepared["system"].(map[string]any)["name"]; got != want {
					t.Fatalf("runtime received draft %q, want %q", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Apply did not prepare the draft")
			}
		})
	}
}
