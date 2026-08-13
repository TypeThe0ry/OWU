# OWU for macOS — Product and implementation plan

## Product definition

OWU for macOS is a lightweight, no-account web launcher and compact browser. A user enters any HTTP or HTTPS address and the app loads it directly with the system WebKit engine. It does not operate a relay, VPN, packet tunnel, traffic obfuscator, or firewall-bypass service.

The native app should feel like the website: one address field, liquid-glass surfaces, automatic light/dark appearance, and very little chrome.

## MVP experience

### Home

```text
┌──────────────────────────────────────────────────────────────┐
│  ◉ OWU                                      Light / Dark     │
│                                                              │
│                    Open the web.                             │
│                   One address away.                          │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ example.com                              Open website ↗ │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

Behavior:

- Accept a hostname, full HTTP/HTTPS URL, path, query, fragment, and optional port.
- Add `https://` when the user omits a scheme.
- Reject non-web schemes and URLs containing embedded credentials.
- Press Return to open in the in-app browser.
- Never send the entered URL to an OWU server for proxying.
- Store theme preference locally; keep history and favorites off by default in the first build.

### Browser view

- Compact unified toolbar: Back, Forward, Reload/Stop, address field, Open in default browser, Share.
- One active page in MVP; tabs are a later milestone.
- Loading progress appears as a subtle line beneath the toolbar.
- Certificate, offline, DNS, and unsupported-scheme failures use plain English error views.
- External application schemes such as `mailto:` require explicit user confirmation before leaving OWU.
- Downloads require a save-panel confirmation and never execute automatically.

## Technical architecture

### Stack

- Swift 6 language mode where the selected Xcode version supports it; otherwise Swift 5.10 with strict concurrency warnings.
- SwiftUI application shell.
- `WKWebView` wrapped with `NSViewRepresentable` for the browser surface.
- `WKNavigationDelegate` for navigation policy, progress, failures, downloads, and external-scheme decisions.
- App Sandbox with outgoing network client entitlement only.
- `@AppStorage` for appearance and small local preferences.
- `WKWebsiteDataStore.default()` for normal browsing; an optional private session can use `.nonPersistent()` later.

Apple documents `WKWebView` as the platform-native interactive web-content view with back/forward navigation and delegate-controlled policy: https://developer.apple.com/documentation/webkit/wkwebview

Sandboxed network browsing requires the outgoing network client entitlement: https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.network.client

### Core modules

```text
OWUApp
├── AppShell
│   ├── ThemeController
│   └── WindowCommands
├── Addressing
│   ├── AddressNormalizer
│   └── ExternalSchemePolicy
├── Browser
│   ├── BrowserModel
│   ├── WebViewContainer
│   ├── NavigationDelegate
│   └── DownloadCoordinator
└── Privacy
    ├── WebsiteDataController
    └── OptionalPrivateSession
```

`AddressNormalizer` should be a pure, heavily tested module. It returns either a canonical `URL` or a localized error; it performs no network request.

### Network and security decisions

- Prefer HTTPS and add it for bare hostnames.
- Do not disable TLS certificate validation.
- App Transport Security remains enabled. Apple notes that ATS requires secure TLS connections by default. If broad HTTP browsing is a hard product requirement, use the web-content-only exception rather than disabling ATS for all app networking, and disclose the risk before loading cleartext pages: https://developer.apple.com/documentation/security/preventing-insecure-network-connections
- Do not add Network Extension entitlements: they are unnecessary for a direct WebKit browser and would incorrectly move the product toward system tunneling.
- Do not inject JavaScript into third-party pages in MVP.
- Do not implement automatic credential capture, page-content logging, traffic inspection, certificate overrides, or background crawling.
- Keep analytics URL-free; record only coarse app events if analytics is added later.

## Visual system

- Material: translucent SwiftUI materials with a restrained colored ambient backdrop.
- Shape: 18–24 point continuous corners for primary glass surfaces.
- Typography: system rounded display face for the headline; monospaced address entry.
- Light: cool mist background, dark navy text, blue-violet accents.
- Dark: near-black blue background, white text, higher-opacity glass borders.
- Accessibility: Reduce Transparency replaces glass with opaque surfaces; Increase Contrast strengthens borders; Reduce Motion removes ambient animation.
- Keyboard: Command-L focuses the address field; Command-R reloads; Command-[ and Command-] navigate; Command-Shift-P toggles private session when implemented.

## Delivery milestones

### M0 — Product shell (1–2 days)

- New Xcode macOS app target and signing setup.
- Liquid-glass home, theme behavior, address normalizer, unit tests.
- Acceptance: launch, theme, keyboard focus, and URL normalization work without network access.

### M1 — Direct browser (2–4 days)

- `WKWebView`, navigation model, progress, back/forward/reload, error surfaces.
- Acceptance: HTTPS sites, custom ports, redirects, cookies, SPA navigation, media, and pop-up decisions tested.

### M2 — System integration (2–3 days)

- Open-with-OWU URL handling, Share menu, Open in default browser, safe downloads.
- Acceptance: `http`/`https` links can be handed to OWU; non-web schemes require confirmation.

### M3 — Privacy and quality (2–4 days)

- Clear website data, optional private window, VoiceOver, keyboard commands, crash/error handling.
- Acceptance: privacy mode is nonpersistent, data clearing works, and accessibility audit passes.

### M4 — Distribution (2–3 days)

- App icon, Developer ID signing/notarization or Mac App Store packaging, privacy copy, update strategy.
- Acceptance: clean install on Apple silicon and Intel test machines, launch after restart, Gatekeeper verification, and uninstall checklist.

## Test matrix

- Addressing: bare host, uppercase host, IDN, IPv6 literal, explicit port, path/query/fragment, malformed URL, credentials, unsupported schemes.
- Navigation: redirects, back/forward, SPA history, pop-up, target blank, download, authentication prompt, certificate failure, offline/DNS error.
- Appearance: light, dark, system switch, Reduce Transparency, Increase Contrast, 200% text scale.
- Lifecycle: cold start, multiple windows, sleep/wake, network change, crash recovery.
- Privacy: clear cookies/cache, private session teardown, no URL values in telemetry or logs.

## Explicit non-goals

- No VPN, packet tunnel, SOCKS, HTTP CONNECT, proxy auto-configuration, traffic relay, protocol disguise, or censorship/firewall evasion.
- No promise that OWU can reach a destination blocked by the user’s network, DNS provider, organization, region, or the destination itself.
- No account system, cloud history, sync, or paid tier in MVP.
