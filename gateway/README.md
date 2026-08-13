# Permit authorized-access gateway

This directory contains the Go MVP control endpoint and HTTP/WebSocket data gateway for Permit. It is deliberately **not** a general-purpose proxy:

- A request can leave the gateway only when its normalized `(scheme, hostname, port)` exactly matches a server-configured resource.
- Anonymous access works only for resources explicitly configured with `public: true`.
- Unknown origins fail closed without a DNS lookup or outbound request.
- URL credentials, IP literals, unsafe address ranges, `CONNECT`, and `TRACE` are rejected.
- DNS is checked before ticket creation, when a one-use ticket is consumed, and again when each upstream connection is made. The connection is made to the checked IP, while TLS SNI and certificate verification remain bound to the configured hostname.

The in-memory ticket, session, limit, and resource stores are suitable for a single-instance MVP. A production rollout with multiple replicas must replace them with atomic shared storage and a durable policy source.

## API flow

`POST /v1/access/check` accepts:

```json
{"input_url":"https://service.example.com:8443/dashboard"}
```

The stable `decision` is one of `allowed`, `sign_in_required`, `resource_not_authorized`, `blocked`, `port_not_allowed`, or `rate_limited`. An allowed public resource also returns a 60-second, one-use `launch_url` under `/_launch/{ticket}`. `POST /v1/launches` provides the same behavior with a `201` response for compatibility with the architecture contract.

Opening the launch URL consumes the ticket, creates a resource-bound session, and redirects to the approved path. Production HTTPS uses the host-only `__Host-aa_session` cookie. Local HTTP demo mode uses `aa_demo_session`, because browsers cannot send a `Secure` cookie over ordinary HTTP.

The browser then requests paths on the gateway origin. The fixed origin stored in the session—not any request parameter—selects the upstream. Relative navigation, same-origin requests, forms, and WebSocket upgrades are supported. The gateway does not rewrite HTML or JavaScript bodies, and cross-origin upstream redirects are stopped with `CROSS_ORIGIN_REDIRECT`.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `PERMIT_LISTEN_ADDR` | `:8081` | HTTP listener behind the deployment TLS edge |
| `PERMIT_PUBLIC_BASE_URL` | `http://localhost:8081` | Bare public origin used in launch links |
| `PERMIT_SESSION_SECRET` | none | At least 32 bytes; mandatory outside demo mode |
| `PERMIT_PUBLIC_RESOURCES_JSON` | `[]` | Static, trusted resource registrations |
| `PERMIT_DEMO_MODE` | `false` | Enables the local fixture and development-only identity header |
| `PERMIT_DEMO_TARGET_ORIGIN` | `http://demo-target:9000` | The one exact private-network origin permitted in demo mode |

Example production resource configuration (shown formatted; provide it as one JSON environment value):

```json
[
  {
    "id": "res_lab_console",
    "public_id": "lab-console",
    "display_name": "Lab console",
    "origin": "https://lab.example.com:8443",
    "public": true,
    "allowed_path_prefixes": ["/"],
    "allowed_methods": ["GET", "HEAD", "POST"],
    "websocket_enabled": true
  }
]
```

This configuration is an administrator-owned allowlist and must not be populated directly from an anonymous form submission. A later resource-registration API must require control verification before it can activate an origin.

## Local demo

The repository-level Compose setup can run a target named `demo-target` on port `9000`. Start the gateway with:

```text
PERMIT_DEMO_MODE=true
PERMIT_PUBLIC_BASE_URL=http://localhost:8081
PERMIT_DEMO_TARGET_ORIGIN=http://demo-target:9000
```

Demo mode creates a public fixture resource for that exact origin. It may resolve to an RFC1918 container address. This narrow exception does not permit `127.0.0.1`, link-local/cloud-metadata addresses, a different private hostname, or another port. Never enable demo mode in production.

An access check is:

```bash
curl -sS -X POST http://localhost:8081/v1/access/check \
  -H 'Content-Type: application/json' \
  --data '{"input_url":"http://demo-target:9000/"}'
```

`X-Permit-Demo-User: demo-user` is honored only while `PERMIT_DEMO_MODE=true`, and exists solely for tests of a non-public resource. Public fixture access does not require it. The header is ignored in production.

## Build and test

With Go 1.23 or newer:

```bash
go test ./...
go build ./cmd/gateway
```

Or from the repository root with Docker:

```bash
docker run --rm -v "$PWD:/src" -w /src/gateway golang:1.23-alpine go test ./...
docker build -t permit-gateway ./gateway
```

Tests cover strict URL normalization, hostile URL forms, public/private grants, one-use tickets, private and metadata address denial, the exact demo exception, DNS rebinding between check and launch, connect-time IP pinning, header/session isolation, and upstream cookie filtering.

## Operational limits

- Access checks: 30 requests per client prefix per minute.
- Data requests: 120 requests per client prefix per minute.
- Concurrent connections: 32 per process and 8 per resource.
- Request body: 16 MiB. Response body: 64 MiB.
- Upstream connect/TLS: 10 seconds. Response headers: 20 seconds.
- Ticket: 60 seconds and one use. Session: 30 minutes by default.

Audit output is newline-delimited JSON on stdout. It records decisions, resource and actor identifiers, method, a SHA-256 path digest, truncated client network, status, byte counts, and duration. It does not record query strings, fragments, cookies, authorization headers, bodies, tickets, or page content.
