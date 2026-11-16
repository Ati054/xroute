[English](README.md) | [Русский](README-RU.md) | [فارسی](README-FA.md) | [简体中文](README-ZH.md) | [繁體中文](README-ZH-TW.md)

<img src="public/favicon.svg" width="40" height="40" alt="XRoute">

# XRoute for MikroTik RouterOS 7

> **Looking for OpenWrt? → [XRoute for OpenWrt](https://github.com/Ati054/xroute-openwrt)**

I built XRoute so I could manage proxy routing on a MikroTik without editing
JSON by hand. It puts a web panel, Xray and the routing tools in one container,
for ARM64 routers. Choose a route for each
device, send selected services through the
normal WAN, and add backup servers when you need them.

This release provides ARM64 images. The x86-64 CHR build will follow after separate testing.

[Installation](docs/INSTALL.md) · [Downloads](https://github.com/Ati054/xroute/releases/latest)

![XRoute overview in English](docs/images/panel-overview-en.png)

![Route quality in a running XRoute panel](docs/images/panel-routing-en.png)

Live screenshots from a test installation. Device names and local IP addresses are examples; public endpoints are hidden.

## What you need

- An ARM64 MikroTik, such as hAP ax³, with RouterOS 7 and container support.
- An external SSD/USB disk with room for the container and its data.
- An internet connection and a supported proxy subscription or another exit.
- A domain and certificate if your incoming connections need them. Outbound-only routing does not require a domain.

## What it does

- Assign routes to LAN, Wi-Fi and supported VPN clients.
- Use VLESS, Hysteria 2, Reverse VLESS or selected RouterOS WireGuard tunnels as exits.
- Choose the lowest-latency server with URLTest, or keep a fixed priority order with backups.
- Keep selected countries, domains and services on the ordinary WAN.
- Manage incoming connections, TLS certificates, subscriptions and client profiles.
- Preview changes before Apply, view logs, make backups and update the container from the panel.

Incoming VLESS supports WebSocket, gRPC, HTTPUpgrade and XHTTP with TLS,
as well as direct REALITY connections. Hysteria 2 uses QUIC/UDP.
The [architecture guide](docs/ARCHITECTURE.md) describes the supported combinations
and the Xray patches used by the project.

XRoute adds its own marked RouterOS objects. Changes to existing objects
are limited to the settings you explicitly select; see
[RouterOS integration](docs/ROUTEROS.md) before installation.

## Install

1. Download the RouterOS bundle from [Releases](https://github.com/Ati054/xroute/releases/latest) **on your computer**, check its SHA-256 and extract it.
2. Upload the ready **`xroute` folder** to an external ext4 SSD, for example `usb1/xroute/`.
3. Run `/import file-name=usb1/xroute/setup.rsc` in the MikroTik terminal. Choose the disk and trusted LAN, then confirm.
4. Open the panel at the address printed by the installer and finish setup.

The router does **not** need access to GitHub for this installation. The installer creates the local REST certificate and service account. Existing VPNs remain in place; devices are assigned to XRoute later in the panel.

Follow the [installation guide](docs/INSTALL.md) for the commands, requirements
and recovery steps. Do not put your private `variables.rsc`, subscription links
or exported configuration in a public repository.

## Everyday use

Save your edits, review the proposed changes, then press Apply. Changes confined
to the container use a shorter path; changes to RouterOS networking get a backup
and a rollback guard. A successful save alone does not mean a change is active.

Use **Operations** for logs, backups, recovery and image updates. Updates keep
your saved configuration. The panel distinguishes a completed operation from
one still waiting for recovery or confirmation.

The [operations guide](docs/OPERATIONS.md) explains failover, DNS, update recovery
and the limits of latency-based server selection.

## Development

The controller and RouterOS tools are written in Go. The panel source is in
`app/`; the container serves a static Next.js export. Vinext/Vite is also used
for local development and the Sites preview, so `next.config.ts` and
`vite.config.ts` serve different build targets. Vite is not a Vitest config.

```sh
make validate
go test ./cmd/... ./internal/... ./tests/tools
npm ci --ignore-scripts
npm run lint
npm test
npm run build:static
```

See [development checks](docs/DEVELOPMENT-CHECKS.md) for release builds and
additional tests. CI cannot reproduce every MikroTik model or every external
proxy server, so test changes on a separate router before relying on them.

## Documentation

Use [Discussions Q&A](https://github.com/Ati054/xroute/discussions/categories/q-a)
for setup and usage questions.
Report problems using the [short bug-report template](.github/ISSUE_TEMPLATE/bug_report.md).
Remove subscription URLs, tokens, keys and personal details from logs.

- [Installation](docs/INSTALL.md)
- [Panel and settings](docs/WEB-UI.md)
- [Regional WAN exceptions](docs/REGIONAL-DIRECT.md)
- [Devices, routes and subscriptions](docs/CLIENTS.md)
- [Operations and recovery](docs/OPERATIONS.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Security](docs/SECURITY.md)
- [Changelog](CHANGELOG.md)

Maintainer notes: [product scope](docs/internal/PRODUCT.md) and
[interface design](docs/internal/DESIGN.md).

## License

Copyright © 2026 [Ati054](https://github.com/Ati054). Licensed under
[AGPL-3.0-or-later](LICENSE).

## FAQ

### Can I route only selected devices or services through a proxy?

Yes. Create a routing policy for selected LAN devices or WireGuard peers. Route
traffic through a proxy with direct WAN exceptions, or keep WAN as the default
and send only selected services and domains through the proxy. See the
[installation guide](docs/INSTALL.md) and [panel guide](docs/WEB-UI.md).

### Where is XRoute stored, and does it change my existing network?

The container image, rule databases and subscription catalog use external
SSD/USB storage to avoid filling or wearing out the router's internal flash.
XRoute creates marked RouterOS objects. Changes to existing network objects
require an explicit selection in the setup or Apply plan; XRoute does not
replace your LAN or Wi-Fi configuration. Network changes use a backup and
an automatic rollback guard.

### Why use XRoute instead of configuring Xray by hand?

One container bundles the web panel, Xray, transparent TPROXY for TCP and UDP,
split DoH/DoT DNS, URLTest and priority failover, guarded container updates,
and mobile client subscriptions. You can manage these from the panel without
editing Xray JSON or maintaining separate routing scripts.

## Community

Telegram: [@xroute_dev](https://t.me/xroute_dev)
