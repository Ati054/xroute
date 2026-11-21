package routeros

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/Ati054/xroute/internal/managedidentity"
)

const managedRouterOSBackupRetention = 3

var (
	managedBackupFilePattern = regexp.MustCompile(managedidentity.Literal(`^(XROUTE-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12})(?:-export\.rsc|\.backup)$`))
	managedImageArchiveName  = regexp.MustCompile(`^(?:sb-gateway|xroute)-[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?-linux-(?:arm64|amd64)\.tar(?:\.sha256)?$`)
)

var managedBootstrapArtifacts = map[string]bool{
	"setup.rsc":             true,
	"bootstrap.rsc":         true,
	"fasttrack-patch.rsc":   true,
	"install.rsc":           true,
	"container-startup.rsc": true,
	"preflight.rsc":         true,
	"variables.rsc":         true,
	"watchdog.rsc":          true,
	"webfig-bootstrap.rsc":  true,
}

type ManagedFilePruneResult struct {
	Backups int
	Install int
}

// PruneManagedFiles removes only exact XRoute-owned files. The newest
// three RouterOS backup/export generations are retained, while one-shot
// bootstrap scripts and extracted local image archives are removed from the
// confirmed project directory. A system-disk directory requires x86 CHR.
// Unknown files and nested runtime data are never selected.
func (client *Client) PruneManagedFiles(ctx context.Context, storageRoot string) (ManagedFilePruneResult, error) {
	root, err := ValidateStorageRoot(storageRoot, true)
	if err != nil {
		return ManagedFilePruneResult{}, err
	}
	storageRoot = root
	if !strings.Contains(root, "/") {
		resources, err := client.listWithHTTP(ctx, client.actionHTTP, "/rest/system/resource?.proplist=architecture-name,board-name")
		if err != nil {
			return ManagedFilePruneResult{}, err
		}
		if len(resources) != 1 || !strings.HasPrefix(text(resources[0]["board-name"]), "CHR") ||
			(text(resources[0]["architecture-name"]) != "x86" && text(resources[0]["architecture-name"]) != "x86_64") {
			return ManagedFilePruneResult{}, errors.New("system-disk file retention requires x86 CHR")
		}
	}
	// A populated disk inventory can exceed the short status-request timeout.
	// Keep the caller's maintenance deadline and request only the fields used
	// below; inspecting file types is unnecessary for exact-name ownership.
	rows, err := client.listWithHTTP(ctx, client.actionHTTP, "/rest/file?.proplist=.id,name")
	if err != nil {
		return ManagedFilePruneResult{}, err
	}

	backupFiles := make(map[string][]map[string]any)
	installFiles := []map[string]any{}
	prefix := storageRoot + "/"
	for _, row := range rows {
		name := text(row["name"])
		if matches := managedBackupFilePattern.FindStringSubmatch(name); len(matches) == 2 {
			backupFiles[matches[1]] = append(backupFiles[matches[1]], row)
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		relative := strings.TrimPrefix(name, prefix)
		if strings.Contains(relative, "/") {
			continue
		}
		if managedBootstrapArtifacts[relative] || managedImageArchiveName.MatchString(relative) {
			installFiles = append(installFiles, row)
		}
	}

	bases := make([]string, 0, len(backupFiles))
	for base := range backupFiles {
		bases = append(bases, base)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(bases)))
	result := ManagedFilePruneResult{}
	for _, base := range bases[min(len(bases), managedRouterOSBackupRetention):] {
		for _, row := range backupFiles[base] {
			if err := client.removeManagedFile(ctx, row); err != nil {
				return result, err
			}
			result.Backups++
		}
	}
	for _, row := range installFiles {
		if err := client.removeManagedFile(ctx, row); err != nil {
			return result, err
		}
		result.Install++
	}
	return result, nil
}

func (client *Client) removeManagedFile(ctx context.Context, row map[string]any) error {
	id := text(row[".id"])
	if id == "" {
		return errors.New("managed RouterOS file has no identifier")
	}
	_, err := client.request(ctx, http.MethodDelete, "/rest/file/"+routerOSResourceID(id), nil)
	return err
}
