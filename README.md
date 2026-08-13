# Open Website Unblocker (OWU)

OWU is a browser-password protected personal web proxy. The page stays focused on one action: enter an HTTP or HTTPS address and load it through the OWU server.

OWU rewrites common HTML, CSS, navigation, form, redirect, fetch, XHR, EventSource, and WebSocket references so browsing remains on the OWU origin. It is not a VPN and does not guarantee compatibility with strict CSP, OAuth, CAPTCHA, DRM, Service Worker, or complex browser-origin assumptions.

## Product

- One English URL field and one primary action.
- Adds `https://` when the scheme is omitted.
- Accepts `http:` and `https:` destinations, including explicit ports.
- Responsive liquid-glass presentation with explicit light and dark themes.
- Nginx Basic Auth protects the entire public entry before the app loads.
- The proxy dials only public Internet addresses and pins the checked DNS result for each connection.
- Target cookies are isolated by upstream origin, and OWU credentials are never forwarded upstream.

## Local development

```powershell
npm install
npm run dev
```

The web UI runs on port `3000`. Build the Go proxy with `go build ./cmd/owu-proxy` from `gateway/`, then run it on `127.0.0.1:3211`. Production Nginx routes `/browse/` and `/socket/` to the proxy.

## Validation

```powershell
npm test
npx eslint app tests
docker run --rm -v "${PWD}/gateway:/src" -w /src golang:1.23-alpine go test ./...
git diff --check
```

## Production deployment

The checked-in deployment templates run Vinext on `127.0.0.1:3210` and the Go proxy on `127.0.0.1:3211` behind Nginx:

- `deploy/owu.service`: hardened Vinext systemd unit.
- `deploy/owu-proxy.service`: hardened Go proxy systemd unit.
- `deploy/nginx-owu.conf`: HTTPS, Basic Auth, web UI, proxy, and WebSocket routing.

The public Nginx entry redirects HTTP to HTTPS and uses an IP-address certificate until a domain is configured. The browser will warn about the self-signed certificate. Do not reuse the server root password for Basic Auth.

## macOS app

The macOS plan remains a separate future surface. The current deliverable is the browser-based personal proxy.
