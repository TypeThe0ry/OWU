# macOS Access Client — Product and Technical Plan

Status: implementation-ready plan  
Apple API and policy check: 2026-08-13  
Language: the shipped app and all first-party help text are English-first.

## 1. Product decision

Build one free macOS client with two delivery stages:

1. **MVP — local proxy:** a SwiftUI app runs authenticated SOCKS5 and HTTP CONNECT listeners on loopback. The user configures a browser, SSH client, database tool, or another app to use one of those listeners. This stage uses Network.framework and does not alter the system routing table.
2. **Full client — system tunnel:** add an `NEPacketTunnelProvider` extension for split routing and private DNS. The system sends only approved resource traffic into the tunnel; the gateway still performs the final authorization check.

The client is free to download and use. “Free for everyone” means anyone may create an account and use resources that they own or have been granted access to. It does **not** mean anonymous access or access to arbitrary Internet/internal destinations. Apple Developer Program membership, signing, hosting, bandwidth, abuse handling, and support still have real operating costs.

### Safety boundary

The product is for user-owned servers, labs, and explicitly authorized resources. It will not implement protocol obfuscation, traffic camouflage, policy evasion, firewall-detection bypass, an anonymous relay, or an unrestricted public proxy.

Every typed destination must resolve to a control-plane resource grant. The client sends a signed `resource_id` grant to the gateway; it never asks the gateway to connect to an unchecked raw `host:port`. The gateway resolves the resource from its own catalog, rejects private/metadata/loopback targets unless explicitly owned and authorized, and checks user, device, protocol, port, expiry, and revocation on every new flow.

## 2. User experience

The app should feel like a small connectivity utility, not a networking console.

### Primary flow

1. Open the app and choose **Sign in**.
2. Complete OIDC login in the system browser.
3. The app registers this Mac and downloads the user's approved resource catalog.
4. Enter a value such as:
   - `https://wiki.example.com`
   - `https://service.example.com:8443`
   - `ssh.example.com:22`
5. The app resolves the input against the authorized catalog.
6. For HTTP/HTTPS, **Open Website** launches the approved web gateway URL in the default browser. For TCP resources, the app shows a one-line SOCKS/CONNECT configuration or copyable client command.

The single input is a convenience lookup, not an arbitrary-URL proxy. Unknown, malformed, or unauthorized destinations fail closed with a clear message.

### MVP information architecture

- **Home:** quick access field, status, recent approved resources.
- **Proxy:** SOCKS5 and HTTP CONNECT addresses, session credentials, start/stop, setup snippets.
- **Resources:** searchable authorized catalog; no global Internet directory.
- **Device:** device name, key status, last policy sync, revoke this device.
- **Settings:** launch behavior, diagnostics export, privacy and open-source notices.

### English wireframes and copy

```text
┌────────────────────────────────────────────────────┐
│ Access Client                               ● Ready │
│                                                    │
│ Access an approved resource                        │
│ ┌────────────────────────────────────────────────┐ │
│ │ Enter a URL or host:port                       │ │
│ └────────────────────────────────────────────────┘ │
│                                      [Continue]    │
│                                                    │
│ Local proxy                                       │
│ SOCKS5       127.0.0.1:1080          [Copy]       │
│ HTTP CONNECT 127.0.0.1:8080          [Copy]       │
│ Session credentials                  [Reveal]      │
│                                                    │
│ Recent resources                                  │
│ Production Wiki                         [Open]     │
│ Lab SSH                                  [Setup]    │
└────────────────────────────────────────────────────┘
```

```text
┌────────────────────────────────────────────────────┐
│ Sign in                                            │
│                                                    │
│ Access servers and services you own or are         │
│ authorized to use.                                 │
│                                                    │
│ Before you continue                                │
│ • Connections are encrypted to the access gateway.│
│ • Access is limited by resource and port.          │
│ • We record connection metadata for security.      │
│ • We do not record passwords or request bodies.    │
│                                                    │
│ [Privacy Policy]                  [Sign in]         │
└────────────────────────────────────────────────────┘
```

```text
┌────────────────────────────────────────────────────┐
│ Resource not available                             │
│                                                    │
│ “example.internal:22” is not in your approved      │
│ resource list. Ask the resource owner for access.  │
│                                                    │
│ Request ID: req_7M…                 [Copy details] │
│                                      [Done]        │
└────────────────────────────────────────────────────┘
```

Full-client status copy:

