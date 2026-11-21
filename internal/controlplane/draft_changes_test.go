package controlplane

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestOverviewAndDraftReportSavedConfigurationChanges(t *testing.T) {
	server := newTestServer(t)
	active := routerOSReadyConfig(t)
	active["local_clients"] = []any{
		map[string]any{"id": "laptop", "name": "Laptop", "enabled": true},
		map[string]any{"id": "old", "name": "Old device"},
	}
	revision, err := server.repository.stageGeneration(active)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.repository.commitActive(commitMetadata{Revision: revision, Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	draft := cloneJSONObject(active)
	draft["local_clients"] = []any{
		map[string]any{"id": "laptop", "name": "Laptop", "enabled": false},
		map[string]any{"id": "new", "name": "New device"},
	}
	objectAt(draft, "dns")["internal_server"] = "192.0.2.4"
	envelope, err := server.draftEnvelope(draft)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := server.overviewPayload(draft)
	if err != nil {
		t.Fatal(err)
	}
	want := []pendingConfigChange{
		{Collection: "dns", Op: "replace", Path: "/dns"},
		{Collection: "local_clients", Op: "replace", Path: "/local_clients/laptop", EntityID: "laptop", DisplayName: "Laptop"},
		{Collection: "local_clients", Op: "add", Path: "/local_clients/new", EntityID: "new", DisplayName: "New device"},
		{Collection: "local_clients", Op: "remove", Path: "/local_clients/old", EntityID: "old", DisplayName: "Old device"},
	}
	for _, payload := range []map[string]any{envelope, overview} {
		if payload["pending_change_count"] != len(want) || payload["pending_config_change_count"] != len(want) {
			t.Fatalf("unexpected counts: %v, %v", payload["pending_change_count"], payload["pending_config_change_count"])
		}
		if got := payload["pending_config_changes"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected pending changes: %#v", got)
		}
	}
	reset, err := server.draftEnvelope(active)
	if err != nil {
		t.Fatal(err)
	}
	if reset["pending_config_change_count"] != 0 || len(reset["pending_config_changes"].([]pendingConfigChange)) != 0 {
		t.Fatal("reverting to the active configuration did not clear the summary")
	}
}

func TestPendingChangePreviewIsBounded(t *testing.T) {
	clients := make([]any, 12)
	for i := range clients {
		clients[i] = map[string]any{"id": fmt.Sprintf("device-%02d", i)}
	}
	summary := summarizePendingConfiguration(
		map[string]any{"system": map[string]any{"port": json.Number("443")}},
		map[string]any{"system": map[string]any{"port": 443}, "local_clients": clients},
	)
	if summary.count != len(clients) || len(summary.changes) != pendingChangePreviewLimit {
		t.Fatalf("unexpected count or preview length: %#v", summary)
	}
}
