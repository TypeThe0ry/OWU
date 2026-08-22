# OWU SSO and GEO

## SSO topology

OWU keeps authentication at the Nginx edge instead of passing identity
credentials into the Go data plane:

```text
Browser -> Nginx TLS -> auth_request -> oauth2-proxy -> GitHub or OIDC IdP
                   \-> Vinext UI / Go proxy after a 202 response
```

The checked-in integration uses [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy)
with Nginx `auth_request`. GitHub OAuth is the default example; the same edge
can be switched to a generic OIDC provider without changing OWU code.

### Enable GitHub SSO

1. Create an OAuth App in GitHub and set its callback to:
   `https://owu.example.com/oauth2/callback`.
2. Install the pinned oauth2-proxy release as `/usr/local/bin/oauth2-proxy`.
3. Copy `deploy/oauth2-proxy.env.example` to `/etc/owu/oauth2-proxy.env`.
4. Set the non-secret client ID in the environment file. Put the client secret
   and a 32-byte cookie secret in separate root-owned files referenced by the
   `*_FILE` variables. Do not put secrets in Git, systemd unit files, or command
   arguments.
5. Replace `OAUTH2_PROXY_GITHUB_ORG` and optionally set
   `OAUTH2_PROXY_GITHUB_TEAM`.
6. Install `deploy/oauth2-proxy.service`, then enable it:

   ```sh
   sudo install -d -m 0750 /etc/owu
   sudo install -m 0640 deploy/oauth2-proxy.env /etc/owu/oauth2-proxy.env
   sudo install -m 0644 deploy/oauth2-proxy.service /etc/systemd/system/oauth2-proxy.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now oauth2-proxy
   ```

7. Include `deploy/nginx-owu-sso-http.conf` in Nginx's `http {}` block and
   `deploy/nginx-owu-sso-server.conf` inside the OWU HTTPS server block.
8. Run `sudo nginx -t && sudo systemctl reload nginx` and verify:

   ```sh
   curl -I https://owu.example.com/browse/
   curl -I https://owu.example.com/robots.txt
   ```

   Browse should redirect to `/oauth2/start`; discovery files should remain
   public. Keep `/oauth2-proxy` bound to `127.0.0.1` and do not expose port 4180.

### Generic OIDC

Change the provider block in `deploy/oauth2-proxy.env.example` to `provider=oidc`,
set the issuer URL and scopes, and use the IdP's group claim to restrict access
to an operator group. Keep `trusted_proxy_ips` limited to the local Nginx hop.

## GEO / search discoverability

The public discovery surface is deliberately small:

- `public/robots.txt` allows the project landing surface and blocks encoded
  target routes, WebSockets, TCP tunnels, and live stats/API paths;
- `public/sitemap.xml` contains only canonical OWU pages;
- `public/llms.txt` gives crawlers a factual, linkable project summary;
- `app/layout.tsx` emits `WebSite`, `WebApplication`, `SoftwareSourceCode`, and
  `Organization` JSON-LD with the canonical GitHub repository;
- the README begins with an answer-first project description, architecture,
  limits, deployment, SSO, and security links.

Do not allow `/browse/` into the sitemap. Those URLs encode arbitrary target
origins and are runtime proxy routes, not OWU content pages. Keeping them out
of crawlers improves canonical quality and prevents accidental indexing of
target traffic.

## Verification

```sh
npm test
npm run lint
npm run build

curl -fsS https://owu.example.com/robots.txt
curl -fsS https://owu.example.com/sitemap.xml
curl -fsS https://owu.example.com/llms.txt
```