- `Protected resources are connected`
- `Only approved resource traffic uses the tunnel`
- `Reconnecting after a network change…`
- `Your device access was revoked. Sign in again or contact the resource owner.`
- Never use the claim `All traffic is protected` unless a separately reviewed full-tunnel mode actually routes all traffic.

Accessibility requirements: full keyboard navigation, VoiceOver labels for every status/control, no color-only state, Reduce Motion support, selectable/copyable error details, and a minimum window size that does not clip at larger text sizes.

## 3. MVP architecture: SwiftUI local proxy

### Targets and modules

```text
AccessClient.app (SwiftUI, macOS 14+)
├── AccessUI                 navigation, status, setup guidance
├── IdentityCore            OIDC/PKCE, token refresh, device proof
├── ResourceCatalog         policy sync and input-to-resource matching
├── LocalProxyKit           SOCKS5 + HTTP CONNECT parsers/listeners
├── GatewayTransport        authenticated TLS stream transport on 443
├── SecureStorage           Keychain and device key
└── Diagnostics             redacted local logs and export
```

Use Swift concurrency with actors around token state, resource catalog state, each listener, and each gateway session. Keep protocol parsers independent of UI and test them with fragmented and adversarial byte streams.

### Local listeners

- Create separate TCP `NWListener` instances for SOCKS5 and HTTP CONNECT.
- Bind only to IPv4 and IPv6 loopback, never `0.0.0.0` or a LAN interface.
- Default to fixed, user-editable ports `1080` and `8080`; if unavailable, choose a high port and show it clearly.
- Require per-launch proxy authentication by default. Generate a 32-byte random secret, keep it in memory, rotate it whenever the proxy starts, and expose it only through an explicit **Reveal** action.
- SOCKS5 MVP supports version 5, username/password authentication, `CONNECT`, IPv4, IPv6, and domain-name address types. Reject `BIND` and `UDP ASSOCIATE` with the correct protocol response.
- HTTP MVP supports `CONNECT host:port` with `Proxy-Authorization`. Reject forward-proxy `GET`, absolute-form requests, request bodies, malformed authority values, and non-CONNECT methods.
- Cap pre-auth bytes, header size, handshake duration, idle duration, concurrent flows, and aggregate bandwidth.
- Stop listeners and zero the session secret when the user signs out or quits the app.

For sandboxed distribution, enable both outgoing network client and incoming network server entitlements. Apple's sandbox documentation treats listening as an incoming-network capability even when the app intends to listen only on loopback.

### Destination authorization

The matching algorithm is deliberately strict:

1. Parse and normalize the user/app destination: IDNA hostname normalization, lowercase DNS name, explicit/default port, and bracketed IPv6 handling.
2. Match against the locally cached signed resource catalog.
3. If the catalog is stale or ambiguous, call `POST /v1/resource-resolutions`.
4. Receive a short-lived route grant containing `resource_id`, canonical names, allowed protocol/port, user ID, device-key thumbprint, gateway audience, expiry, and nonce.
5. Sign the connection request with the device key and send the route grant to the gateway.
6. The gateway revalidates the grant and independently looks up the target. Client-supplied raw target text is audit context only and cannot override the catalog target.

SOCKS domain names are resolved by the authorized gateway, preventing local DNS from becoming an authorization decision. IP-literal requests are accepted only if the catalog explicitly maps that exact IP and port to an approved resource.

Recommended gateway stream messages:

```text
OPEN { protocol_version, resource_id, port, route_grant, device_proof, trace_id }
OPEN_OK { flow_id, idle_timeout_seconds }
OPEN_ERROR { stable_code, request_id }
DATA { flow_id, bytes }
HALF_CLOSE { flow_id }
CLOSE { flow_id, reason }
```

Start with one TLS 1.3 gateway connection per local flow for implementation clarity. Add HTTP/2 or QUIC multiplexing only after measuring connection setup cost. Do not add a custom “stealth” transport.

### Authentication and device enrollment

- Use `ASWebAuthenticationSession` to open the OIDC authorization endpoint in the system browser.
- Use Authorization Code with PKCE, `state`, `nonce`, an exact callback, and a short authorization-code lifetime.
- Keep access tokens in memory and make them short-lived. Store only the refresh/session credential required for continuity in Keychain.
- Create a device key before registration. Prefer a non-exportable Secure Enclave P-256 signing key when supported; fall back to a Keychain-stored P-256 private key on unsupported Macs.
- Do not require Touch ID/user presence for every signature because the packet-tunnel extension must reconnect while the containing app is not foregrounded. Validate the chosen access-control flags on Intel, Apple silicon, locked-session, sleep/wake, and extension processes during the entitlement spike.
- Register only the public key and device metadata with the control plane. Prove possession by signing a server nonce during enrollment and token refresh.
- Bind route grants to the device-key thumbprint. Support server-side device suspension/revocation and client-side key rotation.
- Logout deletes tokens and stops connectivity. **Remove this device** also revokes the device server-side and deletes its private key after confirmation.

