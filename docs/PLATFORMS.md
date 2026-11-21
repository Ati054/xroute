# Container platforms and storage

XRoute builds separate `linux/arm64` and `linux/amd64` images. RouterOS reports
the x86 host architecture as `x86` or `x86_64`; both select the 64-bit AMD64 image,
not a 32-bit `386` image. The panel verifies the architecture inside an uploaded
TAR before accepting it as an update candidate. Registry updates select and verify
the matching platform through the digest-pinned manifest chain. A verified upload
receipt is checked again before scheduling an update. Existing ARM64-only receipts
remain valid only on ARM64.

The offline recovery generator accepts `--arch amd64` for an AMD64 archive and
recognizes both XRoute tags and legacy `sb-gateway` image tags. Its default
architecture stays ARM64 for existing recovery commands.

On CHR, a dedicated project directory such as `xroute` may reside on the system
disk. A second disk can use `slot1/xroute` or `disk1/xroute`. Bare disk names,
system directories and traversal components are rejected. Generated workers check
that a bare project directory runs on x86 CHR before changing RouterOS objects.
The guided installer offers `System disk: xroute` only on x86 CHR with at least
1 GiB of free disk space. It also offers mounted ext4 disks with the same minimum
free space. Hardware routers do not receive the system-disk option. Expanding a
CHR virtual disk is an administrator operation; the installer never resizes or
formats disks.
The storage-aware boot probe accepts this CHR directory as a system-disk path;
external-volume readiness and mount-source checks remain required elsewhere.
Fresh installation and manual removal enforce the same device boundary. Boot
never creates missing folders; only explicit installation may create mount folders.
Post-Apply file cleanup also accepts the system-disk project directory after
checking the live RouterOS board and architecture. It removes only known installer
files and old managed backups; unrelated files and nested runtime data are kept.
Full removal keeps the service account and its permissions until managed files
and scripts have been removed, allowing a subsequent guided installation.
Its fixed worker and scheduler include RouterOS's `ftp` file-operation permission
and `sensitive` permission for removing generated binary backups. This does not
enable the FTP service.
The existing external-storage path remains the default for physical routers;
internal `flash/` storage is not enabled by this platform change.
New ARM64 installation examples use `usb1/xroute`. Existing installation paths
and managed RouterOS object identifiers remain supported during upgrades.

TAR compression affects transfer size. It does not reduce the space occupied by
extracted files. Installation and update checks must account for the archive,
the extracted candidate, persistent data and the retained previous image.

The multi-architecture implementation requires separate built-image and RouterOS
qualification. Source tests alone do not establish runtime support on another
architecture.
Release and CI Go suites include embedded RouterOS asset checks alongside the
controller, packager and recovery checks. The PowerShell bundle wrapper accepts
`-Architecture amd64`; ARM64 remains its default.
