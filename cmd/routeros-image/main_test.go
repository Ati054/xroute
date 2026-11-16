package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ati054/xroute/internal/managedidentity"
)

func TestInstallerBundleContainsMatchingCurrentAndLegacyScripts(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := managedidentity.Parse(managedidentity.LegacyPrefix)
	for _, architecture := range []string{"arm64", "amd64"} {
		t.Run(architecture, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "routeros")
			if err := writeRouterOSScripts(root, out, architecture); err != nil {
				t.Fatal(err)
			}
			files, err := filepath.Glob(filepath.Join(root, "routeros", "*.rsc"))
			if err != nil || len(files) < 10 {
				t.Fatal("maintained installers missing", err)
			}
			for _, source := range files {
				body, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				for subdir, expected := range map[string][]byte{
					"":       bundleScriptContents(filepath.Base(source), body, architecture),
					"legacy": bundleScriptContents(filepath.Base(source), []byte(legacy.Source(string(body))), architecture),
				} {
					actual, err := os.ReadFile(filepath.Join(out, subdir, filepath.Base(source)))
					if err != nil || string(actual) != string(expected) {
						t.Fatalf("%s/%s mismatch: %v", subdir, filepath.Base(source), err)
					}
				}
			}
		})
	}
}

func TestWindowsTempDirCleanupOnly(t *testing.T) {
	cleanup := []byte(
		`{"Action":"output","Test":"TestRelease/subtest","Output":"    testing.go:1617: TempDir RemoveAll cleanup: remove C:\\\\tmp: directory is not empty\\n"}` + "\n" +
			`{"Action":"fail","Test":"TestRelease/subtest"}` + "\n" +
			`{"Action":"fail","Test":"TestRelease"}` + "\n" +
			`{"Action":"fail","Package":"example.test"}` + "\n",
	)
	if !windowsTempDirCleanupOnly(cleanup) {
		t.Fatal("confirmed TempDir cleanup race was not recognized")
	}

	assertion := append([]byte{}, cleanup...)
	assertion = append(assertion, []byte(`{"Action":"fail","Test":"TestOther"}`+"\n")...)
	if windowsTempDirCleanupOnly(assertion) {
		t.Fatal("an unrelated failed test must not be treated as a cleanup race")
	}

	buildFailure := []byte(`{"Action":"build-fail","ImportPath":"example.test"}` + "\n")
	if windowsTempDirCleanupOnly(buildFailure) {
		t.Fatal("a build failure must stop the release")
	}
}

func TestGoReleaseChecksKeepTheFullPackageBoundary(t *testing.T) {
	want := []string{"./cmd/...", "./internal/...", "./routeros/...", "./tests/tools"}
	got := releaseGoTargets()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestImageBuildArgumentsPreserveDockerHealthcheck(t *testing.T) {
	for engine, want := range map[string][]string{
		"podman": {"build", "--format", "docker"},
		"docker": {"build", "--provenance=false"},
	} {
		if got := imageBuildArguments(engine); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", engine, got, want)
		}
	}
}

func TestImageBuildCommonPinsReleaseProvenance(t *testing.T) {
	want := []string{
		"--platform", "linux/arm64",
		"--build-arg", "SB_GATEWAY_VERSION=1.6.0",
		"--build-arg", "SB_GATEWAY_REVISION=abc123",
		"--build-arg", "SB_GATEWAY_SOURCE=https://github.com/example/project",
		"--tag", "sb-gateway:1.6.0-arm64",
	}
	got := imageBuildCommon("1.6.0", "abc123", "https://github.com/example/project", "sb-gateway:1.6.0-arm64")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAMD64BundleUsesMatchingImageAndCHRDirectory(t *testing.T) {
	got := imageBuildCommon("1.7.0", "abc123", "local", "xroute:test-amd64", "amd64")
	if got[1] != "linux/amd64" {
		t.Fatalf("platform=%q", got[1])
	}
	variables := []byte(":global SB_IMAGE_FILE usb1/sb-gateway/xroute-1.7.0-linux-arm64.tar\n:global SB_STORAGE_ROOT usb1/sb-gateway\n")
	output := string(bundleScriptContents("variables.example.rsc", variables, "amd64"))
	if !strings.Contains(output, "xroute/xroute-1.7.0-linux-amd64.tar") || strings.Contains(output, "usb1/") || strings.Contains(output, "-linux-arm64.tar") {
		t.Fatalf("AMD64 variables: %s", output)
	}
	if string(bundleScriptContents("variables.example.rsc", variables, "arm64")) != string(variables) {
		t.Fatal("ARM64 defaults changed")
	}
	actual, err := os.ReadFile("../../routeros/variables.example.rsc")
	if err != nil {
		t.Fatal(err)
	}
	actualAMD64 := string(bundleScriptContents("variables.example.rsc", actual, "amd64"))
	if strings.Contains(actualAMD64, "usb1/") || strings.Contains(actualAMD64, "-linux-arm64.tar") || !strings.Contains(actualAMD64, `:global "SB_STORAGE_ROOT" "xroute"`) {
		t.Fatal("actual AMD64 variables retain ARM64 paths or architecture")
	}
}

func TestStageInputArchiveSurvivesSourceRemoval(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dist", "raw.tar")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("docker archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, cleanup, err := stageInputArchive(root, filepath.Join("dist", "raw.tar"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := os.RemoveAll(filepath.Join(root, "dist")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(staged)
	if err != nil || string(body) != "docker archive" {
		t.Fatalf("staged archive was not preserved: body=%q err=%v", body, err)
	}
	if filepath.Dir(staged) != filepath.Join(root, ".cache-release", "release-input") {
		t.Fatalf("archive staged outside isolated release cache: %s", staged)
	}
}
