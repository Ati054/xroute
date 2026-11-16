# Changelog

## 1.7.0

Initial public release under the **XRoute** name for MikroTik RouterOS 7.

- ARM64 router images with architecture checks before updates. Installations use external SSD/USB storage. The x86-64 CHR build will be published separately after its own testing.
- A browser panel in English, Russian, Persian with RTL, Simplified Chinese and Traditional Chinese, with localized guides and screenshots.
- Transparent TCP/UDP routing with TPROXY and route lists for devices and subnets. Supported exits include VLESS, Hysteria 2, Reverse VLESS and selected RouterOS WireGuard tunnels.
- Regional WAN exceptions for Russia, China and Iran, plus application and service presets.
- Separate proxy and WAN DNS paths with DoH/DoT, aligned with the active routing policy.
- URLTest and priority failover, with paired HTTPS comparisons for URLTest.
- Apply previews, configuration backups and guarded container updates with rollback. Existing settings and installation paths are preserved during upgrades.
- Incoming connections, TLS certificate management, mobile client subscriptions and exported connection profiles.
- Apply uses the latest saved settings after pending saves finish. DNS startup and TCP listener recovery are improved, and panel dependencies are updated.