The host app and future tunnel extension use a dedicated Keychain access group. Store nonsecret policy cache, display preferences, and redacted status in an App Group container; never put refresh tokens or private-key material in `UserDefaults` or `providerConfiguration`.

### MVP lifecycle

```text
signedOut → enrolling → stopped → starting → ready
                          ↑          ↓       ↓
                          └──── stopping ← error/retry
```

- A local-proxy MVP is available only while the app process is running.
- “Launch at login” is opt-in and must be explained; it does not imply a system tunnel.
- On network loss, existing gateway flows fail with a stable error. Retry only idempotent control-plane operations automatically; do not silently replay arbitrary TCP streams.
- On catalog refresh, new connections use new policy. Existing connections may continue only until their grant/maximum-session expiry or an explicit revocation event.
- The UI derives state from the listeners and transport, not from optimistic button taps.

## 4. Full architecture: Network Extension packet tunnel

### Xcode target model

```text
AccessClient.app
└── AccessPacketTunnel.appex / system extension
    └── PacketTunnelProvider : NEPacketTunnelProvider
```

The containing app uses `NETunnelProviderManager` to create, save, load, enable, start, and stop its VPN configuration. `NETunnelProviderManager` configurations are scoped to the app that created them. Observe `NEVPNStatusDidChange` instead of treating `startVPNTunnel()` returning as “connected.”

The provider owns the data path:

1. `startTunnel` loads nonsecret configuration and obtains a device-bound, short-lived tunnel credential.
2. It establishes a TLS 1.3 control/data connection to the authorized gateway on `443` using Network.framework.
3. It downloads the current route and private-DNS mapping.
4. It calls `setTunnelNetworkSettings` and waits for success.
5. Only then does it complete `startTunnel` and begin the `packetFlow` read/write loops.
6. The tunnel edge decapsulates packets and enforces the overlay-IP/port-to-resource mapping before opening a target flow.

For the first packet-tunnel release, encapsulate IP packets in a small versioned framing protocol over TLS/TCP. The server needs a dedicated packet-tunnel edge with per-session addresses, replay protection for control messages, flow/NAT state, MTU handling, and strict route-to-resource authorization. This is separate from the MVP's flow stream endpoint. Evaluate QUIC later for improved loss behavior; do not make it a release dependency.

### Split routing

Default behavior is resource-only routing:

- Assign each approved named resource an overlay IPv4/IPv6 address for the session.
- Install only the overlay prefixes and explicitly approved private CIDRs in `NEIPv4Settings.includedRoutes` / `NEIPv6Settings.includedRoutes`.
- Avoid `0.0.0.0/0`, `::/0`, and `includeAllNetworks` in the normal product.
- Detect overlap with local routes and show a specific conflict instead of unexpectedly capturing a user's LAN.
- The gateway maps overlay address + port back to a resource ID and rechecks policy. A synthetic address with no current mapping is dropped.
- Use `excludedRoutes` only for documented operational exceptions, never to bypass an administrator's policy.

Apple documents that system routes can supersede included/excluded routes. `enforceRoutes` can strengthen route behavior but is mutually exclusive with `includeAllNetworks`; treat it as a separately tested hardening option, not an MVP default.

“Split by app” is not a consumer-app toggle. Apple's `NETunnelProviderManager` documentation says production per-app VPN configuration requires MDM. Offer per-app routing only as a later managed-enterprise feature, with MDM profiles and managed apps; do not promise it for unmanaged Macs.

### Private DNS

Use the packet tunnel's `NETunnelNetworkSettings.dnsSettings`; do not request the separate system-wide `dns-settings` entitlement unless a future product genuinely ships a standalone DNS-settings feature.

