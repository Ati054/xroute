package controlplane

import "testing"

func TestFreshConfigurationHasNoPublicListeners(t *testing.T) {
	server := newTestServer(t)
	config, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	transports := objectNodes(config["transports"])
	if len(transports) == 0 {
		t.Fatal("missing transport templates")
	}
	for _, transport := range transports {
		if transport["enabled"] != false {
			t.Fatalf("public transport enabled before setup: %v", transport["id"])
		}
	}
	// Changing defaults must not disable a listener saved by an existing user.
	transports[0]["enabled"] = true
	if _, err := server.repository.saveDraft(config); err != nil {
		t.Fatal(err)
	}
	restored, err := server.getDraft()
	if err != nil || objectNodes(restored["transports"])[0]["enabled"] != true {
		t.Fatalf("existing listener setting was lost: %v", err)
	}
}

func TestOverviewListenersFollowCommittedConfiguration(t *testing.T) {
	server := newTestServer(t)
	config, err := server.getDraft()
	if err != nil {
		t.Fatal(err)
	}
	config["transports"] = []any{map[string]any{
		"id": "listener", "kind": "ws", "enabled": true, "listen_port": 443,
		"hostname": "gateway.example.test", "hostname_mode": "manual",
		"private_key_secret_ref": "must-not-be-exposed", "path": "/private-path",
	}}
	initial, err := server.overviewPayload(config)
	if err != nil || len(initial["applied_transports"].([]any)) != 0 {
		t.Fatalf("unapplied listener claimed active: %v", err)
	}
	revision, err := server.repository.stageGeneration(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.repository.setActiveRevision(revision); err != nil {
		t.Fatal(err)
	}
	objectNodes(config["transports"])[0]["enabled"] = false
	payload, err := server.overviewPayload(config)
	if err != nil {
		t.Fatal(err)
	}
	listeners := objectNodes(payload["applied_transports"])
	if len(listeners) != 1 || listeners[0]["enabled"] != true || listeners[0]["listen_port"] == nil {
		t.Fatalf("draft edit replaced committed listener: %#v", listeners)
	}
	if len(listeners[0]) != 6 {
		t.Fatalf("listener summary contains fields outside the public allowlist: %#v", listeners[0])
	}
}
