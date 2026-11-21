package controlplane

import (
	"bytes"
	"maps"
	"reflect"
	"slices"
	"strings"
)

const pendingChangePreviewLimit = 8

var pendingCollectionNames = slices.Collect(maps.Values(apiCollections))

type pendingConfigChange struct {
	Collection  string `json:"collection"`
	Op          string `json:"op"`
	Path        string `json:"path"`
	EntityID    string `json:"entity_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type pendingConfigSummary struct {
	count   int
	changes []pendingConfigChange
}

func (summary *pendingConfigSummary) add(change pendingConfigChange) {
	summary.count++
	if len(summary.changes) < pendingChangePreviewLimit {
		summary.changes = append(summary.changes, change)
	}
}

// Both panel endpoints display the same saved change summary. Cache a single
// revision pair so polling does not repeatedly compare large collections.
func (server *Server) pendingConfiguration(activeRevision, draftRevision string, active, draft map[string]any) (pendingConfigSummary, error) {
	if activeRevision == draftRevision {
		return pendingConfigSummary{changes: []pendingConfigChange{}}, nil
	}
	if activeRevision == "" {
		return pendingConfigSummary{count: 1, changes: []pendingConfigChange{{Collection: "configuration", Op: "replace", Path: "/"}}}, nil
	}
	server.pendingConfigMu.Lock()
	defer server.pendingConfigMu.Unlock()
	if server.pendingActiveRevision == activeRevision && server.pendingDraftRevision == draftRevision {
		return server.pendingConfig, nil
	}
	if active == nil {
		var err error
		active, err = server.repository.loadGeneration(activeRevision)
		if err != nil {
			return pendingConfigSummary{}, err
		}
	}
	summary := summarizePendingConfiguration(active, draft)
	server.pendingActiveRevision = activeRevision
	server.pendingDraftRevision = draftRevision
	server.pendingConfig = summary
	return summary, nil
}

func sortedConfigKeys[V any](before, after map[string]V) []string {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	return slices.Sorted(maps.Keys(keys))
}

func summarizePendingConfiguration(active, draft map[string]any) pendingConfigSummary {
	summary := pendingConfigSummary{changes: make([]pendingConfigChange, 0, pendingChangePreviewLimit)}
	for _, key := range sortedConfigKeys(active, draft) {
		if reflect.DeepEqual(active[key], draft[key]) {
			continue
		}
		if slices.Contains(pendingCollectionNames, key) {
			before, beforeOK := draftEntities(active[key])
			after, afterOK := draftEntities(draft[key])
			if beforeOK && afterOK && addPendingEntities(&summary, key, before, after) {
				continue
			}
		}
		if equalConfigValues(active[key], draft[key]) {
			continue
		}
		summary.add(pendingConfigChange{Collection: key, Op: "replace", Path: "/" + key})
	}
	if summary.count == 0 {
		// This also covers older persisted data whose revision changed without
		// a visible entity or section changing in its decoded representation.
		summary.add(pendingConfigChange{Collection: "configuration", Op: "replace", Path: "/"})
	}
	return summary
}

func equalConfigValues(left, right any) bool {
	if reflect.DeepEqual(left, right) {
		return true
	}
	leftJSON, leftErr := canonicalJSON(left)
	rightJSON, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func draftEntities(value any) (map[string]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok && value != nil {
		return nil, false
	}
	entities := make(map[string]map[string]any, len(items))
	for _, entry := range items {
		item, ok := entry.(map[string]any)
		id, _ := item["id"].(string)
		if !ok || id == "" || entities[id] != nil {
			return nil, false
		}
		entities[id] = item
	}
	return entities, true
}

func addPendingEntities(summary *pendingConfigSummary, collection string, before, after map[string]map[string]any) bool {
	initialCount := summary.count
	for _, id := range sortedConfigKeys(before, after) {
		oldItem, newItem := before[id], after[id]
		if equalConfigValues(oldItem, newItem) {
			continue
		}
		op, item := "replace", newItem
		if oldItem == nil {
			op = "add"
		} else if newItem == nil {
			op, item = "remove", oldItem
		}
		var change pendingConfigChange
		if len(summary.changes) < pendingChangePreviewLimit {
			change = pendingConfigChange{
				Collection: collection, Op: op, Path: "/" + collection + "/" + escapeConfigPath(id), EntityID: id,
				DisplayName: stringDefault(item["display_name"], stringDefault(item["name"], "")),
			}
		}
		summary.add(change)
	}
	return summary.count != initialCount
}

func escapeConfigPath(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
