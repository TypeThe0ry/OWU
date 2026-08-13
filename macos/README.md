# OWU for macOS

This directory now contains the first working OWU macOS client implementation.
It creates loopback-only TCP listeners and carries each byte stream over an
authenticated `wss://` connection to one fixed server resource.

Default listeners:

| Resource ID | Local address | Example |
|---|---|---|
| `ssh` | `127.0.0.1:2222` | `ssh -p 2222 user@127.0.0.1` |
| `minecraft` | `127.0.0.1:25565` | Minecraft server `127.0.0.1:25565` |

The gateway resolves these IDs from `OWU_TCP_RESOURCES`; the Mac never sends an
arbitrary destination. Nginx Basic Auth and `X-OWU-Tunnel-Key` use the same
owner password, which the app stores in Keychain. A self-signed deployment is
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

Copy `deploy/owu-proxy.env.example` to `/etc/owu/owu-proxy.env`, replace the
password, configure exact destinations, set mode `0600`, then restart the Go
proxy and reload Nginx. Do not publish `/tunnel/` without both Nginx Basic Auth
and the tunnel-key check.

## Verification

1. Start `ssh`; `lsof -nP -iTCP:2222 -sTCP:LISTEN` must show only `127.0.0.1`.
2. Run `ssh -p 2222 user@127.0.0.1` and verify the remote server fingerprint.
3. Start `minecraft`; connect to `127.0.0.1:25565`.
4. Enter a wrong password or wrong certificate pin; the connection must fail.
5. Change a resource ID to an unknown value; the server must return `404`.
