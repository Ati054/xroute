// Package managedidentity binds one appliance process to its RouterOS-owned
// object namespace, independently of its installation directory.
package managedidentity

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const Environment = "SB_ROUTEROS_NAMESPACE"
const CurrentPrefix = "XROUTE"
const LegacyPrefix = "SB-GATEWAY"

// Namespace is immutable. An absent environment entry retains legacy ownership.
// New installers explicitly bind XROUTE; updates preserve this environment.
type Namespace struct{ prefix string }

func Parse(value string) (Namespace, error) {
	switch value {
	case "", LegacyPrefix:
		return Namespace{LegacyPrefix}, nil
	case CurrentPrefix:
		return Namespace{CurrentPrefix}, nil
	default:
		return Namespace{}, errors.New("unsupported managed RouterOS namespace")
	}
}

var current = func() Namespace {
	namespace, err := Parse(os.Getenv(Environment))
	if err != nil {
		panic(err)
	}
	return namespace
}()

func Prefix() string { return current.prefix }

// Literal resolves a maintained literal BEFORE interpolation of configuration,
// credentials, addresses or paths. Never pass user-supplied or rendered text.
func Literal(canonical string) string { return current.Source(canonical) }

// Script resolves an embedded, maintained script before any user values enter it.
func Script(canonical string) string { return current.Source(canonical) }

var ownershipSlice = regexp.MustCompile(`0 ([0-9]+)(\]\s*(?:=|!=)\s*"(XROUTE[^"\r\n]*)")`)

var legacyNames = strings.NewReplacer(
	"XROUTE", LegacyPrefix,
	"xroute-env", "sb-gateway-env",
	"xroute-config", "sb-gateway-config",
	"xroute-data", "sb-gateway-data",
	"xroute-logs", "sb-gateway-logs",
	"xroute-state", "sb-gateway-state",
	"xroute-api", "sb-gateway-api",
	"xroute-forward", "sb-gateway-forward",
	"xroute-input", "sb-gateway-input",
	"xroute-protocol-", "sb-gateway-protocol-",
	"xroute-public-guard", "sb-gateway-public-guard",
	"xroute-divert", "sb-gateway-divert",
	"xroute-upload-", "sb-gateway-upload-",
	"xroute-before-webfig-bootstrap", "sb-gateway-before-webfig-bootstrap",
	"xroute-manual-backup", "sb-gateway-manual-backup",
	"xroute-manual-export", "sb-gateway-manual-export",
	"xroute-managed", "sb-managed",
	"bridge-xroute", "bridge-sb",
	"veth-xroute", "veth-sb",
	"to-xroute", "to-sb-gateway",
)

// Source resolves a maintained script/template, not a rendered document. Prefix
// guards change their byte lengths with the prefix. Stable configuration keys,
// executable paths and release filenames are not ownership labels.
func (namespace Namespace) Source(canonical string) string {
	const begin = "# XRoute namespace compatibility guard: begin"
	const end = "# XRoute namespace compatibility guard: end"
	if before, rest, ok := strings.Cut(canonical, begin); ok {
		guard, after, found := strings.Cut(rest, end)
		if !found || strings.Contains(after, begin) {
			panic("invalid maintained namespace guard")
		}
		return namespace.Source(before) + begin + guard + end + namespace.Source(after)
	}
	if namespace.prefix == CurrentPrefix {
		return canonical
	}
	if namespace.prefix != LegacyPrefix {
		panic("uninitialized managed RouterOS namespace")
	}
	if strings.Contains(canonical, "[:pick") {
		canonical = ownershipSlice.ReplaceAllStringFunc(canonical, func(value string) string {
			parts := ownershipSlice.FindStringSubmatch(value)
			length, _ := strconv.Atoi(parts[1])
			if length != len(parts[3]) {
				panic("managed ownership prefix length mismatch")
			}
			return "0 " + strconv.Itoa(len(legacyNames.Replace(parts[3]))) + parts[2]
		})
	}
	return legacyNames.Replace(canonical)
}
