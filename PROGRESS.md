# OWU implementation progress

This log records the current state of Open Website Unblocker (OWU), its
personal browser proxy, and the companion macOS TCP client.

## Shipped foundation

- [x] English single-address web UI with liquid-glass light and dark themes.
- [x] Browser-password gate at Nginx and an arbitrary public HTTP/HTTPS target
  proxy behind that gate.
- [x] HTML, CSS, redirects, cookies, Fetch/XHR, WebSocket, Worker, dynamic DOM,
  import-map, charset, and common SPA compatibility rewriting.
- [x] Fixed-resource WebSocket-to-TCP tunnels for SSH, Minecraft, and other
  operator-configured services.
- [x] SwiftUI macOS client with Keychain credentials, certificate pinning,
  loopback listeners, and ordered WSS endpoint failover.
- [x] Multi-port Nginx templates, CI, release documentation, source archives,
  and rollback-oriented production deployment.

## Current compatibility iteration

The reported Poki homepage, SPA-routing, and `srcset` regressions are fixed,
with Level Devil used as a representative game-runtime check:

- GET and HEAD requests no longer acquire empty request-body framing.
- Virtual document URLs expose the destination pathname to client-side routers
  while retaining an OWU origin marker for reload and Referer recovery.
- `srcset` parsing preserves commas inside Cloudflare image-transform URLs and
  data URIs.
- Canonical resource rewriting remains idempotent, including CDN assets.

## Verification evidence

- Node: lint, 3/3 tests, and production build passed.
- Go 1.23: `go test ./...` and `go vet ./...` passed.
- Nginx: shared-host and dedicated-port-80 TLS templates pass `nginx -t`.
- Chromium: Poki homepage stays on `/`, reload succeeds, the verification window
  observed zero failed images, game navigation preserves `/en/g/...`, and
  Level Devil reaches a nested game canvas with ten runtime scripts.
- Production: unauthenticated root remains `401`; authenticated OWU UI,
  canonical Poki route, virtual Poki reload, and Level Devil page return `200`
  on the deployed host. TLS proxy entry points 443, 8080, 8443, and 9443 remain
  active.

Apple-only Network.framework lifecycle behavior still needs compilation and
sleep/wake/network-switch testing on a physical Mac before a signed macOS
binary release.

Origin-bound advertising, identity, DRM, and child-realm SDK behaviors remain
best effort. A target can still emit CSP or origin-pinning warnings even when
its main application and game canvas load successfully.
