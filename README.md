# Open Website Unblocker (OWU)

OWU is a free, no-account website address launcher. The public page stays focused on one action: enter an HTTP or HTTPS address and open it directly in the browser.

OWU does not relay page traffic, rewrite third-party websites, operate a VPN, or bypass a network policy. A destination can still be unavailable because of DNS, the current network, regional restrictions, organizational policy, or the destination itself.

## Product

- One English URL field and one primary action.
- Adds `https://` when the scheme is omitted.
- Accepts only `http:` and `https:` destinations and rejects embedded credentials.
- No login, registration, authorization catalog, history sync, or analytics.
- Responsive liquid-glass presentation with explicit light and dark themes.
- Direct browser navigation; submitted addresses are not sent to an OWU API.

## Local development

```powershell
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

## Validation

```powershell
npm test
npx eslint app tests
git diff --check
```

`npm test` builds the production bundle and runs the focused UI, URL-policy, and theme tests.

## Production deployment

The checked-in deployment templates run the Vinext server on `127.0.0.1:3210` behind Nginx:

- `deploy/owu.service`: hardened systemd unit.
- `deploy/nginx-owu.conf`: IP-host Nginx virtual host.

The deployed app contains no proxy or access-check endpoint. The older `gateway/`, `compose.yaml`, and `demo-target/` directories are retained as historical authorized-gateway prototypes and are not part of the OWU production deployment.

## macOS app

The current macOS direction is a lightweight SwiftUI address launcher with an embedded `WKWebView`, direct WebKit networking, local preferences, and no account or tunnel. See [docs/owu-macos-app-plan.md](docs/owu-macos-app-plan.md).
