# Current product decision

This document supersedes account and visible-authorization flows described in earlier planning notes.

## User experience

- The English home screen contains one website-address input and one visit action.
- The user does not log in, register, or check an authorization confirmation.
- A valid, registered public resource opens immediately after the server-side check creates a one-time launch.
- Errors stay inline and explain that an unavailable address cannot be opened.

## Access model

No account does not mean no policy. The operator maintains a catalog of exact public origins:

```text
(scheme, canonical hostname, effective port) -> public resource policy
```

Anonymous access is allowed only when the submitted origin exactly matches a catalog entry marked `public`, the path and method are permitted, and connect-time DNS/IP safety checks pass. The browser cannot add resources or override the upstream target.

Unknown origins, unsafe IPs, wrong ports, stale grants, expired tickets, and targets outside the catalog fail closed. There is no direct-browser fallback and no general `CONNECT`, SOCKS, or arbitrary fetch endpoint.

## Why this shape

This preserves the requested one-field experience while avoiding an open proxy that could be used for SSRF, scanning, credential attacks, or unbounded bandwidth consumption. Operators may add genuinely public resources after verification without creating user accounts for visitors.

## Current proof

The local Compose demo registers exactly `http://demo-target:9000` through a demo-only exception. The acceptance test proves that the registered resource opens while an arbitrary public site, loopback, cloud metadata, and a wrong port are denied.
