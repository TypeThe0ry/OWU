# Changelog

All notable changes to Open Website Unblocker will be documented here. The
project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- English single-field OWU web interface with responsive light and dark liquid-
  glass themes.
- HTTP and HTTPS browser proxy routes with redirect, cookie, form, CSS, dynamic
  DOM, Fetch, XHR, EventSource, Beacon, Worker, and WebSocket rewriting.
- Import-map rewriting, legacy HTML charset conversion, and root-relative
  navigation fallback for broader application compatibility.
- Public-address validation with DNS-aware pinned upstream dialing.
- Fixed-resource WebSocket-to-TCP tunnels for SSH, Minecraft, and other
  operator-configured TCP services.
- SwiftUI macOS 14 client with loopback-only listeners, Keychain credential
  storage, and optional certificate pinning.
- Ordered macOS WSS gateway failover across ports 443, 80, 8080, 8443, 9443,
  and operator-configured additional TLS ports.
- Shared-host and dedicated-address Nginx profiles for secure multi-port tunnel
  entry points.
- Nginx and systemd deployment templates, automated CI, secret scanning, and
  release documentation.

### Fixed

- Prevented already-proxied URLs from being rewritten a second time, which
  caused JavaScript and stylesheet requests to return a blank page.
- Preserved target-origin cookie isolation and canonical proxy URLs during SPA
  navigation.
- Disabled tunnel handshake redirects and delayed local TCP reads until a WSS
  endpoint passes its TLS and WebSocket probe.

No tagged release has been published yet.
