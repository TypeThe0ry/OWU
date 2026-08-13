# Contributing to OWU

Thanks for improving Open Website Unblocker. Keep changes focused, testable,
and free of deployment-specific data.

## Development setup

Requirements:

- Node.js 22.13 or newer and npm;
- Go 1.23 or newer;
- macOS 14 with Xcode 15.3 or newer for the SwiftUI application.

```sh
npm ci
npm run lint
npm test
npm run build

cd gateway
go test ./...
go vet ./...
```

On macOS, also run:

```sh
cd macos
swift test
```

## Pull requests

1. Create a topic branch from `main`.
2. Keep unrelated formatting and generated output out of the patch.
3. Add or update tests for behavior changes.
4. Update `README.md` and `CHANGELOG.md` when configuration, compatibility,
   public behavior, or operational steps change.
5. Run every applicable check above and `git diff --check`.
6. Describe the user-visible result, tests run, and rollback considerations in
   the pull request.

Do not commit `.env` files, tunnel keys, Basic Auth databases, private keys,
certificate bundles, Keychain exports, IP-specific deployment files, logs, or
captured upstream pages. Use `owu.example.com`, RFC 5737 documentation
addresses, and clearly fake credentials in examples.

## Architecture rules

- Edge authentication stays at Nginx and is stripped before upstream proxying.
- Browser destinations must pass the public-address validation and pinned dial
  path.
- TCP tunnels accept a resource ID only; destinations remain operator-defined
  in `OWU_TCP_RESOURCES`.
- macOS listeners remain loopback-only and secrets remain in Keychain.
- Compatibility work needs a fixture or regression test that demonstrates the
  original failure.

## Security fixes

Do not open a public pull request for an undisclosed vulnerability. Follow
`SECURITY.md` so the fix and disclosure can be coordinated.

## License

By contributing, you agree that your contribution is licensed under the MIT
License in `LICENSE`.
