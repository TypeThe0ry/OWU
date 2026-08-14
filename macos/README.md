# OWU for macOS

This directory now contains the first working OWU macOS client implementation.
It creates loopback-only TCP listeners and carries each byte stream over an
authenticated `wss://` connection to one fixed server resource.

Gateway connections use an ordered port failover plan:

```text
443 (primary) -> 80 -> 8080 -> 8443 -> 9443 -> user-configured additional ports
```

Every candidate uses **WSS/TLS**, even on ports 80 and 8080. The app probes the
candidates in order and does not consume local SSH/Minecraft bytes until a TLS
and WebSocket handshake succeeds. Browser Basic Auth and `X-OWU-Tunnel-Key`
remain separate headers on every attempt and never appear in the URL. The UI
starts with 8443 and 9443 as recommended additional fallbacks; that list can be
replaced with comma-, semicolon-, or space-separated ports in the Gateway card.
Tunnel handshake redirects are rejected rather than forwarding credentials.

Default listeners:

| Resource ID | Local address | Example |
|---|---|---|
| `ssh` | `127.0.0.1:2222` | `ssh -p 2222 user@127.0.0.1` |
| `minecraft` | `127.0.0.1:25565` | Minecraft server `127.0.0.1:25565` |

The gateway resolves these IDs from `OWU_TCP_RESOURCES`; the Mac never sends an
arbitrary destination. The app stores its tunnel key in Keychain. Keep that key
independent from the Nginx Basic Auth password. A self-signed deployment is
supported by an explicit leaf-certificate SHA-256 pin.

## Build

Open `Package.swift` in Xcode 15.3 or later on macOS 14+, select the `OWU`
executable, and run. Portable core tests also run from the command line:

```sh
cd macos
swift test
swift run OWU
```

The app uses `Network.framework`, `URLSessionWebSocketTask`, CryptoKit, and
Keychain Services. It does not need a Network Extension for these app-specific
loopback listeners. A future system-wide, resource-only mode remains a separate
`NEPacketTunnelProvider` phase with Apple entitlements and physical-Mac testing.

## Server configuration

Copy `deploy/owu-proxy.env.example` to `/etc/owu/owu-proxy.env`, set a dedicated
`OWU_TUNNEL_KEY`, configure exact destinations, set mode `0600`, then restart
the Go proxy and reload Nginx. Keep the Nginx Basic Auth password separate from
the tunnel key; enter both in the app and store the server fingerprint only for
self-signed deployments.

For failover to work, the same OWU WSS route and certificate must be served on
443, 80, 8080, and every configured extra port. A conventional plaintext HTTP
redirect on port 80 is not a WSS listener, so that candidate will be skipped
and the client will continue with 8080. Never expose the authenticated tunnel
over clear-text `ws://` merely to make a fallback port respond.

## Verification

1. Start `ssh`; `lsof -nP -iTCP:2222 -sTCP:LISTEN` must show only `127.0.0.1`.
2. Run `ssh -p 2222 user@127.0.0.1` and verify the remote server fingerprint.
3. Start `minecraft`; connect to `127.0.0.1:25565`.
4. Enter a wrong browser password, tunnel key, or certificate pin; the connection must fail.
5. Change a resource ID to an unknown value; the server must return `404`.
6. Stop the 443 listener and confirm a new local connection reaches WSS on 80,
   then repeat for 8080 and an extra configured port.
