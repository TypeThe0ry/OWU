# Open Website Unblocker (OWU)

OWU is an MIT-licensed self-hosted web proxy with a deliberately simple browser
flow: enter an HTTP or HTTPS address and open it through your server. The
browser remains on the OWU origin, while the destination sees the server's
network connection rather than the device's address. The repository includes a
Vinext/React UI, a Go data plane, an optional macOS client, GitHub/OIDC SSO
deployment templates, and crawler-friendly GEO metadata.

OWU also includes a macOS 14+ client for operator-defined TCP resources. It can
publish loopback-only local endpoints for services such as SSH and Minecraft
and carry those streams through the same HTTPS entry as authenticated WebSocket
traffic.

> OWU is an application-layer proxy, not a VPN or a promise that every website
> will work. Read [Compatibility and limitations](#compatibility-and-limitations)
> before deploying it.

## Highlights

- English, single-field web interface with light and dark liquid-glass themes.
- HTTP and HTTPS targets, including explicit non-standard ports.
- HTML, CSS, redirects, forms, cookies, Fetch, XHR, Beacon, EventSource,
  WebSocket, Worker, dynamic DOM URL attributes, and import-map rewriting.
- SPA-friendly virtual document paths, canonical resource fallback, and legacy
  HTML charset conversion.
- Browser-password gate at Nginx; edge credentials are stripped before requests
  reach destination websites.
- Public-address validation and DNS-pinned upstream dialing to reduce SSRF and
  DNS rebinding exposure.
- Per-origin target cookie prefixes so cookies are not sent to another target.
- Fixed-resource TCP tunnels; clients send a resource ID, never a destination
  host or port.
- SwiftUI macOS app with loopback listeners, Keychain storage, and optional
  certificate pinning.

## Architecture

```text
Browser -- HTTPS + Basic Auth --------+
                                      |
macOS loopback TCP -- WSS ------------+--> Nginx TLS edge
                                           :443  primary
                                           :8080 fallback
                                           :8443 additional fallback
                                           :9443 additional fallback
                                           :80   dedicated-address profile only
                                             |-- /             -> Vinext UI :3210
                                             |-- /browse/*     -> Go proxy :3211
                                             |-- /socket/*     -> Go WS proxy :3211
                                             `-- /tunnel/{id}  -> fixed TCP target
```

The web proxy encodes only the destination **origin** in the OWU route. The Go
data plane decodes it, validates the resolved address, pins the checked address
for the connection, rewrites compatible responses, and returns the result under
the OWU origin. The TCP path is separate: `OWU_TCP_RESOURCES` maps stable IDs to
exact server-side destinations.

## Repository layout

| Path | Purpose |
|---|---|
| `app/` | Vinext/React web interface and metadata |
| `gateway/cmd/owu-proxy/` | Production web, WebSocket, and fixed TCP data plane |
| `gateway/internal/webproxy/` | Proxy validation, rewriting, cookies, and tunnels |
| `gateway/internal/safety/` | Public-address validation and pinned dialing |
| `macos/` | SwiftUI client and portable core tests |
| `deploy/` | Nginx, systemd, and environment templates |
| `demo-target/`, `compose.yaml` | Earlier controlled gateway integration fixture |
| `tests/` | Web UI and integration checks |

## SSO and GEO at a glance

- **SSO:** optional GitHub OAuth or generic OIDC through `oauth2-proxy` and
  Nginx `auth_request`; credentials stay outside the repository and the Go
  proxy never receives provider tokens.
- **GEO:** canonical metadata, JSON-LD for OWU/Team TerraCat/source code,
  `robots.txt`, a minimal sitemap, and `llms.txt` keep project facts clear to
  search engines and generative assistants without exposing encoded target
  routes to crawlers.
- **Runbook:** see [`docs/SSO-GEO.md`](docs/SSO-GEO.md) for enablement and
  verification.

## Requirements

- Node.js 22.13+ and npm
- Go 1.23+
- Nginx with TLS for public deployment
- Linux with systemd for the checked-in service units
- macOS 14+ and Xcode 15.3+ to build the desktop app

## Local development

Install and run the web UI:

```sh
cp .env.example .env.local
npm ci
npm run dev
```

Build and run the production Go proxy in another terminal:

```sh
cd gateway
go build -o owu-proxy ./cmd/owu-proxy
OWU_PROXY_LISTEN_ADDR=127.0.0.1:3211 ./owu-proxy
```

Vinext listens on port `3000` in development; the Go proxy defaults to
`127.0.0.1:3211`. Nginx is the normal integration point because it applies the
password gate and routes the UI and proxy paths on one origin.

The production data-plane image builds `cmd/owu-proxy`:

```sh
docker build -t owu-proxy ./gateway
docker run --rm -p 127.0.0.1:3211:3211 \
  --env-file deploy/owu-proxy.env.example owu-proxy
```

`compose.yaml` remains a legacy controlled-resource fixture and explicitly uses
`gateway/Dockerfile.permit-demo`; it is not the public OWU deployment topology.

Run the checks:

```sh
npm run lint
npm test
npm run build

cd gateway
go test ./...
go vet ./...
```

## Configuration

### Web UI

| Variable | Default | Description |
|---|---|---|
| `NEXT_PUBLIC_OWU_BASE_URL` | `https://owu.example.com` | Canonical origin for social metadata; set it before building. |
| `NODE_ENV` | set by the service | Use `production` outside development. |
| `WRANGLER_LOG_PATH` | `/tmp/owu-wrangler.log` in systemd | Writable Vinext/Wrangler log path. |

### Go proxy

| Variable | Default | Description |
|---|---|---|
| `OWU_PROXY_LISTEN_ADDR` | `127.0.0.1:3211` | Data-plane listener; keep it behind Nginx. |
| `OWU_TUNNEL_KEY` | empty | Independent secret required by fixed-resource TCP tunnels; minimum 20 characters. |
| `OWU_TCP_RESOURCES` | empty | Comma-separated `id=host:port` mappings, for example `ssh=127.0.0.1:22,minecraft=mc.example.net:25565`. |
| `OWU_TRANSPORT_POOL_SIZE` | `64` | Maximum per-origin outbound HTTP transport pools retained for connection reuse. |
| `OWU_MEDIA_CACHE_MAX_AGE` | `15m` | Upper bound for an explicitly cacheable target media/static TTL; manifests are capped at 60 seconds. |

Copy `deploy/owu-proxy.env.example` to `/etc/owu/owu-proxy.env`, set mode
`0600`, and generate a tunnel key that is **different** from the Basic Auth
password:

```sh
sudo install -d -m 0750 /etc/owu
sudo install -m 0600 deploy/owu-proxy.env.example /etc/owu/owu-proxy.env
openssl rand -base64 48
sudoedit /etc/owu/owu-proxy.env
```

The checked-in web proxy intentionally has no environment switch that enables
private, loopback, link-local, or cloud-metadata browser destinations. TCP
resources are the explicit server-side path for private services.

Public listener ports are configured at Nginx, not in the Go environment file.
The checked-in profiles keep every TCP tunnel on TLS/WSS because both Basic
Auth and `X-OWU-Tunnel-Key` are credentials:

| Profile | `443` | `8080` | `8443`, `9443` | `80` |
|---|---|---|---|---|
| `deploy/nginx-owu.conf` | HTTPS/WSS primary | HTTPS/WSS fallback | HTTPS/WSS fallback | HTTP redirect only |
| `deploy/nginx-owu-dedicated-port80-tls.conf` | HTTPS/WSS primary | HTTPS/WSS fallback | HTTPS/WSS fallback | HTTPS/WSS fallback |

The dedicated profile is a standalone replacement, not an additional file to
load beside the shared-host profile. It requires a dedicated address with no
cleartext HTTP virtual hosts on port 80. A single address and port cannot serve
both ordinary HTTP and TLS/WSS through normal Nginx `http` virtual hosts.

## Production deployment

The templates assume these paths and ports:

- repository release: `/www/wwwroot/owu/current`
- Vinext UI: `127.0.0.1:3210`
- Go proxy: `127.0.0.1:3211`

The Nginx templates also contain the `__owu_origin_v1` virtual-document
dispatcher. Keep that server-level dispatcher and its
`@owu_virtual_document` location when adapting the templates: client-side
routers must see the destination pathname (for example `/en/g/level-devil`),
while reloads of that virtual path must still reach the Go proxy rather than
the OWU UI. The marker is routing state, not an authentication credential;
the browser-password gate still protects every request.

- primary public entry: Nginx TLS on `443`
- shared-host TLS fallback: Nginx TLS on `8080`
- additional TLS fallbacks: Nginx TLS on `8443` and `9443`
- optional dedicated-address TLS fallback: Nginx TLS on `80`

### 1. Build artifacts

```sh
npm ci --ignore-scripts --no-audit --no-fund
npm run lint
npm test
npm run build

cd gateway
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o ../owu-proxy ./cmd/owu-proxy
cd ..
```

Transfer the source/build bundle and `owu-proxy` to a versioned release
directory on the server. Never include populated environment files, password
files, private keys, logs, `work/`, or captured target pages. Committed
`.env.example` templates may be included only while they contain placeholders.

### 2. Install services

```sh
sudo install -m 0755 owu-proxy /usr/local/bin/owu-proxy
sudo install -m 0644 deploy/owu.service /etc/systemd/system/owu.service
sudo install -m 0644 deploy/owu-proxy.service /etc/systemd/system/owu-proxy.service
sudo systemctl daemon-reload
sudo systemctl enable --now owu owu-proxy
```

The web service expects Node and its dependencies under the paths in
`deploy/owu.service`. Adjust them if the server layout differs.

### 3. Configure TLS, Basic Auth, and Nginx

Replace `owu.example.com`, certificate paths, and service paths in the selected
Nginx profile. `deploy/nginx-owu.conf` is the shared-host default: port 80 keeps
serving an HTTP redirect, while 443, 8080, 8443, and 9443 carry HTTPS/WSS.
Create a unique browser password and install that profile:

```sh
sudo htpasswd -c /www/server/nginx/conf/owu.htpasswd owu
sudo install -m 0644 deploy/nginx-owu.conf /etc/nginx/conf.d/owu.conf
sudo nginx -t
sudo systemctl reload nginx
```

Use `deploy/nginx-owu-dedicated-port80-tls.conf` instead only on an address
dedicated to OWU. Do not load both profiles. This makes every listed client URL
a TLS endpoint:

```text
wss://owu.example.com/tunnel/{resource}       # 443, primary
wss://owu.example.com:80/tunnel/{resource}    # 80, fallback
wss://owu.example.com:8080/tunnel/{resource}  # 8080, fallback
wss://owu.example.com:8443/tunnel/{resource}  # additional fallback
wss://owu.example.com:9443/tunnel/{resource}  # additional fallback
```

On the shared-host profile, the safe endpoint set is 443, 8080, 8443, and 9443;
port 80 is only `http://` -> `https://` browser redirection and must not be
configured as a `ws://` tunnel. Add any further port with another
`listen ... ssl` line and use an explicit `wss://host:port` URL. Open each
selected TCP port in the host firewall and the cloud security group.

Use a trusted certificate for the hostname when possible. The Nginx template
disables access logs because proxy URLs may expose target hosts and query
strings. If request metrics are required, define a target-free log format that
does not record `$request_uri`, `Referer`, `Authorization`, cookies, or request
bodies.

#### Douyin and Bilibili media acceleration

Both Nginx profiles include a bounded `owu_media` edge cache and connection
tuning intended for Douyin/Bilibili video, image, font, manifest, CSS,
JavaScript, and WASM resources. Rewritten HTML remains uncached; public CSS is
cached only after OWU finishes its deterministic URL rewrite. The cache
primitives are deliberately in
the HTTP scope at the top of each profile; keep the selected profile under an
`http { ... }` include such as `conf.d`, and do not paste only its `server`
block. Create the cache directory for the configured Nginx worker user before
the first reload (the user is commonly `nginx`, `www-data`, or `www`):

```sh
sudo install -d -o nginx -g nginx -m 0750 /var/cache/nginx/owu-media
sudo nginx -t
sudo systemctl reload nginx
```

Adjust the owner in that command to match the `user` directive reported by
`sudo nginx -T`. The checked-in default is a 4 GiB disk cache with a 4 GiB
free-space floor, 64 MiB of key metadata, and 24 hours of inactivity retention.
Cache files and metadata are private session data: keep the directory readable
only by the Nginx worker and administrators. Change `max_size`, `min_free`, and
`inactive` in the selected profile when storage is constrained. No
`http_slice_module` dependency is required.

The cache trust contract is intentionally narrow:

- only canonical `/browse/<origin-token>/...` `GET`/`HEAD` resources are
  eligible;
- the Go proxy must emit `X-OWU-Cache: public-media` and a positive
  `X-Accel-Expires` after validating the original target response is public;
- HTML/API responses, virtual documents, UI files, Referer fallbacks,
  WebSockets, and TCP tunnels are explicitly uncached;
- requests with a Cookie for the current target, client `no-cache`, Range, or
  an HTTP validator bypass storage; unrelated OWU cookies are isolated by the
  complete Cookie header in the cache key. Responses with `Set-Cookie`, an
  attachment disposition, an unapproved MIME type, or no explicit Go marker
  are not stored;
- Range and validator requests are internally routed to a location with
  `proxy_cache off`, preserve `Range`/`If-Range`, and stream a destination `206`
  without placing a partial object in the cache;
- the edge Basic `Authorization` value is consumed by Nginx and stripped before
  Go. Destination authorization is never forwarded through that header, and
  `$remote_user` remains part of every cache key for principal isolation.

HTTP/2 handles concurrent page assets on all TLS listener ports, while the UI
and Go upstream pools reuse HTTP/1.1 connections. Cache locking collapses a
first-request stampede; background revalidation and bounded stale-on-error
serve only objects that already passed the public-media contract. Nginx honors
`X-Accel-Buffering: no` from Go for SSE and partial media, so long-lived or
range traffic remains streaming rather than waiting for a temporary file.

For a canonical anonymous media URL that Go has marked cacheable, the first
request should be `MISS` and the second `HIT`. A byte range must remain a 206
with exactly the requested length:

```sh
MEDIA_URL='https://owu.example.com/browse/ORIGIN_TOKEN/path/to/media.mp4'
curl -skI -u owu "$MEDIA_URL" | grep -i '^X-OWU-Cache-Status:'
curl -skI -u owu "$MEDIA_URL" | grep -i '^X-OWU-Cache-Status:'

curl -sku owu -H 'Range: bytes=0-1023' \
  -D /tmp/owu-range.headers -o /tmp/owu-range.bin "$MEDIA_URL"
grep -Eai '^(HTTP/|Content-Range:|X-OWU-Cache-Status:)' /tmp/owu-range.headers
test "$(wc -c </tmp/owu-range.bin)" -eq 1024
```

Expected cache states are `MISS` then `HIT`; the range response is
`206 Partial Content` with `X-OWU-Cache-Status: BYPASS`. A signed media URL is
keyed with its full raw query. Login-protected feeds intentionally bypass the
shared cache once target cookies are present. DRM, device-bound playback,
short-lived signatures, and upstream bot challenges remain destination
compatibility constraints rather than cache misses.

### 4. Verify and roll back

```sh
curl -fsS http://127.0.0.1:3211/healthz
curl -fsS http://127.0.0.1:3210/ >/dev/null
curl -u owu https://owu.example.com/ >/dev/null
curl -u owu https://owu.example.com:8080/ >/dev/null
curl -u owu https://owu.example.com:8443/ >/dev/null
curl -u owu https://owu.example.com:9443/ >/dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' http://owu.example.com/)" = 308
systemctl --no-pager --full status owu owu-proxy
```

For the dedicated-address profile, verify the port-80 TLS handshake explicitly
with `curl -u owu https://owu.example.com:80/`. A plain
`http://owu.example.com:80/` request is intentionally invalid in that profile.

Deploy releases to versioned directories and make `current` a symlink. To roll
back, point `current` to the previous release, restore the previous proxy binary
and configuration if their interfaces changed, then restart both services and
reload Nginx. Validate `/healthz`, the home page, one HTTP page, one HTTPS page,
one WebSocket fixture, and every configured TCP resource after either upgrade
or rollback.

## Protocol support

### Browser HTTP and HTTPS

Enter, for example, `https://example.com`, `http://example.com`, or
`https://example.com:8443/path`. If the scheme is omitted, the UI adds
`https://`. HTML navigation and common runtime URL APIs are rewritten to
`/browse/{encoded-origin}/...`; WebSocket constructors use `/socket/...`.

The public-address policy rejects destinations that resolve to loopback,
private, link-local, multicast, documentation, special-use, or metadata
addresses. This is independent of the Nginx password.

### WebSocket

Proxied pages can create `ws:` or `wss:` connections. OWU maps them onto its
own `ws(s)://.../socket/{encoded-origin}/...` route and forwards the Upgrade.
Test application-specific subprotocols and authentication flows before relying
on them.

### Fixed TCP, SSH, and Minecraft

The macOS app exposes only the resource IDs compiled/configured in its preset
list. The server resolves each ID from `OWU_TCP_RESOURCES`:

```dotenv
OWU_TCP_RESOURCES=ssh=127.0.0.1:22,minecraft=mc.example.net:25565
```

After connecting in the app:

```sh
ssh -p 2222 user@127.0.0.1
```

For Minecraft, add `127.0.0.1:25565` as the server in the client. These are raw
TCP byte streams carried inside WSS; UDP is not implemented. Arbitrary client-
chosen CONNECT or SOCKS destinations are not exposed.

An SSH banner smoke test is included for deployments:

```sh
cd gateway
OWU_SMOKE_URL=wss://owu.example.com/tunnel/ssh \
OWU_SMOKE_USERNAME=owu \
OWU_SMOKE_PASSWORD='<browser-password>' \
OWU_SMOKE_TUNNEL_KEY='<independent-tunnel-key>' \
go run ./cmd/owu-tunnel-smoke
```

Repeat the same smoke test with ports 8080, 8443, and 9443. With the
dedicated-address profile, also test
`wss://owu.example.com:80/tunnel/ssh`. A port is not considered a usable
fallback until the TLS handshake, Basic Auth, tunnel-key check, WebSocket
Upgrade, and upstream banner/echo all succeed through that exact URL.

The tunnel key is a separate application credential sent as
`X-OWU-Tunnel-Key` during WebSocket setup. It must match `OWU_TUNNEL_KEY`, remain
in Keychain on macOS, and must not be the same value as the edge Basic Auth
password.

## macOS build and verification

Open `macos/Package.swift` in Xcode 15.3+ on macOS 14+, select the `OWU`
executable, and run it. Command-line builds and tests:

```sh
cd macos
swift test
swift run OWU
```

Set the OWU server URL, Basic Auth username if requested by the deployment,
independent tunnel key, optional SHA-256 leaf-certificate pin, and the ordered
TLS endpoints in the app. Keep 443 first, then 8080, 8443, and 9443. Add port 80
only when the dedicated-address TLS profile is actually deployed.
Verify that listeners bind only to loopback:

```sh
lsof -nP -iTCP:2222 -sTCP:LISTEN
lsof -nP -iTCP:25565 -sTCP:LISTEN
```

See `macos/README.md` and `docs/owu-macos-app-plan.md` for implementation and
future `NEPacketTunnelProvider` planning.

## Compatibility and limitations

| Capability | Status | Notes |
|---|---|---|
| HTTP/1.1 and HTTPS websites | Supported | Any public DNS destination and valid port; upstream TLS requires TLS 1.2+. |
| Redirects, forms, cookies | Supported | Redirects and cookies are rewritten; cookies are isolated by target-origin prefix. |
| HTML/CSS assets and lazy-load attributes | Supported | Relative, root-relative, comma-bearing `srcset` image URLs, common `data-*`, CSS `url()`, and import-map references are covered. |
| SPA navigation | Best effort | Virtual document URLs preserve the destination pathname for React/Vue/Wouter-style routers; history navigation, direct reload, canonical fallback, and idempotent resource rewriting are covered. |
| Fetch, XHR, Beacon, EventSource | Best effort | Common constructors are wrapped; code that captures pristine intrinsics or constructs URLs in unsupported native paths may escape rewriting and fail closed. |
| WebSocket | Best effort | Upgrade and common subprotocol use work; application-specific Origin checks may reject OWU. |
| Service Worker / PWA offline mode | Disabled | All proxied sites share the OWU browser origin, so registration is blocked and existing registrations are removed to prevent one target controlling another. |
| Browser storage | Shared-origin limitation | `localStorage`, IndexedDB, Cache Storage, permissions, and some browser state are not fully virtualized per destination. Do not treat them as isolated browser profiles. |
| OAuth, SSO, CAPTCHA, passkeys | Limited | Redirect allowlists, popup/opener checks, third-party cookie policy, device binding, and origin assertions can reject the proxy origin. |
| DRM, WebAuthn, certificate/device binding | Usually incompatible | These mechanisms intentionally bind to an origin, device, certificate, or protected media path OWU cannot impersonate. |
| Destination HTTP Authorization | Limited | OWU's public edge uses Basic Auth and strips `Authorization` before upstream forwarding, so target sites that require an `Authorization` request header need a separate design. URL-embedded credentials are rejected. |
| Frames and strict origin checks | Limited | OWU supplies a compatibility CSP, but frame ancestors, postMessage origin validation, COOP/COEP, CORS, and server-side Origin policy can still break complex apps. |
| Downloads, range requests, media | Best effort | Streaming and exact 206 ranges pass through. HLS URI lines/attributes and standard DASH URL surfaces are rewritten; DASH CDATA/vendor extensions, DRM, and very large rewritable bodies remain compatibility limits. |
| Raw TCP on macOS | Supported for fixed resources | SSH, Minecraft, databases, and similar TCP protocols work when mapped by the operator. No arbitrary host/port selection. |
| UDP and full-device VPN | Not implemented | A future Network Extension phase requires Apple entitlements, packet routing, DNS handling, and physical-Mac validation. |

No same-origin rewriting proxy can guarantee compatibility with every site.
When a site fails, capture the browser console and network errors against a
disposable account/fixture, add a regression test, and avoid logging credentials
or response bodies.

## Logging and privacy

- Nginx access logs are disabled in the template. Error logs remain enabled.
- The Go data plane logs startup and server-level errors, not request bodies,
  passwords, cookies, or target response bodies.
- Vinext/Wrangler logs go to the configured local log path.
- Target websites still observe the OWU server's public IP, headers that are
  forwarded, traffic timing, and account/application identifiers.
- OWU terminates TLS at Nginx and processes proxied response bodies to rewrite
  them; it is not end-to-end opaque to the server operator.
- Apply restrictive permissions, log rotation, short retention, and disk-
  encryption appropriate to the host. Never enable body logging on login or
  payment flows.

## GitHub releases

Before tagging a release:

1. update `CHANGELOG.md` and the version in `package.json`;
2. run all web, Go, and macOS checks from CI;
3. run a secret scan and inspect `git ls-files` for generated artifacts;
4. build the Linux `owu-proxy` binary with `-trimpath`;
5. create checksums, for example `sha256sum owu-proxy release.tar.gz`;
6. tag `vMAJOR.MINOR.PATCH` and attach the source archive, binary, checksums,
   compatibility notes, migration steps, and rollback instructions.

Do not attach `.env` files, certificate material, Basic Auth databases, server
logs, local `work/` artifacts, or macOS Keychain data. Code signing and
notarization should be added before distributing the macOS app outside Xcode.

## Contributing and security

See `CONTRIBUTING.md` for development rules and `SECURITY.md` for private
vulnerability reporting. OWU is released under the MIT License in `LICENSE`.
