# Changelog

## 1.7.8

- Upload large recovery archives through the panel. The panel checks for duplicates before sending the file.
- Reconcile container memory changes after restart so pending operations can clear without opening Settings.
- Allow WireGuard and Reverse VLESS in routing policies without a provider subscription.
- Apply changes to a policy without LAN clients while its outbound is blocked. Policies serving LAN clients still require a working outbound.
- Include manually configured internal subnets in remote client access and hide transport subnets.

## 1.7.7

- Document where XRoute stores recovery backups and show the correct status in the panel.
- Localize TLS forms, configuration history and device states in all five interface languages.
- Include `setup.rsc` in the offline installation bundles for ARM64 and x86_64 CHR.

## 1.7.6

- Use the uploaded archive for guided installation and catch missing or incomplete uploads before changing RouterOS settings.
- Fill in REST connection details when you select a RouterOS export. Show the manual fields if the export omits any required details.
- Add a distinct file picker button for the RouterOS CA certificate.

## 1.7.5

- Shorten setup to three steps and read RouterOS connection settings from the installer export.
- Use the RouterOS container-network address for internal DNS by default and remove the legacy TUN field from setup.
- Select the RouterOS CA certificate file directly in the wizard.

## 1.7.4

- Reuse the installer's panel access settings and preserve additional LAN and VPN interfaces and subnets.
- Preserve public inbounds and client subscription settings when reopening the setup wizard.
- Keep manual network entry under Internal networks and show new subnets that need review.
- Retry stalled DoH setup or reused connections within the existing request deadline.
- Fix the setup review layout, routing dialog spacing and configuration history labels.

## 1.7.3

- Improve RouterOS export import and first-run setup.
- Position managed firewall rules safely alongside dynamic system rules.
- Compress static panel assets and update saved Nginx configurations automatically.
- Improve dialog layout, keyboard controls and operation labels in all five panel languages.

## 1.7.2

- Add AMD64 images and guided system-disk installation for x86_64 CHR.
- Restore installer-owned management settings and remove unused installer certificates during full removal; preserve manually configured services.
- Translate the full-removal preview in all five panel languages.
- Preserve checksums for both architectures when packaging release assets.
- Allow up to 30 minutes for streaming image uploads over slow links.

## 1.7.1

- Improve offline installation and recovery after a failed first Apply.
- Preserve network settings when reopening the setup wizard.
- Reduce RouterOS temporary-file cleanup requests and use the maintenance timeout for retained-file cleanup.
- Reject image versions whose installation directories are already in use.
- Clarify panel text in all five languages.

## 1.7.0

Initial public release under the **XRoute** name for MikroTik RouterOS 7.

- Guided installation from a ready folder uploaded from a computer, without GitHub access from the router. Local REST certificates, a dedicated account and an encrypted pre-install backup are created automatically; manual installation remains available.
- ARM64 router images with architecture checks before updates. Installations use external SSD/USB storage. The x86-64 CHR build will be published separately after its own testing.
- A browser panel in English, Russian, Persian with RTL, Simplified Chinese and Traditional Chinese, with localized guides and screenshots.
- Transparent TCP/UDP routing with TPROXY and route lists for devices and subnets. Supported exits include VLESS, Hysteria 2, Reverse VLESS and selected RouterOS WireGuard tunnels.
- Regional WAN exceptions for Russia, China and Iran, plus application and service presets.
- Separate proxy and WAN DNS paths with DoH/DoT, aligned with the active routing policy.
- URLTest and priority failover, with paired HTTPS comparisons for URLTest.
- Apply previews, configuration backups and guarded container updates with rollback. Existing settings and installation paths are preserved during upgrades.
- Incoming connections, TLS certificate management, mobile client subscriptions and exported connection profiles.
- Apply uses the latest saved settings after pending saves finish. DNS startup and TCP listener recovery are improved, and panel dependencies are updated.