- Run an authenticated resolver reachable only through the tunnel.
- Set `NEDNSSettings.servers` to its tunnel address.
- Set `matchDomains` to approved resource DNS suffixes and `matchDomainsNoSearch = true` so unrelated queries continue to use the user's normal resolver.
- Keep the original hostname in the application connection so HTTPS certificate validation and SNI remain correct; private DNS returns the resource's overlay address.
- Use a domain beneath a registered service-controlled parent for generated aliases. Do not use `.local`, which conflicts with multicast DNS behavior.
- For shared public suffixes, the private resolver must forward or return normal answers for unauthorized names, while routes and the gateway still enforce resource grants.
- Flush/rotate mappings when policy changes, device access is revoked, or a tunnel session ends.

Apple notes that an empty string in `matchDomains` makes the resolver the default. Never insert it in split-DNS mode.

### Provider lifecycle and recovery

- `startTunnel`: authenticate, connect, apply settings, then call completion. Apply a deadline and return a meaningful provider error if any prerequisite fails.
- `stopTunnel`: stop accepting packets, close the transport, zero transient credentials, emit final counters, and call completion promptly for every stop reason.
- `sleep`: pause keepalives and finish the sleep completion handler promptly. Do not hold up system sleep for log upload.
- `wake`: reauthenticate/reconnect if needed and refresh routes/DNS before declaring the path healthy.
- Network transition: set `reasserting = true`, create a fresh transport, reapply settings if necessary, then clear `reasserting`. Existing TCP streams may fail; do not claim seamless recovery until verified.
- Containing-app exit: tunnel correctness must not depend on the SwiftUI process remaining alive.
- Provider restart: recover from Keychain/App Group state plus fresh control-plane policy, not in-memory assumptions.
- Revocation: a pushed or polled revocation cancels the session immediately, removes tunnel settings through normal provider shutdown, and requires a fresh login or administrator action.

## 5. Entitlements and distribution risk

### MVP entitlement set

For a Mac App Store sandboxed build:

```text
com.apple.security.app-sandbox = true
com.apple.security.network.client = true
com.apple.security.network.server = true
keychain access group for AccessClient
App Group only when shared state is needed
```

The local-proxy MVP does not need Network Extension entitlement because it does not create a system VPN/interface.

### Full-client entitlement set

- Add the Network Extensions capability and `com.apple.developer.networking.networkextension`.
- Mac App Store packet-tunnel builds use the `packet-tunnel-provider` entitlement value.
- Developer ID system-extension builds use the documented `packet-tunnel-provider-systemextension` value and an appropriate Developer ID provisioning profile.
- The host and provider targets need correctly matched App IDs, signing profiles, extension embedding, App Group, and Keychain access groups.
- Validate exact signing and installation behavior in a throwaway entitlement spike before building the full tunnel. Do not assume a development profile proves Developer ID or App Store distribution will work.

### Recommended distribution sequence

1. **Internal development:** development-signed MVP and packet-tunnel spike on physical Macs.
2. **Public MVP pilot:** Developer ID signed, Hardened Runtime enabled, notarized, and stapled. This gives rapid iteration without Mac App Store review; the MVP still remains sandbox-capable.
3. **Mac App Store decision:** submit only after the legal entity, privacy disclosures, review environment, and territory licensing position are ready.
4. **Full client:** maintain separate, reproducible signing pipelines for Mac App Store app-extension distribution and Developer ID/system-extension distribution if both are required.

### App Store and policy risks

- Apple's App Review Guideline 5.4 says VPN apps must use the Network Extension VPN APIs and may only be offered by developers enrolled as an organization.
- Before the first use of a VPN service, the app must clearly declare what user data is collected and how it is used. VPN data may not be sold, used, or disclosed to third parties for unrelated purposes, and the privacy policy must commit to this.
- Regional VPN licensing may be required; applicable license information belongs in App Review notes. Limit storefront availability until counsel/operations confirms each territory.
- App Review needs a working demo account, reachable review resources, and explanatory notes for the custom tunnel and why each entitlement is required.
- Mac App Store builds must use App Sandbox and store-driven updates. Developer ID builds should be notarized; system extensions and all embedded executable code must be signed correctly.
- Encryption export-compliance declarations still apply even when the end-user price is zero.
- Review may classify even the local-proxy MVP as a VPN/service-access product based on behavior and marketing. Developer ID distribution is a launch path, not a way to ignore privacy, security, or local law.

## 6. Security and privacy requirements

