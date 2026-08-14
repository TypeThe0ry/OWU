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
- Explicitly marked public media/static edge caching for Douyin and
  Bilibili workloads, with HTTP/2, upstream keepalive pools, cache locking,
  background revalidation, and bounded disk/temp-file use.
- HLS playlist and standard DASH manifest URL rewriting for proxied segments,
  renditions, keys, maps, templates, initialization objects, and BaseURL trees.
- Nginx and systemd deployment templates, automated CI, secret scanning, and
  release documentation.

### Fixed

- Prevented already-proxied URLs from being rewritten a second time, which
  caused JavaScript and stylesheet requests to return a blank page.
- Preserved target-origin cookie isolation and canonical proxy URLs during SPA
  navigation.
- Removed empty request-body framing from GET and HEAD requests for strict edge
  renderers, including the Poki homepage failure reproduced during testing.
- Added virtual document URLs so SPA routers see the destination pathname while
  reloads remain routed through OWU, plus raw-query-preserving Referer recovery.
- Replaced comma splitting in `srcset` rewriting with a candidate parser that
  preserves Cloudflare image-resizing URLs and data URIs.
- Disabled tunnel handshake redirects and delayed local TCP reads until a WSS
  endpoint passes its TLS and WebSocket probe.
- Isolated Range, If-Range, and validator requests in an explicitly uncached
  streaming Nginx path so inherited server caches cannot turn a media `206`
  into a full `200`; target-cookie and Set-Cookie traffic also stays out of
  cache while unrelated OWU cookies use an isolated cache key.

No tagged release has been published yet.
