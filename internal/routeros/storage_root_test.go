package routeros

import (
	"strings"
	"testing"
)

func TestStorageRootsKeepDedicatedDirectoryAndFlashBoundaries(t *testing.T) {
	for _, entry := range []struct {
		root          string
		system, valid bool
	}{
		{"xroute", true, true}, {"/xroute/", true, true}, {"xroute", false, false},
		{"slot1/xroute", false, true}, {"disk1/xroute", false, true}, {"usb1/xroute", false, true},
		{"disk1", true, false}, {"slot1", true, false}, {"flash/xroute", true, false},
		{"xroute/../other", true, false}, {"xroute//other", true, false}, {"/", true, false},
		{"skins", true, false}, {"xroute; /file/remove", true, false},
	} {
		_, err := ValidateStorageRoot(entry.root, entry.system)
		if (err == nil) != entry.valid {
			t.Errorf("root=%q system=%t err=%v", entry.root, entry.system, err)
		}
	}
}

func TestSystemDiskWorkersCheckCHRBeforeMutation(t *testing.T) {
	for _, root := range []string{"xroute", "slot1/xroute"} {
		worker := renderFullUninstallWorker(root)
		guard, mutation := strings.Index(worker, "board-name"), strings.Index(worker, "/system/scheduler/remove")
		if root == "xroute" && (guard < 0 || mutation < guard) {
			t.Fatal("system-disk worker mutates before checking CHR")
		}
		if root != "xroute" && guard >= 0 {
			t.Fatal("external disk unexpectedly requires CHR")
		}
	}
}

func TestImageWorkersCheckArchitectureBeforeStoppingCurrent(t *testing.T) {
	for _, architecture := range []string{"arm64", "amd64"} {
		spec, err := validateImageUpdateSpec(ImageUpdateSpec{Architecture: architecture, Version: "1.7.0", StorageRoot: "disk1/xroute", CandidateRoot: "disk1/xroute/root-1.7.0", CandidateSource: "local-file", CandidateReference: "disk1/xroute/data/lifecycle-uploads/image.tar", ContainerAddress: "192.0.2.2"})
		if err != nil {
			t.Fatal(err)
		}
		worker := renderImageUpdateWorker(spec)
		guard, stop := strings.Index(worker, "image does not match this router architecture"), strings.Index(worker, "/container/stop")
		if guard < 0 || stop < guard {
			t.Fatal("worker stops current container before validating architecture")
		}
	}
}
