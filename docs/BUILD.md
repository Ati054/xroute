# Build Storage

The image builder supports `--arch arm64` (default) and `--arch amd64`:

```sh
go run ./cmd/routeros-image --arch amd64 --output-dir dist/routeros-amd64
```

Images are separate single-platform Docker archives. The packager verifies the
embedded image configuration against the requested architecture; changing a TAR
filename does not change its platform. GitHub asset packaging accepts the matching
`-Architecture amd64` switch. ARM64 retains its existing bundle filename; AMD64
uses a separate `routeros-amd64-bundle.zip` filename.
The Docker context includes the RouterOS scripts embedded by the controller,
including the shared guided-uninstall cleanup. The runtime-artifact tests check
these files against the Docker ignore list before image compilation.
Run `sh scripts/static-check.sh` to check the runtime shell scripts and Docker
build steps, including the `cmd/xroute` command that prepares the bundled rule sets.
Each package includes architecture-specific `SHA256SUMS-arm64` or
`SHA256SUMS-amd64`. An AMD64-only package does not write the shared `SHA256SUMS`.
When both complete architecture sets are present in the output directory, the
packager verifies their manifests and generates one shared file covering both.
Keep both sets when refreshing that file for publication; an incomplete or
modified prior set is rejected before checksums are changed.

Before a large local compile or image build, check both the workspace disk and
the disk containing Docker and temporary files. On Windows with WSL, use:

```powershell
node scripts/check-build-space.mjs --wsl <distribution> -- wsl.exe --distribution <distribution> --user root --cd / --exec bash <build-script>
```

The wrapper requires at least 8 GiB available on every checked filesystem and
does not run the build if a disk check fails. It neither deletes files nor
restarts services. Run it before each large build phase; it is an admission
check, not a reservation against concurrent disk writers. A larger build may
need a higher `--min-free-gib` value.

Without `--wsl`, only local paths are checked. Repeat `--path` for additional
output volumes and `--wsl-path` for nondefault guest storage paths. A command
after `--` runs only after all checks pass, with its exit status preserved.
The WSL disk query starts from the Windows system directory and guest root;
the build command retains the caller's working directory.

Use one reusable Go build cache rather than keeping separate release caches.
For Linux compilation, keep that cache on the Linux filesystem rather than
duplicating it in the mounted Windows workspace.
Retain current verified artifacts; remove obsolete intermediate images and
inactive caches after inspecting ownership and container references. Shared
Docker hosts must not be cleaned with an unfiltered system-wide prune. BuildKit
garbage collection is useful but does not replace the pre-build disk check.
