# OWU for macOS — implementation plan

## Product position

OWU for macOS is the desktop companion for the owner's password-protected OWU
gateway. The first working slice exposes a few loopback-only TCP ports and
transports them over authenticated WebSockets on the existing HTTPS entry.

```text
Minecraft / SSH client
        │ TCP to 127.0.0.1 only
        ▼
OWU macOS app (NWListener)
        │ WSS + Basic Auth + tunnel key
        ▼
OWU Nginx :443 ──► Go /tunnel/{resource_id}
                         │ exact server-side mapping
                         ▼
                    owned service
```

The client never sends a raw destination to `/tunnel/`. Server configuration
maps `ssh` and `minecraft` to exact `host:port` values. This keeps the tunnel a
small personal remote-access surface rather than a public CONNECT proxy.

## Current implementation

- SwiftUI macOS 14 app with liquid-glass light/dark presentation.
- Gateway URL, Basic Auth username/password, and optional TLS certificate pin.
- Password storage through Keychain Services.
- `NWListener` bound to `127.0.0.1` only.
- Binary TCP bridging through `URLSessionWebSocketTask`.
- One independent listener and lifecycle per configured resource.
- Default local mappings:
  - SSH: `127.0.0.1:2222` → resource ID `ssh`.
  - Minecraft: `127.0.0.1:25565` → resource ID `minecraft`.
- Portable Swift tests for request construction, credential placement, resource
  ID validation, and default ports.

The current server maps `ssh` to its own `127.0.0.1:22`. It also reserves the
`minecraft` ID for `127.0.0.1:25565`; that route becomes usable when a Minecraft
server is actually listening there, or when the operator changes the exact
server-side mapping.

## Authentication and trust

1. Nginx validates the same browser Basic Auth credential used by the website.
2. The app repeats the owner password in `X-OWU-Tunnel-Key`; Go validates it in
   constant time before choosing a resource.
3. Target sites and proxied JavaScript never receive this header.
4. A normal domain uses system TLS trust.
5. The current IP/self-signed deployment uses an explicit SHA-256 leaf pin.
6. A pin mismatch, wrong password, unknown resource, or offline target fails the
   connection without a direct-network fallback.

## UX

The first window contains one Gateway card and two resource cards. Each resource
shows its loopback address, a ready/failed state, a usage string, and one
Start/Stop button. No account or separate registration screen is required.

SSH example:

```sh
ssh -p 2222 root@127.0.0.1
```

Minecraft example:

```text
Server address: 127.0.0.1:25565
```

## Delivery milestones

### M1 — working fixed-resource tunnels (implemented)

- `/tunnel/{resource_id}` Go data plane.
- Nginx WebSocket upgrade route.
- loopback SSH and Minecraft listeners.
- Keychain password and self-signed certificate pin.
- unit tests plus a live SSH banner smoke test.

Acceptance: external WSS smoke test returns the configured server's OpenSSH
banner while requests without the tunnel key return `401` and unknown IDs return
`404`.

### M2 — Mac/Xcode validation and resilience

- Build both Apple silicon and Intel archives in Xcode.
- Validate certificate challenges on the current IP deployment.
- Add exponential reconnect for active client flows.
- Add sleep/wake recovery, network-change handling, idle timeouts, connection
  counters, and clearer handshake failures.
- Test concurrent SSH sessions and Minecraft login/play traffic.

Acceptance: 100 reconnect cycles, sleep/wake, and Wi-Fi changes complete without
leaking listeners or credentials.

### M3 — resource catalog and custom local mappings

- Fetch a signed list of resource IDs, display names, and recommended local ports.
- Let the owner change only local ports; remote destinations remain server-side.
- Add resource health and latency probes.
- Add a small Web tab that opens the existing `/browse/` UI in `WKWebView`.

Acceptance: adding a resource on the server makes it appear in the app without a
new build; an unknown or modified remote target remains impossible from the Mac.

### M4 — packaging

- App Sandbox network client/server capabilities where required.
- Hardened Runtime, Developer ID signing, notarization, stapling, update feed,
  rollback, and privacy disclosures.

Acceptance: install, launch, update, and uninstall on a clean supported Mac
without Xcode.

### M5 — optional system integration

- Evaluate `NEPacketTunnelProvider` only for resource-specific routing and split
  DNS after entitlement approval.
- Keep default routes and arbitrary destination forwarding disabled.
- Add physical-Mac tests for route overlap, IPv4/IPv6, revocation, and clean
  extension removal.

## Verification matrix

| Check | Expected result |
|---|---|
| Correct password + SSH resource | SSH banner over WSS |
| Missing/wrong tunnel key | HTTP `401` |
| Unknown resource ID | HTTP `404` |
| Minecraft backend offline | connection fails, no fallback |
| Wrong certificate pin | TLS challenge cancelled |
| Listener inspection | only `127.0.0.1:2222/25565` |
| Password storage | Keychain item; no password in URL/UserDefaults/logs |

## Apple references

- `NWListener`: https://developer.apple.com/documentation/network/nwlistener
- `URLSessionWebSocketTask`: https://developer.apple.com/documentation/foundation/urlsessionwebsockettask
- Keychain Services: https://developer.apple.com/documentation/security/keychain-services
- Network Extension: https://developer.apple.com/documentation/networkextension
- Notarization: https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
