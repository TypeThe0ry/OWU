# OWU implementation progress

This log tracks the durable implementation goal for the authorized-access MVP.

## Goal contract

- Build a locally verifiable control plane and HTTP(S)/WebSocket gateway.
- Keep every upstream connection bound to an explicitly registered public resource.
- Deny anonymous arbitrary proxying, unsafe address ranges, unapproved ports, and direct browser fallbacks.
- Connect the English web UI to the real authorization decision API.
- Provide a macOS SwiftUI client foundation without claiming unavailable Apple entitlements.
- Finish only when builds, policy tests, integration tests, and a Docker Compose demo pass.

## Checkpoints

- [x] Product, gateway, and macOS architecture documents completed.
- [x] English web concept deployed as an owner-only preview.
- [x] Go control API and resource gateway implemented.
- [x] Web UI connected to the control API.
- [x] macOS client foundation implemented.
- [x] Docker Compose demo and authorized target fixture implemented.
- [x] Security and end-to-end acceptance suite passing.
- [x] Operator and developer runbooks updated.

## Current iteration

The personal OWU deployment is protected by browser Basic Auth. The web proxy now
rewrites static and dynamically inserted page references. The macOS client has a
working loopback TCP-to-WSS implementation for fixed `ssh` and `minecraft`
resource IDs; arbitrary remote destinations are not accepted by the tunnel API.

## Verification evidence

- `npm test`: production web build plus 3/3 Node tests passed.
- `npx eslint app tests`: passed.
- Go 1.23 container: `go test ./...` and `go vet ./...` passed; gateway image built successfully.
- Swift 5.10 container: `swift test` passed 22/22 portable core tests.
- `docker compose up --build -d`: web, gateway, and internal demo target started; health checks passed.
- `tests/e2e-demo.ps1`: single-input UI, same-origin BFF, one-time launch, proxied fixture response, and fail-closed denials passed.

Live deployment smoke tests: unauthenticated web access `401`, proxied
`example.com` `200`, loopback target `403`, missing tunnel key `401`, unknown
tunnel ID `404`, and the configured SSH tunnel returned
`SSH-2.0-OpenSSH_8.0`. Apple-specific SwiftUI, Security.framework, and
Network.framework branches still require compilation and lifecycle tests in
Xcode on a physical Mac.
