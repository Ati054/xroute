package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/Ati054/xroute/internal/imagebundle"
	"github.com/Ati054/xroute/internal/managedidentity"
	"github.com/Ati054/xroute/internal/routeros"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("routeros-recovery", flag.ContinueOnError)
	var archive, checksum, version, root, address, identity, destination, architecture string
	flags.StringVar(&archive, "archive", "", "verified local Docker archive")
	flags.StringVar(&architecture, "arch", "arm64", "image architecture: arm64 or amd64")
	flags.StringVar(&checksum, "sha256", "", "expected archive SHA256 from trusted release metadata")
	flags.StringVar(&version, "version", "", "expected image version")
	flags.StringVar(&root, "storage-root", "", "existing RouterOS external project root")
	flags.StringVar(&address, "container-ip", "", "existing container veth IPv4 address")
	flags.StringVar(&identity, "router-identity", "", "exact RouterOS identity")
	flags.StringVar(&destination, "output", "", "new offline recovery .rsc file; never overwritten")
	if err := flags.Parse(args); err != nil {
		return err
	}
	checksum = strings.ToLower(checksum)
	if architecture != "arm64" && architecture != "amd64" {
		return errors.New("--arch must be arm64 or amd64")
	}
	if flags.NArg() != 0 || archive == "" || destination == "" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(checksum) {
		return errors.New("archive, trusted sha256 and output are required; no positional arguments")
	}
	info, err := os.Stat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 2<<30 {
		return errors.New("recovery archive must be a regular local file")
	}
	actual, err := imagebundle.SHA256File(archive)
	if err != nil {
		return err
	}
	if actual != checksum {
		return errors.New("recovery archive SHA256 mismatch")
	}
	details, err := imagebundle.ValidatePlatform(archive, "xroute:"+version+"-"+architecture, version, architecture)
	if err != nil {
		// Existing releases retain the legacy repository tag inside the TAR.
		details, err = imagebundle.ValidatePlatform(archive, "sb-gateway:"+version+"-"+architecture, version, architecture)
	}
	if err != nil {
		return fmt.Errorf("validate recovery archive: %w", err)
	}
	root = strings.Trim(root, "/")
	reference := root + "/data/lifecycle-uploads/" + managedidentity.Literal("xroute-upload-") + checksum[:16] + ".tar"
	script, err := routeros.RenderOfflineImageRecovery(routeros.ImageUpdateSpec{
		Architecture:  architecture,
		CatalogSchema: details.SubscriptionCatalogSchema,
		Version:       version, StorageRoot: root, CandidateRoot: root + "/root-" + version,
		CandidateSource: "local-file", CandidateReference: reference, ContainerAddress: address,
	}, info.Size(), identity)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, script)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.Join(writeErr, closeErr)
	}
	fmt.Fprintf(output, "ARCHIVE_VERIFIED=PASS\nROUTEROS_NAMESPACE=%s\nROUTEROS_ARCHIVE=%s\nRECOVERY_SCRIPT=%s\n", managedidentity.Prefix(), reference, destination)
	return nil
}
