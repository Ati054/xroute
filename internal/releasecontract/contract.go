// Package releasecontract defines the compatibility boundary shared by the
// release builder and the in-place RouterOS updater.
package releasecontract

import (
	"strconv"

	"github.com/Ati054/xroute/internal/managedidentity"
)

const (
	ImageTitle               = "sb-gateway"
	LifecycleVersionLabel    = "io.sb-gateway.lifecycle.version"
	LifecycleVersion         = "1"
	ConfigSchemaLabel        = "io.sb-gateway.config.schema"
	MinimumConfigSchemaLabel = "io.sb-gateway.config.minimum-schema"
	ConfigSchemaVersion      = 1
	RouterOSNamespaceLabel   = "io.xroute.routeros.namespace.version"
	RouterOSNamespaceVersion = "1"
)

// Compatible reports whether an image can consume state written with
// currentSchema without an intermediate image update.
func Compatible(labels map[string]string, currentSchema int) (schema, minimumSchema int, ok bool) {
	if labels["org.opencontainers.image.title"] != ImageTitle || labels[LifecycleVersionLabel] != LifecycleVersion {
		return 0, 0, false
	}
	if !namespaceCompatible(labels, managedidentity.Prefix()) {
		return 0, 0, false
	}
	schema, schemaErr := strconv.Atoi(labels[ConfigSchemaLabel])
	minimumSchema, minimumErr := strconv.Atoi(labels[MinimumConfigSchemaLabel])
	ok = schemaErr == nil && minimumErr == nil && minimumSchema >= 1 && minimumSchema <= currentSchema && currentSchema <= schema
	return schema, minimumSchema, ok
}

// A new installation must not downgrade to a core that only recognizes legacy
// RouterOS objects. Older installations retain their original update contract.
func namespaceCompatible(labels map[string]string, namespace string) bool {
	return namespace == managedidentity.LegacyPrefix ||
		(namespace == managedidentity.CurrentPrefix && labels[RouterOSNamespaceLabel] == RouterOSNamespaceVersion)
}
