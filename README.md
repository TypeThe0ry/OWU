# Permit

Permit is a free, no-account web access gateway for operator-registered public resources. The user-facing product is deliberately small: enter one HTTP or HTTPS address and visit it. There is no login, registration form, or authorization checkbox.

The gateway is deliberately not an anonymous open proxy. A submitted address must exactly match a public resource registered by the operator. Unknown origins, wrong ports, IP literals, private or metadata addresses, cross-origin redirects, `CONNECT`, and `TRACE` fail closed.

## Local demo

Prerequisites:

- Docker Desktop with Compose
- PowerShell 7 for the Windows E2E script

Start the complete demo:

```powershell
docker compose up --build -d
```

Open [http://localhost:3000](http://localhost:3000) and enter:

```text
http://demo-target:9000
```

That exact origin is registered only when `PERMIT_DEMO_MODE=true`. It is reachable through the gateway even though the target container is not published to the host. Other destinations remain denied.

Run the end-to-end acceptance test:

```powershell
.\tests\e2e-demo.ps1
```

Stop the demo:

```powershell
docker compose down
```

## Validation

Web build and tests:

```powershell
npm test
npx eslint app tests
```

Go gateway tests without a host Go installation:

```powershell
docker run --rm -v "${PWD}/gateway:/src" -w /src golang:1.23-alpine go test ./...
docker run --rm -v "${PWD}/gateway:/src" -w /src golang:1.23-alpine go vet ./...
```

Portable Swift core tests without a host Swift installation:

```powershell
docker run --rm -v "${PWD}/macos:/workspace" -w /workspace swift:5.10-jammy swift test --parallel
```

Apple-specific SwiftUI, Security.framework, Network.framework, signing, and entitlement paths still require Xcode 15.3+ on macOS.

## Repository map

- `app/`: single-input English web interface and same-origin access-check BFF.
- `gateway/`: Go control endpoint, one-time launch, resource session, HTTP(S)/WebSocket data plane, SSRF policy, audit, and limits.
- `macos/`: SwiftUI/SwiftPM client foundation and portable policy tests.
- `demo-target/`: deterministic internal fixture registered only by demo mode.
- `compose.yaml`: complete local demo topology.
- `tests/e2e-demo.ps1`: acceptance flow and denial checks.
- `docs/`: product, threat-model, gateway, and macOS planning documents.
- `PROGRESS.md`: durable goal checkpoints and evidence.

## Production boundary

Demo mode is not a production configuration. Before public deployment:

- turn off `PERMIT_DEMO_MODE`;
- use a random secret from a secrets manager;
- register only verified public origins through `PERMIT_PUBLIC_RESOURCES_JSON` or a durable control plane;
- terminate TLS at the public web and gateway origins;
- keep private resources behind an owner-operated outbound connector rather than relaxing SSRF rules;
- add durable distributed rate limits, storage, revocation, abuse handling, monitoring, and security review.

See [gateway/README.md](gateway/README.md) and [macos/README.md](macos/README.md) for component details.
