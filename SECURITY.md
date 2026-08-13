# Security policy

## Supported versions

OWU is currently pre-1.0. Security fixes are applied to the latest commit on
`main` and to the newest published release. Older snapshots are not maintained.

## Reporting a vulnerability

Use GitHub's **Report a vulnerability** form (Security tab -> Advisories ->
New draft security advisory) when private vulnerability reporting is enabled.
If it is unavailable, open an issue containing only a request for a private
contact channel. Do not include an exploit, credential, private hostname,
session cookie, tunnel key, or production URL in a public issue.

A useful report includes:

- the affected commit or release;
- the affected component (`app`, `gateway`, `macos`, or deployment template);
- reproducible steps using a disposable target;
- expected and observed behavior;
- impact and any suggested remediation.

Please allow the maintainers time to reproduce and patch the issue before
publishing technical details.

## High-value security boundaries

Reports are especially useful when they demonstrate one of these failures:

- the browser password is forwarded to a destination website;
- a web request reaches loopback, link-local, private, metadata, multicast, or
  another prohibited address;
- DNS validation and the actual upstream connection use different addresses;
- cookies for one upstream origin are sent to another origin;
- `/tunnel/{resource-id}` accepts a client-supplied host or port;
- a tunnel works without both edge authentication and the configured tunnel
  key;
- the macOS listener binds to a non-loopback interface;
- credentials are written to logs, defaults, build artifacts, or source.

## Deployment responsibility

The checked-in Nginx configuration requires TLS and Basic Auth before any OWU
route. Replace every example hostname and credential, keep `/etc/owu` and the
password file readable only by their service users, and never commit generated
certificates, password files, environment files, or Keychain exports.