- No anonymous mode and no “disable policy checks” debug switch in release builds.
- Gateway authorization is authoritative; client-side catalog checks exist for UX and load reduction only.
- Validate all protocol lengths and states; fuzz SOCKS5, HTTP CONNECT, grant parsing, and packet framing.
- Use system trust evaluation for TLS and a controlled certificate-rotation plan. If certificate pinning is added, ship overlapping backup keys and a recovery path.
- Do not log URLs beyond normalized approved resource identity, DNS query contents beyond approved resource names, request/response bodies, credentials, tokens, packet payloads, or clipboard contents.
- Audit metadata may include user, device, resource, port, start/end time, bytes, result, policy decision, and request ID, subject to a published retention period.
- Diagnostics export is explicit and redacted. Display the generated archive contents before saving when practical.
- Clipboard copies of proxy credentials should expire/clear when possible, but the UI must warn that other apps may have observed clipboard history.
- Ship a signed remote kill/revocation path for compromised devices and a key-rotation runbook.

## 7. Delivery plan and exit gates

### Iteration 0 — Apple entitlement and signing spike (1 week)

Deliverables:

- Empty SwiftUI host plus minimal packet-tunnel provider.
- Development and Developer ID profiles, App Group, and shared Keychain proof.
- Provider can start, set a test route/DNS setting, survive host-app exit, stop, sleep, and wake on two Apple silicon Macs and one supported Intel Mac if Intel is in scope.
- Written App Store organization/licensing decision owner.

Exit gate: archive, sign, install, and run a notarized Developer ID build on a clean Mac. If Network Extension distribution cannot be provisioned, continue only with the local-proxy product until resolved.

### Iteration 1 — app shell, login, and device identity (1–2 weeks)

- SwiftUI navigation and English copy.
- OIDC + PKCE using `ASWebAuthenticationSession`.
- Secure Enclave/Keychain device key, registration, refresh, logout, and revoke.
- Signed resource catalog and strict input parser.

Exit gate: a new Mac can enroll, sign a challenge, list only its approved resources, refresh after relaunch, and revoke itself.

### Iteration 2 — local proxy MVP (2 weeks)

- Authenticated loopback SOCKS5 and HTTP CONNECT listeners.
- Gateway flow protocol, policy denial mapping, setup snippets, and diagnostics.
- Load, parser-fuzz, IPv4/IPv6-only, and abuse-limit tests.

Exit gate: SSH, PostgreSQL, HTTPS on `8443`, and WebSocket-over-CONNECT work for approved resources; every unknown host or disallowed port is denied at both client and gateway.

### Iteration 3 — public MVP hardening (2 weeks)

- Automatic update plan appropriate to the chosen distribution channel.
- Notarization pipeline, privacy screen, retention controls, alerts, crash reporting with payload redaction.
- Pilot runbook and support bundle.

Exit gate: 7-day pilot with no open-proxy finding, no sensitive-payload logging, and successful remote device revocation.

### Iteration 4 — packet-tunnel data-plane spike (2 weeks)

- Packet framing, packet edge, overlay address mapping, policy enforcement, MTU tests.
- Private split DNS and route-conflict detection.
- Measurements comparing TLS/TCP framing with a QUIC prototype; choose based on evidence.

Exit gate: one approved TCP service survives ordinary use through `packetFlow`; unrelated traffic demonstrably stays on the normal interface.

### Iteration 5 — full system client (4–6 weeks)

- Production provider lifecycle, reconnection, policy refresh, DNS, IPv4/IPv6, metrics, and revocation.
- Managed MDM/per-app design document, but no unmanaged “per-app” claim.

Exit gate: all full-client acceptance criteria below pass on supported macOS versions and clean installations.

### Iteration 6 — production and store readiness (3 weeks)

- High-availability tunnel edge, staged rollout/rollback, disaster recovery, key rotation, App Review package, and region controls.

Exit gate: operational readiness review, threat-model review, external security test, and signed launch approval.

## 8. Acceptance criteria

### MVP

- Login uses the system browser; authorization-code, state, nonce, and PKCE validation failures are rejected.
- The private device key is non-exportable when Secure Enclave is available and never leaves Keychain otherwise.
- SOCKS5 and CONNECT listeners are reachable only via loopback and require rotating authentication.
- SOCKS5 domain, IPv4, and IPv6 connect modes pass; unsupported commands fail cleanly.
- HTTP CONNECT handles fragmented headers and caps header size/time; non-CONNECT requests are rejected.
- An approved `host:port` succeeds; the same host on a denied port fails with a stable request ID.
- Unknown, expired, revoked, and ambiguous destinations fail closed. Loopback or private targets require an explicit owned-resource policy; link-local and cloud-metadata endpoints are never valid resources.
- App quit/sign-out stops listeners; no background process remains unexpectedly.
- 100 concurrent flows and configured bandwidth/idle limits behave predictably without UI hangs.
- No packet/body/token/credential content appears in application, gateway, crash, or analytics logs.

