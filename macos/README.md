# Permit for macOS

This directory contains a SwiftUI macOS 14+ application scaffold and a portable,
testable policy core for Permit. The default product flow does not require an
account: it can list and connect only to resources that the service has
pre-registered, verified, and marked `public`.

This is not an unrestricted URL, SOCKS, or CONNECT proxy. A typed destination is
only a lookup key. It must match an exact endpoint in a fresh, signature-verified
catalog, and the server must issue a short-lived grant for that resource before a
gateway flow can open. The gateway request contains `resource_id`, port, grant,
device proof, and a trace ID; it has no raw target override field.

## Current status

The repository is an intentionally fail-closed implementation scaffold:

- `PermitCore` implements destination parsing, public-resource visibility,
  exact endpoint matching, catalog freshness checks, route-grant binding,
  loopback listener limits, and a split-tunnel adapter contract.
- `PermitMacPlatform` provides a Keychain/Secure Enclave device-key adapter and
  a loopback-only `Network.framework` listener scaffold.
- `PermitApp` provides English SwiftUI screens for Home, Proxy, Resources,
  Local Security, and Settings.
- The included listener is named `DenyOnlyLoopbackListenerService` and closes
  every connection. It exists to prove loopback binding and lifecycle structure;
  it must not be presented as a working proxy.
- The UI keeps the proxy start action disabled until authenticated SOCKS5 and
  HTTP CONNECT parsers, rotating per-launch credentials, catalog sync, grant
  resolution, and the TLS gateway transport are implemented and tested.

There is no fake sign-in, fake resource, fake gateway, unrestricted fallback, or
development switch that bypasses policy.

## Layout

```text
macos/
├── Package.swift
├── Sources/
│   ├── PermitCore/          portable models, policy, and adapter contracts
│   ├── PermitMacPlatform/   Security.framework and Network.framework adapters
│   └── PermitApp/           macOS 14+ SwiftUI application shell
├── Tests/PermitCoreTests/   pure Swift/XCTest policy and parser coverage
└── scripts/                 local verification helpers
```

Open `Package.swift` in Xcode 15.3 or newer. The Swift package is deliberately
dependency-free. For command-line verification on a Mac with Swift 5.10+:

```sh
cd macos
./scripts/verify.sh
```

PowerShell environments with Swift installed can run:

```powershell
Set-Location macos
.\scripts\verify.ps1
```

The initial scaffold was assembled on Windows, where Swift and Xcode were not
available. CI or a Mac developer must run `swift test`, then build and inspect the
SwiftUI executable in Xcode before treating the skeleton as compile-verified.

## Public access flow

1. Fetch a signed public catalog over the control-plane TLS endpoint.
2. Verify its signature and expiry before replacing the last trusted snapshot.
3. Parse a user-entered URL or `host:port` and match exact protocol, host, and port.
4. Reject resources not marked `public`, unknown endpoints, ambiguous entries,
   stale catalogs, and unsafe IP literals.
5. Ask the server for a short-lived grant using only the matched `resource_id`
   and catalog endpoint.
6. Bind the grant to the anonymous installation key and gateway audience.
7. Open the gateway flow using `GatewayOpenRequest`. The gateway independently
   resolves the resource from its own catalog and repeats authorization.

No login is required in this flow. The installation key is a local anti-abuse and
grant-binding primitive, not a user account. Restricted resources remain modeled
for a possible future authenticated mode but are hidden and denied by the default
policy.

## Local proxy integration gate

Before replacing the deny-only listener, the implementation must provide all of
the following in one reviewed change:

- separate IPv4 and IPv6 loopback listeners; never `0.0.0.0` or a LAN address;
- a random 32-byte per-launch credential that is kept in memory and rotated;
- bounded, fragmented-input SOCKS5 and HTTP CONNECT state machines;
- SOCKS5 `CONNECT` only; reject `BIND` and `UDP ASSOCIATE`;
- HTTP `CONNECT` only; reject forward-proxy methods and absolute-form requests;
- handshake byte/time limits, idle limits, flow limits, and bandwidth accounting;
- an exact catalog match and fresh server grant before opening a TLS 1.3 gateway
  stream on port 443;
- gateway APIs that accept a `resource_id` but never an unchecked host supplied by
  the local protocol client;
- sign-out is not part of the default public flow, but app quit, key revocation,
  and catalog revocation must stop listeners and clear in-memory credentials.

## Network Extension boundary

`SystemTunnelManaging` and `ApprovedTunnelConfiguration` are adapter boundaries
only. The scaffold does not include a packet-tunnel extension target, VPN
configuration, or entitlement. It rejects default routes (`0.0.0.0/0`, `::/0`)
and the empty default DNS match domain so a future implementation starts from
resource-only routing.

Do not claim Network Extension support until the team has:

- an Apple organization account and approved Network Extension capability;
- matching App IDs, provisioning profiles, App Group, and Keychain access groups;
- a real `NEPacketTunnelProvider` target built and signed on macOS;
- physical-Mac lifecycle tests for sleep/wake, host-app exit, route overlap,
  revocation, IPv4/IPv6, and clean uninstall;
- reviewed privacy disclosures, distribution requirements, and regional rules.

The local-proxy MVP does not itself require a Network Extension entitlement. A
sandboxed distribution does require outgoing network client and incoming network
server capabilities even when listeners bind only to loopback.

## Security risks still open

- Catalog signature verification and rollback protection are not implemented.
- Opaque grant signature verification is represented by an interface, not a
  cryptographic implementation.
- The Keychain adapter needs compilation and behavior tests on both Apple silicon
  and supported Intel Macs; Secure Enclave availability and access-control flags
  must be validated under lock, sleep, and wake.
- SOCKS5/CONNECT parsers and the gateway transport are intentionally absent.
- IDNA/UTS #46 hostname canonicalization needs a reviewed implementation shared
  with the server; Foundation parsing alone is not the production policy key.
- Public catalog access still needs rate limits, quotas, revocation, audit metadata,
  abuse response, and clear retention rules even when end users are not charged.
- Private and link-local targets must not be enabled by relaxing public gateway
  SSRF rules. Private resources require an owner-operated outbound connector with
  its own local resource allowlist.

Do not log route grants, proxy credentials, URLs beyond approved resource identity,
request bodies, passwords, or packet payloads.
