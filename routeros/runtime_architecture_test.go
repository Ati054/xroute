package routerosassets

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimePreflightArchitecture(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell required for the runtime preflight")
	}
	body, err := os.ReadFile("../scripts/preflight.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the shipped entrypoint's architecture phase before its mounted
	// certificate and secret checks, without creating production filesystem paths.
	prefix, _, ok := strings.Cut(strings.ReplaceAll(string(body), "\r\n", "\n"), "\nfor file in \\")
	if !ok {
		t.Fatal("cannot locate the preflight's mounted-file checks")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte("#!/bin/sh\nprintf '%s\\n' \"$XROUTE_TEST_MACHINE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, machine := range []string{"aarch64", "x86_64", "armv7l", "i686", "mips", "riscv64", ""} {
		t.Run(machine, func(t *testing.T) {
			supported := machine == "aarch64" || machine == "x86_64"
			override := "1"
			if supported {
				override = "0"
			}
			cmd := exec.Command(shell, "-c", prefix+"\nprintf 'architecture accepted\\n'\n")
			cmd.Env = append(os.Environ(), "PATH="+bin, "XROUTE_TEST_MACHINE="+machine, "SB_ALLOW_NON_ARM64="+override)
			output, err := cmd.CombinedOutput()
			if supported {
				if err != nil || string(output) != "architecture accepted\n" {
					t.Fatalf("supported architecture rejected: %s (%v)", output, err)
				}
				return
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 78 || !strings.Contains(string(output), "preflight:") || strings.Contains(string(output), "architecture accepted") {
				t.Fatalf("unsupported architecture was not rejected with EX_CONFIG: %s (%v)", output, err)
			}
		})
	}
}