### Full client

- Only configured overlay prefixes/approved CIDRs appear in tunnel routes; unrelated browser traffic stays outside the tunnel.
- Private DNS is used only for configured match domains; the empty default domain is absent.
- HTTPS keeps the original hostname and passes normal certificate validation.
- The provider does not report connected until transport, policy, and `setTunnelNetworkSettings` all succeed.
- Host-app termination does not stop a healthy tunnel or its policy enforcement.
- Sleep/wake and Wi-Fi/Ethernet changes reconnect within the agreed service target and never route authorized destinations directly during reassertion.
- A revoked device loses new and existing access within the revocation service target.
- Route overlap is surfaced before activation; the client never silently captures a conflicting LAN.
- IPv4-only, IPv6-only, dual-stack, captive-portal, and gateway-address-change scenarios have explicit tests.
- Clean install, upgrade, rollback, uninstall, and remove-device flows leave no active configuration or usable credential beyond the documented behavior.

## 9. Apple feasibility notes and official sources

The following findings were checked against current Apple documentation on 2026-08-13:

- [`NWListener`](https://developer.apple.com/documentation/network/nwlistener) is the supported Network.framework API for accepting local TCP connections.
- [App Sandbox network server entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.network.server) is required for a sandboxed app to listen for incoming network connections; [Apple's sandbox setup guide](https://developer.apple.com/documentation/xcode/configuring-the-macos-app-sandbox) confirms incoming/outgoing network capabilities and that App Sandbox is required for the Mac App Store.
- [`ASWebAuthenticationSession`](https://developer.apple.com/documentation/authenticationservices/aswebauthenticationsession) is Apple's system flow for authenticating with a web service on macOS.
- [`SecKeyCreateRandomKey`](https://developer.apple.com/documentation/security/seckeycreaterandomkey%28_%3A_%3A%29) creates a key pair, and [Protecting keys with the Secure Enclave](https://developer.apple.com/documentation/security/protecting-keys-with-the-secure-enclave) documents hardware support, P-256 constraints, and non-exportable operation.
- [`NEPacketTunnelProvider`](https://developer.apple.com/documentation/networkextension/nepackettunnelprovider) provides the virtual interface and requires Network Extension entitlement; [`startTunnel`](https://developer.apple.com/documentation/networkextension/nepackettunnelprovider/starttunnel%28options%3Acompletionhandler%3A%29) must apply network settings before signaling successful startup.
- [`NEPacketTunnelFlow`](https://developer.apple.com/documentation/networkextension/nepackettunnelflow) is the read/write API for IP packets on the virtual interface.
- [Routing your VPN network traffic](https://developer.apple.com/documentation/networkextension/routing-your-vpn-network-traffic) documents included/excluded routes, full-route behavior, `enforceRoutes`, and the MDM/per-app model.
- [`NEDNSSettings.matchDomains`](https://developer.apple.com/documentation/networkextension/nednssettings/matchdomains) documents split DNS and the empty-string default-domain behavior.
- [`NETunnelProviderManager`](https://developer.apple.com/documentation/networkextension/netunnelprovidermanager) documents configuration ownership and states that production per-app VPN requires MDM.
- [`NEProvider.sleep`](https://developer.apple.com/documentation/networkextension/neprovider/sleep%28completionhandler%3A%29) and [`NEProvider.wake`](https://developer.apple.com/documentation/networkextension/neprovider/wake%28%29) are the provider lifecycle hooks for system sleep/wake.
- [Network Extensions entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.developer.networking.networkextension) lists `packet-tunnel-provider` and Developer ID `packet-tunnel-provider-systemextension` and explains App Store versus Developer ID setup.
- [App Review Guideline 5.4](https://developer.apple.com/app-store/review/guidelines/#vpn-apps) sets the organization, disclosure, data-use, privacy-policy, local-law, and licensing requirements for VPN apps.
- [Preparing a macOS app for distribution](https://developer.apple.com/documentation/xcode/preparing-your-app-for-distribution) distinguishes App Store sandboxing from Developer ID Hardened Runtime, and [Apple's notarization guide](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) covers Developer ID notarization and stapling.

Apple APIs, entitlement policy, review guidance, and regional rules can change. Repeat this check before the entitlement spike, public beta, and every store submission.
