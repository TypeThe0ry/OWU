# OWU for macOS — personal proxy app plan

## Product position

OWU for macOS is a small SwiftUI browser for the same password-protected OWU web proxy. The user enters a website address, and the app loads the encoded `/browse/{origin-token}/...` route in `WKWebView`. Destination sites receive the OWU server connection rather than a direct connection from the Mac.

The first release is an embedded browser, not a system VPN, SOCKS server, packet tunnel, or traffic-obfuscation tool.

## First-run experience

1. Show the OWU server address, fixed to the owner's deployment by default.
2. Ask for the Basic Auth username and password once.
3. Save the credential in Keychain, never `UserDefaults` or logs.
4. Validate `/healthz` through the authenticated HTTPS entry.
5. Open the single-address home screen.

For the current IP deployment, the server uses a self-signed certificate. A personal Developer ID build may pin that exact certificate fingerprint. A distributable release must use a normal domain and publicly trusted TLS certificate; it must not silently accept arbitrary server-trust failures.

## Main interface

```text
┌──────────────────────────────────────────────────────────────┐
│  OWU                                      Light / Dark   •   │
│                                                              │
│                 Open the web through OWU.                    │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ example.com                              Open website  │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  Connected to your personal proxy                            │
└──────────────────────────────────────────────────────────────┘
```

After navigation, the window becomes a compact browser with Back, Forward, Reload, Home, an editable address field, current loading state, and an external-browser button.

## Technical architecture

```text
SwiftUI shell
├── AppModel
│   ├── OWU server configuration
│   ├── URL normalization and origin-token encoding
│   └── connection and error state
├── KeychainCredentialStore
│   └── Basic Auth username/password
├── ProxyWebView
│   ├── WKWebView
│   ├── WKNavigationDelegate
│   └── WKUIDelegate
└── TrustPolicy
    ├── normal system trust for production domains
    └── exact certificate pin for the personal IP build
```

Use `WKWebView` so page networking, cookies, JavaScript, downloads, and browser navigation use Apple's WebKit stack. App Sandbox needs outgoing network access through `com.apple.security.network.client`.

## Authentication handling

- Handle only an HTTP Basic authentication challenge whose protection space host exactly matches the configured OWU server.
- Supply the Keychain credential with session persistence.
- Never supply the OWU credential to another host or to a target-site authentication challenge.
- On HTTP 401, clear the in-memory credential, return to the credential screen, and preserve the typed destination locally.
- Provide an explicit “Forget proxy credential” action that deletes the Keychain item.

The proxy remains responsible for stripping `Authorization` before upstream requests.

## Navigation rules

- Accept only `http` and `https` destination addresses.
- Reject embedded usernames and passwords.
- Add `https://` when the scheme is omitted.
- Encode only `scheme://host[:port]` with Base64 URL encoding; keep the target path and query after the opaque origin token.
- Load only the configured OWU origin in the main `WKWebView`.
- Treat downloads, `mailto:`, `tel:`, and external application schemes as explicit user-confirmed handoffs.
- Display the decoded destination hostname in app chrome even though the web view URL remains on OWU.

## Privacy and storage

- Store the OWU credential in Keychain with a service identifier scoped to the configured server.
- Keep browsing history off by default.
- Do not add analytics, crash payloads containing URLs, request-body logging, or cross-device sync.
- Provide clear buttons for clearing WebKit website data and target cookies.
- Log only coarse connection state in release builds.

## Compatibility expectations

The app inherits the web proxy's compatibility boundary. Server-rendered sites and conventional SPAs should work best. Strict CSP, OAuth redirect validation, CAPTCHA, DRM, Service Worker, target Basic Auth, certificate pinning, and JavaScript that hardcodes origin assumptions can fail. The app cannot make an incompatible rewrite proxy equivalent to a native network tunnel.

## Delivery milestones

### M0 — Swift package and URL model

- SwiftUI macOS 14 target.
- URL normalization and origin-token encoder shared with unit tests.
- Acceptance: Unicode hostnames, ports, paths, queries, fragments, and invalid schemes have deterministic tests.

### M1 — Authenticated WebKit shell

- Server setup, Keychain credential storage, Basic Auth challenge handling, and certificate policy.
- Acceptance: a correct credential opens OWU; an incorrect credential returns to setup; the credential never appears in logs or preferences.

### M2 — Browser workflow

- Address bar, navigation controls, progress, error page, downloads, theme, and keyboard shortcuts.
- Acceptance: browse through OWU across two sites, navigate within each site, return Home, and clear website data.

### M3 — Hardening

- Exact-host credential scoping, pinned personal certificate option, normal public TLS path, process crash recovery, sleep/wake testing, and accessibility review.
- Acceptance: authentication is never answered for a non-OWU host; an unexpected certificate fails closed.

### M4 — Distribution

- Developer ID signing, hardened runtime, notarization, update feed, rollback, and privacy documentation.
- Acceptance: install and update on a clean Mac without Xcode; previous signed build remains recoverable.

## Explicit non-goals for the first app

- `NEPacketTunnelProvider` or system-wide routing.
- Per-app VPN, SOCKS5, HTTP CONNECT, UDP, or arbitrary TCP forwarding.
- Protocol camouflage or network-policy bypass.
- Multi-user accounts, cloud history, shared credentials, or public proxy discovery.

## Apple references

- `WKWebView`: https://developer.apple.com/documentation/webkit/wkwebview
- `WKNavigationDelegate`: https://developer.apple.com/documentation/webkit/wknavigationdelegate
- Keychain Services: https://developer.apple.com/documentation/security/keychain-services
- App Sandbox outgoing network entitlement: https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.network.client
- Notarization: https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
